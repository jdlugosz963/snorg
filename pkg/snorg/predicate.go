package snorg

import (
	"regexp"
	"strings"
	"time"

	"github.com/jdlugosz963/snorg/internal/archive"
)

// Page is one candidate page as a Predicate sees it: the two documents exactly as
// stored, plus read-only access to what else the archive knows about the page (its
// transcription, its template). Only Query builds one.
type Page struct {
	Note NoteDoc
	Doc  PageDoc

	c *Client
}

// Transcription returns the page's <PAGEID>.md effective content, AI or
// hand-written; a never-analyzed page reads as empty.
func (p Page) Transcription() (string, error) {
	return p.c.arch.ReadAnalysisMD(p.Note.FileID, p.Doc.PageID)
}

// Template returns the config template the page is drawn on, or nil (not templated,
// or no templates configured).
func (p Page) Template() *Template {
	return p.c.tmpl.MatchBackground(p.Doc.BackgroundHash)
}

// Predicate decides whether a candidate page matches. Client.Query calls one per
// page of the archive; every Match* function below builds one, and a hand-written
// predicate is just a func of the same shape. A predicate that cannot decide (a read
// failed) returns the error rather than false: Query stops and returns it, since an
// unreadable page counted as a non-match is one MatchNot would report as a match.
type Predicate func(Page) (bool, error)

// TextMatcher decides whether one string matches; every text filter takes one.
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
func MatchAll(Page) (bool, error) { return true, nil }

// MatchStarred matches pages flagged with a star.
func MatchStarred(p Page) (bool, error) { return p.Doc.Starred, nil }

// MatchUnanalyzed matches pages without a stored analysis.
func MatchUnanalyzed(p Page) (bool, error) { return p.Doc.Analysis == nil, nil }

// MatchTemplated matches pages drawn on a configured template — a page whose
// background_hash resolves to a templates: entry. The resolution is the config's,
// not the stored analysis', so a page matches before it is ever analyzed; with no
// templates configured nothing matches, and MatchNot(MatchTemplated) is exactly
// the free-form pages.
func MatchTemplated(p Page) (bool, error) { return p.Template() != nil, nil }

// MatchAnd matches a page only when every predicate does (empty = matches all).
// Used to intersect a filter with a piped candidate set, so query A | query B ==
// A∩B. The first error stops it.
func MatchAnd(preds ...Predicate) Predicate {
	return func(p Page) (bool, error) {
		for _, pred := range preds {
			if ok, err := pred(p); err != nil || !ok {
				return false, err
			}
		}
		return true, nil
	}
}

// MatchOr matches a page when any predicate does (empty = matches nothing, the
// dual of MatchAnd). The first error stops it.
func MatchOr(preds ...Predicate) Predicate {
	return func(p Page) (bool, error) {
		for _, pred := range preds {
			if ok, err := pred(p); err != nil || ok {
				return ok, err
			}
		}
		return false, nil
	}
}

// MatchNot matches exactly the pages pred does not (the inverse of a filter); an
// error passes through uninverted. Under piping it combines via MatchAnd, so
// query A | query NOT B == A minus B.
func MatchNot(pred Predicate) Predicate {
	return func(p Page) (bool, error) {
		ok, err := pred(p)
		if err != nil {
			return false, err
		}
		return !ok, nil
	}
}

// MatchIDs matches pages whose PAGEID is in ids (an empty set matches nothing).
// This is the restriction applied when PAGEIDs are piped into query on stdin.
func MatchIDs(ids []string) Predicate {
	set := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		set[id] = struct{}{}
	}
	return func(p Page) (bool, error) {
		_, ok := set[p.Doc.PageID]
		return ok, nil
	}
}

// MatchNote matches the pages of the notes whose FILE_ID matches.
func MatchNote(m TextMatcher) Predicate {
	return func(p Page) (bool, error) { return m(p.Note.FileID), nil }
}

// MatchKeyword matches pages with at least one device keyword whose text matches.
func MatchKeyword(m TextMatcher) Predicate {
	return func(p Page) (bool, error) {
		for _, kw := range p.Doc.Keywords {
			if m(kw.Text) {
				return true, nil
			}
		}
		return false, nil
	}
}

// MatchTag matches pages with at least one matching snorg-managed tag — the
// page's own tags plus the ones inherited from its note (EffectiveTags).
func MatchTag(m TextMatcher) Predicate {
	return func(p Page) (bool, error) {
		for _, t := range EffectiveTags(p.Note, p.Doc) {
			if m(t) {
				return true, nil
			}
		}
		return false, nil
	}
}

// MatchDate matches pages whose creation day (embedded in the PAGEID) falls within
// [from, to], both inclusive. Only each bound's calendar day counts (in its own
// location; the time of day is ignored), and a zero bound is open. Pages whose
// PAGEID carries no date never match.
func MatchDate(from, to time.Time) Predicate {
	lo, hi := dayOf(from), dayOf(to)
	return func(p Page) (bool, error) {
		d, ok := pageDate(p.Doc.PageID)
		if !ok {
			return false, nil
		}
		return (lo == "" || d >= lo) && (hi == "" || d <= hi), nil
	}
}

// dayOf formats t as the "YYYYMMDD" a PAGEID embeds, or "" for the zero time (an
// open bound). Fixed-width digits, so the days order as strings.
func dayOf(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("20060102")
}

// MatchContent matches pages whose transcription (Page.Transcription) matches. A
// never-analyzed page reads as empty, so it matches only a matcher that accepts the
// empty string; an unreadable sidecar is an error, not empty. On a templated page
// the transcription is the whole region-section document, markers included —
// MatchRegion is the per-box view of the same text.
func MatchContent(m TextMatcher) Predicate {
	return func(p Page) (bool, error) {
		md, err := p.Transcription()
		if err != nil {
			return false, err
		}
		return m(md), nil
	}
}

// MatchRegion matches templated pages whose transcription of one template box
// matches. boxID names the box ("" = any of them); only boxes the page's template
// declares are considered, a declared box with no section yet reads as empty text,
// and a non-templated page never matches.
func MatchRegion(boxID string, m TextMatcher) Predicate {
	return func(p Page) (bool, error) {
		tmpl := p.Template()
		if tmpl == nil {
			return false, nil
		}
		md, err := p.Transcription()
		if err != nil {
			return false, err
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
				return true, nil
			}
		}
		return false, nil
	}
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
