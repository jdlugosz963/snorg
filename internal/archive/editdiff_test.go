package archive

import (
	"os"
	"strings"
	"testing"
)

// editArchive builds an archive holding one note with one page Pa.
func editArchive(t *testing.T) *Archive {
	t.Helper()
	a := New(t.TempDir())
	if _, err := a.Write(note("Pa"), svgMap(map[string]string{"Pa": "<svg/>"})); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestReadAnalysisBaseWithoutEdits(t *testing.T) {
	a := editArchive(t)

	// No md at all.
	if base, err := a.ReadAnalysisBase("F_TEST", "Pa"); err != nil || base != "" {
		t.Errorf("no md: base = %q, %v, want empty", base, err)
	}

	// md without a diff is its own base.
	if err := a.WriteAnalysisMD("F_TEST", "Pa", "# ai output"); err != nil {
		t.Fatal(err)
	}
	if base, err := a.ReadAnalysisBase("F_TEST", "Pa"); err != nil || base != "# ai output\n" {
		t.Errorf("md, no diff: base = %q, %v", base, err)
	}
}

func TestWriteAnalysisEditRoundTrip(t *testing.T) {
	a := editArchive(t)
	if err := a.WriteAnalysisMD("F_TEST", "Pa", "# ai output\n\nbody"); err != nil {
		t.Fatal(err)
	}

	base := "# ai output\n\nbody\n"
	if _, err := a.WriteAnalysisEdit("F_TEST", "Pa", base, "# ai output\n\nbody, fixed by hand"); err != nil {
		t.Fatal(err)
	}
	if md, err := a.ReadAnalysisMD("F_TEST", "Pa"); err != nil || md != "# ai output\n\nbody, fixed by hand\n" {
		t.Errorf("md = %q, %v", md, err)
	}
	if _, err := os.Stat(a.diffPath("F_TEST", "Pa")); err != nil {
		t.Fatalf("edit diff not written: %v", err)
	}
	if got, err := a.ReadAnalysisBase("F_TEST", "Pa"); err != nil || got != base {
		t.Errorf("reconstructed base = %q, %v, want %q", got, err, base)
	}
}

func TestWriteAnalysisEditRevertRemovesDiff(t *testing.T) {
	a := editArchive(t)
	base := "# ai output\n"
	if err := a.WriteAnalysisMD("F_TEST", "Pa", base); err != nil {
		t.Fatal(err)
	}
	if _, err := a.WriteAnalysisEdit("F_TEST", "Pa", base, "edited\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.WriteAnalysisEdit("F_TEST", "Pa", base, base); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(a.diffPath("F_TEST", "Pa")); !os.IsNotExist(err) {
		t.Errorf("edit diff not removed on revert: %v", err)
	}
	if md, err := a.ReadAnalysisMD("F_TEST", "Pa"); err != nil || md != base {
		t.Errorf("md = %q, %v, want %q", md, err, base)
	}
}

func TestMergeAnalysisWithoutEdits(t *testing.T) {
	a := editArchive(t)
	if err := a.WriteAnalysisMD("F_TEST", "Pa", "old ai\n"); err != nil {
		t.Fatal(err)
	}
	c, err := a.MergeAnalysis("F_TEST", "Pa", "new ai")
	if err != nil {
		t.Fatal(err)
	}
	if c.Conflicts {
		t.Error("unexpected c.Conflicts")
	}
	// Now is the stored form (NormMD), so it matches the md on disk byte for byte.
	if c.Now != "new ai\n" {
		t.Errorf("effective = %q, want %q", c.Now, "new ai\n")
	}
	if c.Kind != TextUpdated {
		t.Errorf("Kind = %q, want %q", c.Kind, TextUpdated)
	}
	if c.Was != "old ai\n" {
		t.Errorf("Was = %q, want %q", c.Was, "old ai\n")
	}
	if md, err := a.ReadAnalysisMD("F_TEST", "Pa"); err != nil || md != "new ai\n" {
		t.Errorf("md = %q, %v", md, err)
	}
}

func TestMergeAnalysisRebasesEdits(t *testing.T) {
	a := editArchive(t)
	base := "intro\nline one\nline two\noutro\n"
	if err := a.WriteAnalysisMD("F_TEST", "Pa", base); err != nil {
		t.Fatal(err)
	}
	if _, err := a.WriteAnalysisEdit("F_TEST", "Pa", base, "intro\nline one, edited\nline two\noutro\n"); err != nil {
		t.Fatal(err)
	}

	theirs := "intro\nline one\nline two\noutro, reanalyzed\n"
	c, err := a.MergeAnalysis("F_TEST", "Pa", theirs)
	if err != nil {
		t.Fatal(err)
	}
	if c.Conflicts {
		t.Fatalf("unexpected c.Conflicts:\n%s", c.Now)
	}
	want := "intro\nline one, edited\nline two\noutro, reanalyzed\n"
	if c.Now != want {
		t.Errorf("effective = %q, want %q", c.Now, want)
	}
	// theirs became the new base: the user's edit was rebased onto it.
	if newBase, err := a.ReadAnalysisBase("F_TEST", "Pa"); err != nil || newBase != theirs {
		t.Errorf("new base = %q, %v, want %q", newBase, err, theirs)
	}
}

func TestMergeAnalysisDropsDiffWhenAbsorbed(t *testing.T) {
	a := editArchive(t)
	base := "line\n"
	if err := a.WriteAnalysisMD("F_TEST", "Pa", base); err != nil {
		t.Fatal(err)
	}
	if _, err := a.WriteAnalysisEdit("F_TEST", "Pa", base, "line, edited\n"); err != nil {
		t.Fatal(err)
	}

	// The new analysis independently matches the user's version (e.g. the user
	// corrected the page itself): the diff must disappear.
	c, err := a.MergeAnalysis("F_TEST", "Pa", "line, edited\n")
	if err != nil {
		t.Fatal(err)
	}
	if c.Conflicts {
		t.Fatalf("unexpected c.Conflicts:\n%s", c.Now)
	}
	if _, err := os.Stat(a.diffPath("F_TEST", "Pa")); !os.IsNotExist(err) {
		t.Errorf("edit diff not removed when merge equals theirs: %v", err)
	}
}

func TestMergeAnalysisConflict(t *testing.T) {
	a := editArchive(t)
	base := "line\n"
	if err := a.WriteAnalysisMD("F_TEST", "Pa", base); err != nil {
		t.Fatal(err)
	}
	if _, err := a.WriteAnalysisEdit("F_TEST", "Pa", base, "line, edited\n"); err != nil {
		t.Fatal(err)
	}

	c, err := a.MergeAnalysis("F_TEST", "Pa", "line, reanalyzed\n")
	if err != nil {
		t.Fatal(err)
	}
	if !c.Conflicts {
		t.Fatalf("expected c.Conflicts, c.Now %q", c.Now)
	}
	md, err := a.ReadAnalysisMD("F_TEST", "Pa")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(md, "<<<<<<< edited") || !strings.Contains(md, ">>>>>>> reanalyzed") {
		t.Errorf("conflict markers missing from md:\n%s", md)
	}
	// The conflicted md still maps back to theirs, so a re-analysis before the
	// user resolves keeps working.
	if newBase, err := a.ReadAnalysisBase("F_TEST", "Pa"); err != nil || newBase != "line, reanalyzed\n" {
		t.Errorf("new base = %q, %v", newBase, err)
	}
}

func TestReadAnalysisBaseRejectsForeignMDEdit(t *testing.T) {
	a := editArchive(t)
	base := "line one\nline two\n"
	if err := a.WriteAnalysisMD("F_TEST", "Pa", base); err != nil {
		t.Fatal(err)
	}
	if _, err := a.WriteAnalysisEdit("F_TEST", "Pa", base, "line one, edited\nline two\n"); err != nil {
		t.Fatal(err)
	}
	// The md changes behind the tool's back: the stored diff no longer applies.
	if err := a.WriteAnalysisMD("F_TEST", "Pa", "rewritten outside analyze-edit\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ReadAnalysisBase("F_TEST", "Pa"); err == nil {
		t.Fatal("expected error when the diff no longer applies")
	} else if !strings.Contains(err.Error(), "remove Pa.md.diff") {
		t.Errorf("error lacks the recovery hint: %v", err)
	}
}

// TestWritePrunesEditDiff: removing a page from the note removes its edit diff
// with every other page artifact; a re-ingest keeping the page preserves it.
func TestWritePrunesEditDiff(t *testing.T) {
	a := editArchive(t)
	base := "line\n"
	if err := a.WriteAnalysisMD("F_TEST", "Pa", base); err != nil {
		t.Fatal(err)
	}
	if _, err := a.WriteAnalysisEdit("F_TEST", "Pa", base, "line, edited\n"); err != nil {
		t.Fatal(err)
	}

	if _, err := a.Write(note("Pa"), svgMap(map[string]string{"Pa": "<svg/>"})); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(a.diffPath("F_TEST", "Pa")); err != nil {
		t.Errorf("edit diff not preserved across re-ingest: %v", err)
	}

	if _, err := a.Write(note("Pb"), svgMap(map[string]string{"Pb": "<svg/>"})); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(a.diffPath("F_TEST", "Pa")); !os.IsNotExist(err) {
		t.Errorf("edit diff not pruned with its page: %v", err)
	}
}

// The writers report what they did, so a caller never re-derives it. Reverting is
// WriteAnalysisEdit's verdict alone: it is the one writer holding the AI base.
func TestWriteAnalysisEditReportsTheChange(t *testing.T) {
	a := editArchive(t)
	base := "ai output\n"

	c, err := a.WriteAnalysisEdit("F_TEST", "Pa", "", base)
	if err != nil {
		t.Fatal(err)
	}
	if c.Kind != TextNew || c.Was != "" || c.Now != base {
		t.Errorf("first write = %+v, want new with Was empty and Now %q", c, base)
	}

	c, err = a.WriteAnalysisEdit("F_TEST", "Pa", base, "edited\n")
	if err != nil {
		t.Fatal(err)
	}
	if c.Kind != TextUpdated || c.Was != base {
		t.Errorf("edit = %+v, want updated from %q", c, base)
	}

	c, err = a.WriteAnalysisEdit("F_TEST", "Pa", base, base)
	if err != nil {
		t.Fatal(err)
	}
	if c.Kind != TextReverted {
		t.Errorf("revert = %+v, want %q", c, TextReverted)
	}

	// Rewriting the same content is not a revert — it never moved.
	c, err = a.WriteAnalysisEdit("F_TEST", "Pa", base, base)
	if err != nil {
		t.Fatal(err)
	}
	if c.Kind != TextUnchanged {
		t.Errorf("no-op write = %+v, want %q", c, TextUnchanged)
	}
}

// A fresh analyze writes base == content, which must not read as a revert.
func TestMergeAnalysisNeverReverts(t *testing.T) {
	a := editArchive(t)
	c, err := a.MergeAnalysis("F_TEST", "Pa", "first transcription\n")
	if err != nil {
		t.Fatal(err)
	}
	if c.Kind != TextNew {
		t.Errorf("fresh merge = %q, want %q", c.Kind, TextNew)
	}
}

// A conflicted merge reports Conflicts on top of an ordinary kind — the two are
// separate axes, so the caller learns both what moved and that it needs resolving.
func TestMergeAnalysisReportsConflictsWithKind(t *testing.T) {
	a := editArchive(t)
	base := "line\n"
	if err := a.WriteAnalysisMD("F_TEST", "Pa", base); err != nil {
		t.Fatal(err)
	}
	if _, err := a.WriteAnalysisEdit("F_TEST", "Pa", base, "line, edited\n"); err != nil {
		t.Fatal(err)
	}
	c, err := a.MergeAnalysis("F_TEST", "Pa", "line, reanalyzed\n")
	if err != nil {
		t.Fatal(err)
	}
	if !c.Conflicts {
		t.Error("Conflicts = false, want true")
	}
	if c.Kind != TextUpdated {
		t.Errorf("Kind = %q, want %q", c.Kind, TextUpdated)
	}
}
