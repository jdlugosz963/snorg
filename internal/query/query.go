// Package query is the read-only walk over the archive: it visits every
// note/page and returns the pages a match function accepts. Like retrieve it is
// platform-agnostic and talks to the archive only through its read accessors.
// The filter vocabulary itself (the Match* predicates and the filter DSL) lives
// in pkg/snorg, where the Client a predicate needs is in scope; this package
// only supplies the walk.
package query

import (
	"fmt"

	"github.com/jdlugosz963/snorg/internal/archive"
)

// Match is one page that satisfied a query predicate.
type Match struct {
	FileID string
	PageID string
}

// Pages walks every note/page in the archive (List order, then note.json page
// order) and returns the pages for which match is true. The match function gets
// the whole NoteDoc (not just its FileID) so note-level state — the
// snorg-managed tags every page inherits — is in scope without a second read. An
// error from match abandons the walk exactly like a failed read of the archive
// itself: a filter that could not be evaluated must not yield a result set.
func Pages(a *archive.Archive, match func(archive.NoteDoc, archive.PageDoc) (bool, error)) ([]Match, error) {
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
			ok, err := match(nd, pd)
			if err != nil {
				return nil, err
			}
			if ok {
				out = append(out, Match{FileID: fileID, PageID: pd.PageID})
			}
		}
	}
	return out, nil
}
