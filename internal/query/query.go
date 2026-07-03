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
