// Package query is the read-only filter side of the archive: it walks every
// note/page and selects the pages matching a predicate over their stored
// metadata (star, keywords, ...). Like retrieve it is platform-agnostic and
// talks to the archive only through its read accessors; callers (CLI, future
// tools) compose a Predicate and get back the matching page identities.
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

// Predicate decides whether a page matches.
type Predicate func(archive.PageDoc) bool

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
			if pred(pd) {
				out = append(out, Match{FileID: fileID, PageID: pd.PageID})
			}
		}
	}
	return out, nil
}

// Starred matches pages flagged with a star.
func Starred(pd archive.PageDoc) bool { return pd.Starred }

// Keyword matches pages with at least one keyword whose text matches re.
func Keyword(re *regexp.Regexp) Predicate {
	return func(pd archive.PageDoc) bool {
		for _, kw := range pd.Keywords {
			if re.MatchString(kw.Text) {
				return true
			}
		}
		return false
	}
}
