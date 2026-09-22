package snorg

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jdlugosz963/snorg/internal/archive"
	"github.com/jdlugosz963/snorg/internal/querylang"
)

// Page is one candidate page as a Predicate sees it: the two documents exactly as
// stored, plus the Client they came from — so a predicate can reach anything else
// the archive knows (the transcription sidecar, the configured templates) instead
// of being limited to the two structs.
type Page struct {
	Client *Client
	Note   NoteDoc
	Doc    PageDoc
}

// Predicate decides whether a candidate page matches. Client.Query calls one per
// page of the archive; every Match* function below builds one, and a hand-written
// predicate is just a func of the same shape.
type Predicate func(Page) bool

// TextMatcher decides whether one string matches — the seam every text filter is
// built on, so the same predicate serves a substring search, a regexp and an exact
// comparison (the query language's ':', '~' and '=' operators) without three
// variants of each Match* function. A hand-written matcher is just a func of the
// same shape.
type TextMatcher func(string) bool

// Regexp matches text the regexp matches anywhere (the '~' operator).
func Regexp(re *regexp.Regexp) TextMatcher {
	return func(s string) bool { return re.MatchString(s) }
}

// Substring matches text containing sub, case-insensitively (the ':' operator —
// the everyday one, which is why it folds case and needs no escaping).
func Substring(sub string) TextMatcher {
	folded := strings.ToLower(sub)
	return func(s string) bool { return strings.Contains(strings.ToLower(s), folded) }
}

// Exact matches text equal to want (the '=' operator).
func Exact(want string) TextMatcher {
	return func(s string) bool { return s == want }
}

// MatchAll matches every page.
func MatchAll(Page) bool { return true }

// MatchStarred matches pages flagged with a star.
func MatchStarred(p Page) bool { return p.Doc.Starred }

// MatchUnanalyzed matches pages without a stored analysis.
func MatchUnanalyzed(p Page) bool { return p.Doc.Analysis == nil }

// MatchTemplated matches pages drawn on a configured template — a page whose
// background_hash resolves to a templates: entry. The resolution is the config's,
// not the stored analysis', so a page matches before it is ever analyzed; with no
// templates configured nothing matches, and MatchNot(MatchTemplated) is exactly
// the free-form pages.
func MatchTemplated(p Page) bool { return pageTemplate(p) != nil }

// MatchAnd matches a page only when every predicate does (empty = matches all).
// Used to intersect a filter with a piped candidate set, so query A | query B ==
// A∩B.
func MatchAnd(preds ...Predicate) Predicate {
	return func(p Page) bool {
		for _, pred := range preds {
			if !pred(p) {
				return false
			}
		}
		return true
	}
}

// MatchOr matches a page when any predicate does (empty = matches nothing, the
// dual of MatchAnd).
func MatchOr(preds ...Predicate) Predicate {
	return func(p Page) bool {
		for _, pred := range preds {
			if pred(p) {
				return true
			}
		}
		return false
	}
}

// MatchNot matches exactly the pages pred does not (the inverse of a filter). Under
// piping it combines via MatchAnd, so query A | query NOT B == A minus B.
func MatchNot(pred Predicate) Predicate {
	return func(p Page) bool { return !pred(p) }
}

// MatchIDs matches pages whose PAGEID is in ids (an empty set matches nothing).
// This is the restriction applied when PAGEIDs are piped into query on stdin.
func MatchIDs(ids []string) Predicate {
	set := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		set[id] = struct{}{}
	}
	return func(p Page) bool {
		_, ok := set[p.Doc.PageID]
		return ok
	}
}

// MatchNote matches the pages of the notes whose FILE_ID matches.
func MatchNote(m TextMatcher) Predicate {
	return func(p Page) bool { return m(p.Note.FileID) }
}

// MatchKeyword matches pages with at least one device keyword whose text matches.
func MatchKeyword(m TextMatcher) Predicate {
	return func(p Page) bool {
		for _, kw := range p.Doc.Keywords {
			if m(kw.Text) {
				return true
			}
		}
		return false
	}
}

// MatchTag matches pages with at least one matching snorg-managed tag — the
// page's own tags plus the ones inherited from its note (EffectiveTags).
func MatchTag(m TextMatcher) Predicate {
	return func(p Page) bool {
		for _, t := range EffectiveTags(p.Note, p.Doc) {
			if m(t) {
				return true
			}
		}
		return false
	}
}

