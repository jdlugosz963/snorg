package snorg

import (
	"github.com/jdlugosz963/snorg/internal/archive"
	"github.com/jdlugosz963/snorg/internal/query"
)

// Tag adds tag to (or, when remove is set, removes it from) each of pageIDs — the
// snorg-managed labels that live alongside a page's device keywords. Tags are kept
// sorted and de-duplicated on disk. It returns the number of pages actually changed
// (a no-op add/remove writes nothing); it stops and reports the first error, so an
// unknown PAGEID aborts the batch.
func (c *Client) Tag(tag string, pageIDs []string, remove bool) (int, error) {
	changed := 0
	for _, pageID := range pageIDs {
		ok, err := c.arch.TagPage(pageID, tag, remove)
		if err != nil {
			return changed, err
		}
		if ok {
			changed++
		}
	}
	return changed, nil
}

// TagNote adds tag to (or, when remove is set, removes it from) each of fileIDs —
// a note-level snorg tag, stored in that note's note.json only and inherited by
// every one of its pages (the pages' own tags are never touched). It returns the
// number of notes actually changed (a no-op add/remove writes nothing); it stops and
// reports the first error, so an unknown FILE_ID aborts the batch.
func (c *Client) TagNote(tag string, fileIDs []string, remove bool) (int, error) {
	changed := 0
	for _, fileID := range fileIDs {
		ok, err := c.arch.TagNote(fileID, tag, remove)
		if err != nil {
			return changed, err
		}
		if ok {
			changed++
		}
	}
	return changed, nil
}

// EffectiveTags is a page's effective tag set: its own snorg-managed tags unioned
// with the ones inherited from its note, sorted and de-duplicated — the same rule
// every read surface applies.
func EffectiveTags(nd NoteDoc, pd PageDoc) []string { return archive.EffectiveTags(nd, pd) }

// Keywords lists the archive's distinct device keywords with per-value page counts,
// sorted by value (the read side of the CLI's `list keywords`).
func (c *Client) Keywords() ([]ValueCount, error) { return query.Keywords(c.arch) }

// Tags lists the archive's distinct snorg-managed tags with per-value page counts,
// sorted by value (the read side of the CLI's `list tags`).
func (c *Client) Tags() ([]ValueCount, error) { return query.Tags(c.arch) }
