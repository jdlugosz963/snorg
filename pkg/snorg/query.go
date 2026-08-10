package snorg

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jdlugosz963/snorg/internal/query"
)

// Predicate constructors, re-exported so callers can build filters for Client.Query.
// All, Starred and Unanalyzed are predicates themselves; the rest are constructors.
var (
	All        = query.All
	Starred    = query.Starred
	Unanalyzed = query.Unanalyzed
)

// And matches pages satisfying every predicate.
func And(preds ...Predicate) Predicate { return query.And(preds...) }

// Not inverts a predicate.
func Not(pred Predicate) Predicate { return query.Not(pred) }

// InSet matches pages whose PAGEID is in ids (used to intersect two queries).
func InSet(ids []string) Predicate { return query.InSet(ids) }

// InNote matches every page of the given FILE_ID.
func InNote(fileID string) Predicate { return query.InNote(fileID) }

// Keyword matches pages with a device keyword matching re.
func Keyword(re *regexp.Regexp) Predicate { return query.Keyword(re) }

// Date matches pages whose day (from the PAGEID's leading 8 digits) is within the
// inclusive [from, to] range, each "YYYYMMDD"; an empty bound is open.
func Date(from, to string) Predicate { return query.Date(from, to) }

// Content matches pages whose transcription matches re. It reads the archive, so it
// is a method rather than a free constructor.
func (c *Client) Content(re *regexp.Regexp) Predicate { return query.Content(c.arch, re) }

// QueryFilters lists the filter words ParseFilter accepts.
const QueryFilters = "all, note <FILE_ID>, unanalyzed, keyword <regexp>, content <regexp>, starred, date <spec>, not <filter> (inverse)"

// ParseFilter builds a Predicate from a filter word and its arguments — the string
// DSL behind the CLI's query command: all, starred, unanalyzed, note <FILE_ID>,
// keyword <regexp>, content <regexp>, date <spec> (today|yesterday|YYYY-MM-DD|
// FROM..TO with open ends), and a "not" prefix that inverts any filter.
func (c *Client) ParseFilter(filter string, args []string) (Predicate, error) {
	arity := func(n int, usage string) error {
		if len(args) != n {
			return fmt.Errorf("filter %q takes: %s", filter, usage)
		}
		return nil
	}
	// "not" inverts any filter: it recurses on the rest (so the inner filter's own
	// arg parsing/arity apply verbatim) and negates the result.
	if filter == "not" {
		if len(args) < 1 {
			return nil, fmt.Errorf("not takes a filter: not <filter> [arg] (filters: %s)", QueryFilters)
		}
		inner, err := c.ParseFilter(args[0], args[1:])
		if err != nil {
			return nil, err
		}
		return Not(inner), nil
	}
	switch filter {
	case "all":
		return All, arity(0, "all")
	case "starred":
		return Starred, arity(0, "starred")
	case "unanalyzed":
		return Unanalyzed, arity(0, "unanalyzed")
	case "note":
		if err := arity(1, "note <FILE_ID>"); err != nil {
			return nil, err
		}
		return InNote(args[0]), nil
	case "keyword":
		if err := arity(1, "keyword <regexp>"); err != nil {
			return nil, err
		}
		re, err := regexp.Compile(args[0])
		if err != nil {
			return nil, fmt.Errorf("invalid keyword regexp: %w", err)
		}
		return Keyword(re), nil
	case "content":
		if err := arity(1, "content <regexp>"); err != nil {
			return nil, err
		}
		re, err := regexp.Compile(args[0])
		if err != nil {
			return nil, fmt.Errorf("invalid content regexp: %w", err)
		}
		return c.Content(re), nil
	case "date":
		if err := arity(1, "date <spec>   (today|yesterday|YYYY-MM-DD|FROM..TO, open ends ok)"); err != nil {
			return nil, err
		}
		from, to, err := ParseDateSpec(args[0])
		if err != nil {
			return nil, err
		}
		return Date(from, to), nil
	default:
		return nil, fmt.Errorf("unknown filter: %q (want: %s)", filter, QueryFilters)
	}
}

// ParseDateSpec turns a date filter argument into an inclusive [from, to] range
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
