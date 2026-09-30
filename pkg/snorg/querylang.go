package snorg

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jdlugosz963/snorg/internal/querylang"
)

// QuerySyntax summarizes the expression language ParseQuery accepts, for command
// help and usage errors.
const QuerySyntax = `an expression of terms joined by AND / OR / NOT, grouped with parentheses:
  terms (no value): all, starred, unanalyzed, templated
  terms (with a value): note, keyword, tag, content, region[<box-id>], date:<spec>
  operators: ':' substring (case-insensitive), '~' regexp, '=' exact
  date <spec>: today | yesterday | YYYY-MM-DD | FROM..TO (either end may be empty)
  a value must follow its operator immediately; quote it if it holds spaces or parens
  example: starred AND (date:2026-04-04..2026-09-12 OR content:"some thing")`

// ParseQuery compiles a query expression into a Predicate — the string language
// behind the CLI's query command (see QuerySyntax). The grammar (AND/OR/NOT,
// parentheses, terms) is parsed by internal/querylang; this is where the field
// vocabulary lives, so an unknown name, a value on a term that takes none or a
// bad regexp/date is rejected here, with the column it appeared at.
func ParseQuery(expr string) (Predicate, error) {
	ast, err := querylang.Parse(expr)
	if err != nil {
		return nil, err
	}
	return compileExpr(ast)
}

func compileExpr(e *querylang.Expr) (Predicate, error) {
	preds, err := compileAll(e.Terms, compileAnd)
	if err != nil {
		return nil, err
	}
	if len(preds) == 1 {
		return preds[0], nil
	}
	return MatchOr(preds...), nil
}

func compileAnd(a *querylang.AndExpr) (Predicate, error) {
	preds, err := compileAll(a.Terms, compileUnary)
	if err != nil {
		return nil, err
	}
	if len(preds) == 1 {
		return preds[0], nil
	}
	return MatchAnd(preds...), nil
}

func compileAll[T any](nodes []T, compile func(T) (Predicate, error)) ([]Predicate, error) {
	preds := make([]Predicate, 0, len(nodes))
	for _, n := range nodes {
		pred, err := compile(n)
		if err != nil {
			return nil, err
		}
		preds = append(preds, pred)
	}
	return preds, nil
}

func compileUnary(u *querylang.Unary) (Predicate, error) {
	switch {
	case u.Not != nil:
		inner, err := compileUnary(u.Not)
		if err != nil {
			return nil, err
		}
		return MatchNot(inner), nil
	case u.Sub != nil:
		return compileExpr(u.Sub)
	default:
		return compileTerm(u.Term)
	}
}

// compileTerm is the field vocabulary: it decides which names exist, which take a
// value (and which may carry a [scope]), and how a value becomes a matcher.
func compileTerm(t *querylang.Term) (Predicate, error) {
	fail := func(format string, args ...any) (Predicate, error) {
		return nil, fmt.Errorf("col %d: %s", t.Pos.Column, fmt.Sprintf(format, args...))
	}
	if t.Scope != "" && t.Field != "region" {
		return fail("%q takes no [%s] qualifier (only region does)", t.Field, t.Scope)
	}
	// A term either stands alone or takes a value; mixing the two is the common
	// typo ("starred:x", "tag" with no value), so both are named errors.
	bare := func(pred Predicate) (Predicate, error) {
		if t.HasValue() {
			return fail("%q takes no value (write it alone)", t.Field)
		}
		return pred, nil
	}
	text := func(build func(TextMatcher) Predicate) (Predicate, error) {
		if !t.HasValue() {
			return fail("%q needs a value: %s:<substring>, %s~<regexp> or %s=<exact>", t.Field, t.Field, t.Field, t.Field)
		}
		m, err := textMatcher(t.Op, string(t.Value))
		if err != nil {
			return fail("%s%s%s: %v", t.Field, t.Op, t.Value, err)
		}
		return build(m), nil
	}
	switch t.Field {
	case "all":
		return bare(MatchAll)
	case "starred":
		return bare(MatchStarred)
	case "unanalyzed":
		return bare(MatchUnanalyzed)
	case "templated":
		return bare(MatchTemplated)
	case "note":
		return text(MatchNote)
	case "keyword":
		return text(MatchKeyword)
	case "tag":
		return text(MatchTag)
	case "content":
		return text(MatchContent)
	case "region":
		return text(func(m TextMatcher) Predicate { return MatchRegion(string(t.Scope), m) })
	case "date":
		if t.Op != ":" {
			return fail("date takes date:<spec> (today|yesterday|YYYY-MM-DD|FROM..TO), not %q", t.Op+string(t.Value))
		}
		from, to, err := ParseDateSpec(string(t.Value))
		if err != nil {
			return fail("%v", err)
		}
		return MatchDate(from, to), nil
	default:
		return fail("unknown term %q\n%s", t.Field, QuerySyntax)
	}
}

// textMatcher turns one operator + value into the matcher it denotes.
func textMatcher(op, value string) (TextMatcher, error) {
	switch op {
	case ":":
		return Substring(value), nil
	case "=":
		return Exact(value), nil
	case "~":
		re, err := regexp.Compile(value)
		if err != nil {
			return nil, fmt.Errorf("invalid regexp: %w", err)
		}
		return Regexp(re), nil
	default:
		return nil, fmt.Errorf("unknown operator %q", op)
	}
}

// ParseDateSpec turns a date term's value into an inclusive [from, to] range of
// days for MatchDate, each at midnight (a zero bound is open). It accepts
// "today"/"yesterday" (local time), a single "YYYY-MM-DD" day, and "FROM..TO" ranges
// with either end omitted.
func ParseDateSpec(spec string) (from, to time.Time, err error) {
	switch spec {
	case "today", "yesterday":
		y, m, d := time.Now().Date()
		if spec == "yesterday" {
			d--
		}
		day := time.Date(y, m, d, 0, 0, 0, 0, time.Local)
		return day, day, nil
	}
	day := func(s string) (time.Time, error) {
		t, err := time.Parse("2006-01-02", s)
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid date %q (want YYYY-MM-DD): %w", s, err)
		}
		return t, nil
	}
	lo, hi, isRange := strings.Cut(spec, "..")
	if !isRange {
		d, err := day(spec)
		return d, d, err
	}
	if lo != "" {
		if from, err = day(lo); err != nil {
			return time.Time{}, time.Time{}, err
		}
	}
	if hi != "" {
		if to, err = day(hi); err != nil {
			return time.Time{}, time.Time{}, err
		}
	}
	if from.IsZero() && to.IsZero() {
		return time.Time{}, time.Time{}, fmt.Errorf("empty date range %q", spec)
	}
	return from, to, nil
}
