package snorg

import "github.com/jdlugosz963/snorg/internal/archive"

// Tag adds tag to each of pageIDs — the snorg-managed labels that live alongside a
// page's device keywords. Tags are kept sorted and de-duplicated on disk. It returns
// the number of pages actually changed (a page already carrying the tag writes
// nothing); it stops and reports the first error, so an unknown PAGEID aborts the
// batch.
func (c *Client) Tag(pageIDs []string, tag string) (int, error) {
	return countChanged(pageIDs, func(id string) (bool, error) { return c.arch.TagPage(id, tag, false) })
}

// Untag removes tag from each of pageIDs, like Tag in reverse. A tag the page only
// inherits from its note is not its own to remove (see UntagNote) and counts as
// unchanged.
func (c *Client) Untag(pageIDs []string, tag string) (int, error) {
	return countChanged(pageIDs, func(id string) (bool, error) { return c.arch.TagPage(id, tag, true) })
}

// TagNote adds tag to each of fileIDs — a note-level snorg tag, stored in that
// note's note.json only and inherited by every one of its pages (the pages' own tags
// are never touched). It returns the number of notes actually changed; it stops and
// reports the first error, so an unknown FILE_ID aborts the batch.
func (c *Client) TagNote(fileIDs []string, tag string) (int, error) {
	return countChanged(fileIDs, func(id string) (bool, error) { return c.arch.TagNote(id, tag, false) })
}

// UntagNote removes a note-level tag from each of fileIDs, like TagNote in reverse.
func (c *Client) UntagNote(fileIDs []string, tag string) (int, error) {
	return countChanged(fileIDs, func(id string) (bool, error) { return c.arch.TagNote(id, tag, true) })
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
