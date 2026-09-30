package snorg

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/jdlugosz963/snorg/internal/archive"
)

// Page is one candidate page as a Predicate sees it: the two documents exactly as
// stored, plus the Client they came from — so a predicate can reach anything else
// the archive knows (the transcription sidecar, the configured templates) instead
// of being limited to the two structs.
type Page struct {
	Client *Client
	Note   NoteDoc
	Doc    PageDoc

	// fail is where Fail records; Query owns it, since a Page is passed by value.
	fail *queryErr
}

// queryErr holds the first error a predicate hit during one walk.
type queryErr struct{ err error }

// Fail records err as the reason this query cannot be answered: Query abandons the
// walk and returns it instead of a result set. A predicate that reads outside the two
// documents must call it rather than answering false — an unreadable page counted as a
// non-match is a page MatchNot then reports as a match. The first error wins, and a
// predicate invoked outside Query has nowhere to report, so Fail is then a no-op.
func (p Page) Fail(err error) {
	if p.fail != nil && p.fail.err == nil {
		p.fail.err = fmt.Errorf("page %s: %w", p.Doc.PageID, err)
	}
}

// Predicate decides whether a candidate page matches. Client.Query calls one per
// page of the archive; every Match* function below builds one, and a hand-written
// predicate is just a func of the same shape.
type Predicate func(Page) bool

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
// fails the whole query (see Page.Fail) rather than passing for empty. On a
// templated page the sidecar is the whole region-section document, markers included
// — MatchRegion is the per-box view of the same text.
func MatchContent(m TextMatcher) Predicate {
	return func(p Page) bool {
		md, err := p.Client.arch.ReadAnalysisMD(p.Note.FileID, p.Doc.PageID)
		if err != nil {
			p.Fail(err)
			return false
		}
		return m(md)
	}
}

// MatchRegion matches templated pages whose transcription of one template box
// matches. boxID names the box ("" = any of them); only boxes the page's template
// declares are considered, a declared box with no section yet reads as empty text,
// and a non-templated page never matches. An unreadable sidecar fails the query, as
// in MatchContent.
func MatchRegion(boxID string, m TextMatcher) Predicate {
	return func(p Page) bool {
		tmpl := pageTemplate(p)
		if tmpl == nil {
			return false
		}
		md, err := p.Client.arch.ReadAnalysisMD(p.Note.FileID, p.Doc.PageID)
		if err != nil {
			p.Fail(err)
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

// pageTemplate resolves the config template a page is drawn on, or nil (not templated
// or none configured).
func pageTemplate(p Page) *Template {
	return p.Client.Templates().MatchBackground(p.Doc.BackgroundHash)
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
