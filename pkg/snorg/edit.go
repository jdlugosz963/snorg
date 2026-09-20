package snorg

import "github.com/jdlugosz963/snorg/internal/edit"

// EditorFromEnv returns the editor command line from $VISUAL, else $EDITOR — the
// editor an interactive front-end passes to EditPage.
func EditorFromEnv() (string, error) { return edit.EditorFromEnv() }

// EditPage is the interactive convenience behind the CLI's analyze-edit: it opens
// the page buffer in editor (a shell command line) and stores the result. The
// returned PageEdit says what the round-trip changed — the content TextChange, the
// per-box breakdown on a templated page, and the title/link name overrides. Library
// callers that do not want to spawn an editor should use PageBuffer + ApplyPage.
func (c *Client) EditPage(pageID, editor string) (PageEdit, error) {
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
// override. It returns the same PageEdit account as EditPage. A malformed buffer
// writes nothing.
func (c *Client) ApplyPage(pageID, buffer string) (PageEdit, error) {
	return edit.Apply(c.arch, pageID, buffer)
}
