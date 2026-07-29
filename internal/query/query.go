// Package query is the read-only filter side of the archive: it walks every
// note/page and selects the pages matching a predicate over their stored
// metadata (star, keywords, note membership, analysis state, ...). Like
// retrieve it is platform-agnostic and talks to the archive only through its
// read accessors; callers (CLI, future tools) compose a Predicate and get back
// the matching page identities — ready to pipe into analyze.
package query

import (
	"fmt"
	"regexp"

	"github.com/jdlugosz963/snorg/internal/archive"
)

// Match is one page that satisfied a query predicate.
type Match struct {
	FileID string
	PageID string
}

// Predicate decides whether a page (of the note fileID) matches.
type Predicate func(fileID string, pd archive.PageDoc) bool

// Pages walks every note/page in the archive (List order, then note.json page
// order) and returns the pages for which pred is true.
func Pages(a *archive.Archive, pred Predicate) ([]Match, error) {
	ids, err := a.List()
	if err != nil {
		return nil, err
	}
	var out []Match
	for _, fileID := range ids {
		nd, err := a.ReadNote(fileID)
		if err != nil {
			return nil, fmt.Errorf("note %s: %w", fileID, err)
		}
		for _, ref := range nd.Pages {
			pd, err := a.ReadPage(fileID, ref.ID)
			if err != nil {
				return nil, fmt.Errorf("note %s page %s: %w", fileID, ref.ID, err)
			}
			if pred(fileID, pd) {
				out = append(out, Match{FileID: fileID, PageID: pd.PageID})
			}
		}
	}
	return out, nil
}

// And matches a page only when every predicate does (empty = matches all). Used
// to intersect a filter with a piped candidate set, so query A | query B == A∩B.
func And(preds ...Predicate) Predicate {
	return func(fileID string, pd archive.PageDoc) bool {
		for _, p := range preds {
			if !p(fileID, pd) {
				return false
			}
		}
		return true
	}
}

// Not matches exactly the pages pred does not (the inverse of a filter). Under
// piping it combines via And, so query A | query not B == A minus B.
func Not(pred Predicate) Predicate {
	return func(fileID string, pd archive.PageDoc) bool { return !pred(fileID, pd) }
}

// InSet matches pages whose PAGEID is in ids (an empty set matches nothing). This
// is the restriction applied when PAGEIDs are piped into query on stdin.
func InSet(ids []string) Predicate {
	set := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		set[id] = struct{}{}
	}
	return func(_ string, pd archive.PageDoc) bool {
		_, ok := set[pd.PageID]
		return ok
	}
}

// All matches every page.
func All(string, archive.PageDoc) bool { return true }

// Starred matches pages flagged with a star.
func Starred(_ string, pd archive.PageDoc) bool { return pd.Starred }

// Unanalyzed matches pages without a stored analysis.
func Unanalyzed(_ string, pd archive.PageDoc) bool { return pd.Analysis == nil }

// InNote matches the pages of one note.
func InNote(fileID string) Predicate {
	return func(id string, _ archive.PageDoc) bool { return id == fileID }
}

// Keyword matches pages with at least one keyword whose text matches re.
func Keyword(re *regexp.Regexp) Predicate {
	return func(_ string, pd archive.PageDoc) bool {
		for _, kw := range pd.Keywords {
			if re.MatchString(kw.Text) {
				return true
			}
		}
		return false
	}
}

// Content matches pages whose transcribed content (the <PAGEID>.md effective
// content, AI or hand-written) matches re. Pages with no transcription never
// match; an unreadable md is treated as non-matching.
func Content(a *archive.Archive, re *regexp.Regexp) Predicate {
	return func(fileID string, pd archive.PageDoc) bool {
		md, err := a.ReadAnalysisMD(fileID, pd.PageID)
		if err != nil {
			return false
		}
		return re.MatchString(md)
	}
}

// Date matches pages whose creation day (embedded in the PAGEID) falls within
// [from, to], both inclusive and formatted "YYYYMMDD"; an empty bound is open.
// Pages whose PAGEID carries no date never match.
func Date(from, to string) Predicate {
	return func(_ string, pd archive.PageDoc) bool {
		d, ok := pageDate(pd.PageID)
		if !ok {
			return false
		}
		return (from == "" || d >= from) && (to == "" || d <= to)
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
