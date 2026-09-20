package snorg

// The change vocabulary's function surface: how a TextChange is built, and the two
// ways to render one. The types themselves are aliased in types.go.

import (
	"github.com/jdlugosz963/snorg/internal/archive"
	"github.com/jdlugosz963/snorg/internal/textmerge"
)

// TextChangeOf classifies the transition was → now — new, updated, cleared or
// unchanged — and carries both texts. It never reports TextReverted: returning to
// the AI base is a property of a write, not of two texts, so only an edit that
// lands on the stored base can produce that verdict.
func TextChangeOf(was, now string) TextChange { return archive.TextChangeOf(was, now) }

// TextDiff renders a TextChange as a unified diff of its before and after text —
// the human form of the change, for a front-end that wants to show it. Empty when
// nothing changed.
func TextDiff(c TextChange) (string, error) { return textmerge.Diff(c.Was, c.Now) }

// TextStat counts the lines a TextChange added and removed. It is derived from
// Was/Now on demand rather than stored on the struct, so a caller that never
// displays a change never pays for its diff.
func TextStat(c TextChange) (added, removed int, err error) {
	return textmerge.Stat(c.Was, c.Now)
}
