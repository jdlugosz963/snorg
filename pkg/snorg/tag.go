package snorg

import "github.com/jdlugosz963/snorg/internal/archive"

// Tag adds tag to (or, when remove is set, removes it from) each of pageIDs — the
// snorg-managed labels that live alongside a page's device keywords. Tags are kept
// sorted and de-duplicated on disk. It returns the number of pages actually changed
// (a no-op add/remove writes nothing); it stops and reports the first error, so an
// unknown PAGEID aborts the batch.
func (c *Client) Tag(tag string, pageIDs []string, remove bool) (int, error) {
	return countChanged(pageIDs, func(id string) (bool, error) { return c.arch.TagPage(id, tag, remove) })
}

// TagNote adds tag to (or, when remove is set, removes it from) each of fileIDs —
// a note-level snorg tag, stored in that note's note.json only and inherited by
// every one of its pages (the pages' own tags are never touched). It returns the
// number of notes actually changed (a no-op add/remove writes nothing); it stops and
// reports the first error, so an unknown FILE_ID aborts the batch.
func (c *Client) TagNote(tag string, fileIDs []string, remove bool) (int, error) {
	return countChanged(fileIDs, func(id string) (bool, error) { return c.arch.TagNote(id, tag, remove) })
}

// countChanged applies apply to each id in order and counts the ones it changed,
// stopping at the first error.
func countChanged(ids []string, apply func(string) (bool, error)) (int, error) {
	changed := 0
	for _, id := range ids {
		ok, err := apply(id)
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
