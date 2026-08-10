package snorg

import "github.com/jdlugosz963/snorg/internal/edit"

// EditorFromEnv returns the editor command line from $VISUAL, else $EDITOR — the
// editor an interactive front-end passes to EditPage.
func EditorFromEnv() (string, error) { return edit.EditorFromEnv() }

// EditPage is the interactive convenience behind the CLI's analyze-edit: it opens
// the page buffer in editor (a shell command line) and stores the result, returning
// the content EditOutcome and the number of names changed. Library callers that do
// not want to spawn an editor should use PageBuffer + ApplyPage instead. Needs git
// on PATH.
func (c *Client) EditPage(pageID, editor string) (EditOutcome, int, error) {
	return edit.Page(c.arch, pageID, editor)
}

// PageBuffer renders a page's editable transcription buffer: the per-region title/
// link name header (only when the page has regions) plus the current content. It is
// the non-interactive form of the CLI's analyze-edit — no editor is spawned. Feed an
// edited buffer back to ApplyPage.
func (c *Client) PageBuffer(pageID string) (string, error) {
	return edit.Serialize(c.arch, pageID)
}

// ApplyPage stores an edited page buffer (as produced by PageBuffer): the content
// becomes the effective transcription — its divergence from the AI base is kept so
// the edit survives re-analysis — and each changed title/link name becomes a user
// override. It returns the content EditOutcome and the number of names changed. A
// malformed buffer writes nothing. Needs git on PATH.
func (c *Client) ApplyPage(pageID, buffer string) (EditOutcome, int, error) {
	return edit.Apply(c.arch, pageID, buffer)
}
