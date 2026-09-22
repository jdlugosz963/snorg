package archive

// The shared "what changed" vocabulary. Like WriteReport (archive.go), these are
// orthogonal facts about one operation — sparse and cheap, a zero value meaning
// the operation never touched that thing. analyze and edit each assemble their own
// result from these parts; neither owns them, so one front-end formats both.
//
// The parts describe *what moved*, never *how it was produced*: a cost fact only
// one write path can produce (an LLM call skipped, a blank crop) belongs on that
// path's own type, so no field here is permanently zero for some caller.

import "github.com/jdlugosz963/snorg/internal/textmerge"

// TextKind classifies a transition of one text document: a page's whole content,
// or a single template region's text.
type TextKind string

const (
	TextUnchanged TextKind = "unchanged" // before == after
	TextNew       TextKind = "new"       // there was nothing before
	TextUpdated   TextKind = "updated"   // both sides non-empty and different
	TextCleared   TextKind = "cleared"   // there is nothing after
	TextReverted  TextKind = "reverted"  // the after side is exactly the AI base
)

// TextChange is one text document's transition. Was/Now hold the actual text in
// its stored form (NormMD) — the change itself rather than a summary of it, so a
// caller renders or re-diffs it (see the public TextDiff/TextStat) without going
// back to the archive. Conflicts means the text carries 3-way-merge conflict
// markers. A zero TextChange (Kind == "") is one nobody computed, which is not
// the same as TextUnchanged.
type TextChange struct {
	Kind      TextKind
	Conflicts bool
	Was, Now  string
}

// Changed reports whether the text actually moved — the filter a reporter applies
// before saying anything about it.
func (c TextChange) Changed() bool { return c.Kind != "" && c.Kind != TextUnchanged }

// TextChangeOf classifies the transition was → now. Both sides are normalized
// (NormMD) first, so the verdict matches the bytes on disk. Conflicts is set by
// sniffing now for merge markers; a caller holding the structural answer ORs it in.
//
// It never reports TextReverted: returning to the AI base is a property of the
// write, not of the two texts, and only WriteAnalysisEdit knows that base.
func TextChangeOf(was, now string) TextChange {
	w, n := NormMD(was), NormMD(now)
	c := TextChange{Was: w, Now: n, Conflicts: textmerge.HasConflicts(n)}
	switch {
	case w == n:
		c.Kind = TextUnchanged
	case n == "":
		c.Kind = TextCleared
	case w == "":
		c.Kind = TextNew
	default:
		c.Kind = TextUpdated
	}
	return c
}

// NameChange is one title/link name that moved. Index is 1-based and matches the
// analyze-edit buffer's <!-- title N --> / <!-- link N --> markers, so a message
// about it names the thing the user would edit. Override means the new value was
// stored as a user override (analysis.edited), which analyze then honors.
type NameChange struct {
	Kind     string // "title" | "link"
	Index    int
	Was, Now string
	Override bool
}

// RegionChange is one template box's text change on a templated page. Label is
// informational (the box's config label); ID is the key the region sections in
// <PAGEID>.md are stored under.
type RegionChange struct {
	ID, Label string
	Text      TextChange
}