// MatchDate matches pages whose creation day (embedded in the PAGEID) falls within
// [from, to], both inclusive and formatted "YYYYMMDD"; an empty bound is open.
// Pages whose PAGEID carries no date never match.
func MatchDate(from, to string) Predicate {
	return func(p Page) bool {
		d, ok := pageDate(p.Doc.PageID)
		if !ok {
			return false
		}
		return (from == "" || d >= from) && (to == "" || d <= to)
	}
}

// MatchContent matches pages whose transcription (the <PAGEID>.md effective
// content, AI or hand-written) matches. A never-analyzed page reads as empty, so
// it matches only a matcher that accepts the empty string; an unreadable sidecar
// is treated as non-matching. On a templated page the sidecar is the whole
// region-section document, markers included — MatchRegion is the per-box view of
// the same text.
func MatchContent(m TextMatcher) Predicate {
	return func(p Page) bool {
		md, err := p.Client.ReadAnalysis(p.Note.FileID, p.Doc.PageID)
		if err != nil {
			return false
		}
		return m(md)
	}
}

// MatchRegion matches templated pages whose transcription of one template box
// matches. boxID names the box ("" = any of them), and only boxes the page's
// template actually declares are considered — the ids come from the config, so a
// section left in the sidecar for a box that no longer exists (a tombstone) is
// never matched. A declared box with no section yet reads as empty text, and a
// non-templated page never matches. A broken templates: section cannot be seen
// here — Open resolves the set once and fails there.
func MatchRegion(boxID string, m TextMatcher) Predicate {
	return func(p Page) bool {
		tmpl := pageTemplate(p)
		if tmpl == nil {
			return false
		}
		md, err := p.Client.ReadAnalysis(p.Note.FileID, p.Doc.PageID)
		if err != nil {
			return false
		}
		text := make(map[string]string)
		for _, sec := range archive.ParseRegions(md) {
			text[sec.ID] = sec.Text
		}
		for _, b := range tmpl.Boxes {
			if boxID != "" && b.ID != boxID {
				continue
			}
			if m(text[b.ID]) {
				return true
			}
		}
		return false
	}
}

// pageTemplate resolves the config template a page is drawn on, or nil (not
// templated, none configured, or an unreadable set — Open already rejected a
// broken one).
func pageTemplate(p Page) *Template {
	ts, err := p.Client.Templates()
	if err != nil {
		return nil
	}
	return ts.MatchBackground(p.Doc.BackgroundHash)
}

// pageDate extracts the "YYYYMMDD" day embedded in a supernote id. PAGEIDs (and
// FILE_IDs) are "P"/"F" + 14-digit YYYYMMDDHHMMSS + tail; fewer than 8 leading
// digits after the optional prefix means no date (ok == false).
func pageDate(id string) (string, bool) {
	s := id
	if len(s) > 0 && (s[0] == 'P' || s[0] == 'F') {
		s = s[1:]
	}
	n := 0
	for n < len(s) && s[n] >= '0' && s[n] <= '9' {
		n++
	}
	if n < 8 {
		return "", false
	}
	return s[:8], true
}

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

// ParseDateSpec turns a date term's value into an inclusive [from, to] range
// formatted "YYYYMMDD" (an empty bound is open). It accepts "today"/"yesterday", a
// single "YYYY-MM-DD" day, and "FROM..TO" ranges with either end omitted.
func ParseDateSpec(spec string) (from, to string, err error) {
	day := func(s string) (string, error) {
		t, err := time.Parse("2006-01-02", s)
		if err != nil {
			return "", fmt.Errorf("invalid date %q (want YYYY-MM-DD): %w", s, err)
		}
		return t.Format("20060102"), nil
	}
	switch spec {
	case "today":
		d := time.Now().Format("20060102")
		return d, d, nil
	case "yesterday":
		d := time.Now().AddDate(0, 0, -1).Format("20060102")
		return d, d, nil
	}
	lo, hi, isRange := strings.Cut(spec, "..")
	if !isRange {
		d, err := day(spec)
		return d, d, err
	}
	if lo != "" {
		if from, err = day(lo); err != nil {
			return "", "", err
		}
	}
	if hi != "" {
		if to, err = day(hi); err != nil {
			return "", "", err
		}
	}
	if from == "" && to == "" {
		return "", "", fmt.Errorf("empty date range %q", spec)
	}
	return from, to, nil
}
