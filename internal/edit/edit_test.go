package edit

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jdlugosz963/snorg/internal/archive"
	"github.com/jdlugosz963/snorg/internal/snote"
)

// editArchive builds an archive holding one note with one page Pa.
func editArchive(t *testing.T) *archive.Archive {
	t.Helper()
	a := archive.New(t.TempDir())
	n := &snote.Note{FileID: "F_TEST", Pages: []snote.Page{{ID: "Pa", Number: 1}}}
	if err := a.Write(n, map[string][]byte{"Pa": []byte("<svg/>")}); err != nil {
		t.Fatal(err)
	}
	return a
}

// fakeEditor writes an executable shell script into a temp dir and returns it
// as the editor command line, proving the sh -c invocation end to end.
func fakeEditor(t *testing.T, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "editor.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestPageEditCreatesDiffAndKeepsBase(t *testing.T) {
	a := editArchive(t)
	base := "# ai output\n\nbody\n"
	if err := a.WriteAnalysisMD("F_TEST", "Pa", base); err != nil {
		t.Fatal(err)
	}

	editor := fakeEditor(t, `printf '# ai output\n\nbody, edited by hand\n' > "$1"`)
	outcome, _, err := Page(a, "Pa", editor)
	if err != nil {
		t.Fatal(err)
	}
	if outcome != Edited {
		t.Errorf("outcome = %q, want %q", outcome, Edited)
	}
	if md, err := a.ReadAnalysisMD("F_TEST", "Pa"); err != nil || md != "# ai output\n\nbody, edited by hand\n" {
		t.Errorf("md = %q, %v", md, err)
	}
	if got, err := a.ReadAnalysisBase("F_TEST", "Pa"); err != nil || got != base {
		t.Errorf("base = %q, %v, want %q", got, err, base)
	}
}

func TestPageUnchangedWritesNothing(t *testing.T) {
	a := editArchive(t)
	if err := a.WriteAnalysisMD("F_TEST", "Pa", "# ai output\n"); err != nil {
		t.Fatal(err)
	}

	outcome, _, err := Page(a, "Pa", fakeEditor(t, "true"))
	if err != nil {
		t.Fatal(err)
	}
	if outcome != Unchanged {
		t.Errorf("outcome = %q, want %q", outcome, Unchanged)
	}
}

func TestPageRevertRemovesDiff(t *testing.T) {
	a := editArchive(t)
	base := "# ai output\n"
	if err := a.WriteAnalysisMD("F_TEST", "Pa", base); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Page(a, "Pa", fakeEditor(t, `printf 'edited\n' > "$1"`)); err != nil {
		t.Fatal(err)
	}

	outcome, _, err := Page(a, "Pa", fakeEditor(t, `printf '# ai output\n' > "$1"`))
	if err != nil {
		t.Fatal(err)
	}
	if outcome != Reverted {
		t.Errorf("outcome = %q, want %q", outcome, Reverted)
	}
	if base2, err := a.ReadAnalysisBase("F_TEST", "Pa"); err != nil || base2 != base {
		t.Errorf("base = %q, %v, want %q", base2, err, base)
	}
}

func TestPageEditorFailureLeavesPageUntouched(t *testing.T) {
	a := editArchive(t)
	if err := a.WriteAnalysisMD("F_TEST", "Pa", "# ai output\n"); err != nil {
		t.Fatal(err)
	}

	if _, _, err := Page(a, "Pa", fakeEditor(t, `printf 'half-finished\n' > "$1"; exit 1`)); err == nil {
		t.Fatal("expected error from a failing editor")
	}
	if md, err := a.ReadAnalysisMD("F_TEST", "Pa"); err != nil || md != "# ai output\n" {
		t.Errorf("md modified despite editor failure: %q, %v", md, err)
	}
}

func TestPageHumanTranscription(t *testing.T) {
	a := editArchive(t)

	// Never analyzed: the editor opens empty and the saved text becomes the
	// page's transcription, with an empty AI base behind it.
	editor := fakeEditor(t, `if [ -s "$1" ]; then exit 1; fi; printf 'written by hand\n' > "$1"`)
	outcome, _, err := Page(a, "Pa", editor)
	if err != nil {
		t.Fatal(err)
	}
	if outcome != Edited {
		t.Errorf("outcome = %q, want %q", outcome, Edited)
	}
	if md, err := a.ReadAnalysisMD("F_TEST", "Pa"); err != nil || md != "written by hand\n" {
		t.Errorf("md = %q, %v", md, err)
	}
	if base, err := a.ReadAnalysisBase("F_TEST", "Pa"); err != nil || base != "" {
		t.Errorf("base = %q, %v, want empty", base, err)
	}
}

func TestPageEmptySaveOnEmptyPage(t *testing.T) {
	a := editArchive(t)

	// Editors like vim save an "empty" buffer as a single newline; that must
	// still count as no content and create no files.
	outcome, _, err := Page(a, "Pa", fakeEditor(t, `printf '\n' > "$1"`))
	if err != nil {
		t.Fatal(err)
	}
	if outcome != Unchanged {
		t.Errorf("outcome = %q, want %q", outcome, Unchanged)
	}
	if _, err := os.Stat(filepath.Join(a.Root, "F_TEST", "Pa.md")); !os.IsNotExist(err) {
		t.Errorf("md created for an empty save: %v", err)
	}
}

func TestPageClearingHumanTranscriptionRemovesFiles(t *testing.T) {
	a := editArchive(t)
	if _, _, err := Page(a, "Pa", fakeEditor(t, `printf 'written by hand\n' > "$1"`)); err != nil {
		t.Fatal(err)
	}

	// Emptying a page that has no AI base removes the transcription entirely:
	// no empty sidecars left behind.
	outcome, _, err := Page(a, "Pa", fakeEditor(t, `printf '' > "$1"`))
	if err != nil {
		t.Fatal(err)
	}
	if outcome != Reverted {
		t.Errorf("outcome = %q, want %q", outcome, Reverted)
	}
	for _, name := range []string{"Pa.md", "Pa.md.diff"} {
		if _, err := os.Stat(filepath.Join(a.Root, "F_TEST", name)); !os.IsNotExist(err) {
			t.Errorf("%s left behind: %v", name, err)
		}
	}
}

func TestPageUnknownPageID(t *testing.T) {
	a := editArchive(t)
	if _, _, err := Page(a, "Pmissing", fakeEditor(t, "true")); err == nil {
		t.Fatal("expected error for unknown PAGEID")
	}
}

// regionArchive builds an archive whose page Pa has one title and one link, so
// the analyze-edit buffer carries a name header.
func regionArchive(t *testing.T) *archive.Archive {
	t.Helper()
	a := archive.New(t.TempDir())
	n := &snote.Note{FileID: "F_TEST", Pages: []snote.Page{{
		ID:     "Pa",
		Number: 1,
		Titles: []snote.Title{{Rect: snote.Rect{X: 1, Y: 2, W: 3, H: 4}, Level: 1}},
		Links:  []snote.Link{{Rect: snote.Rect{X: 5, Y: 6, W: 7, H: 8}, Name: "NoteB", TargetPageID: "P2", TargetFileID: "F_OTHER"}},
	}}}
	if err := a.Write(n, map[string][]byte{"Pa": []byte("<svg/>")}); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestPageEditsNames(t *testing.T) {
	a := regionArchive(t)

	// The content section carries a blank line, which must survive verbatim.
	editor := fakeEditor(t, `printf '<!-- title 1 -->\nFixed Title\n<!-- link 1 -->\nFixed Link\n\n<!-- content -->\n# heading\n\nbody text\n' > "$1"`)
	outcome, namesChanged, err := Page(a, "Pa", editor)
	if err != nil {
		t.Fatal(err)
	}
	if outcome != Edited {
		t.Errorf("outcome = %q, want %q", outcome, Edited)
	}
	if namesChanged != 2 {
		t.Errorf("namesChanged = %d, want 2", namesChanged)
	}

	pd, err := a.ReadPage("F_TEST", "Pa")
	if err != nil {
		t.Fatal(err)
	}
	if pd.Titles[0].Analysis == nil || pd.Titles[0].Analysis.Name != "Fixed Title" || !pd.Titles[0].Analysis.Edited {
		t.Errorf("title analysis = %+v, want name override marked Edited", pd.Titles[0].Analysis)
	}
	if pd.Links[0].Analysis == nil || pd.Links[0].Analysis.Name != "Fixed Link" || !pd.Links[0].Analysis.Edited {
		t.Errorf("link analysis = %+v, want name override marked Edited", pd.Links[0].Analysis)
	}
	if md, err := a.ReadAnalysisMD("F_TEST", "Pa"); err != nil || md != "# heading\n\nbody text\n" {
		t.Errorf("md = %q, %v", md, err)
	}
}

func TestPageNameOnlyEditKeepsContent(t *testing.T) {
	a := regionArchive(t)
	if err := a.WriteAnalysisMD("F_TEST", "Pa", "the body\n"); err != nil {
		t.Fatal(err)
	}

	// Change only the title name; leave the content section as serialized.
	editor := fakeEditor(t, `printf '<!-- title 1 -->\nRenamed\n<!-- link 1 -->\n\n<!-- content -->\nthe body\n' > "$1"`)
	outcome, namesChanged, err := Page(a, "Pa", editor)
	if err != nil {
		t.Fatal(err)
	}
	if outcome != Unchanged {
		t.Errorf("content outcome = %q, want %q", outcome, Unchanged)
	}
	if namesChanged != 1 {
		t.Errorf("namesChanged = %d, want 1", namesChanged)
	}
	if md, err := a.ReadAnalysisMD("F_TEST", "Pa"); err != nil || md != "the body\n" {
		t.Errorf("content changed: md = %q, %v", md, err)
	}
}

func TestPageBadHeaderSavesNothing(t *testing.T) {
	a := regionArchive(t)

	// Drop the required title marker: parse must fail and nothing is written.
	editor := fakeEditor(t, `printf '<!-- link 1 -->\nX\n<!-- content -->\nbody\n' > "$1"`)
	if _, _, err := Page(a, "Pa", editor); err == nil {
		t.Fatal("expected a parse error for a malformed header")
	}
	if _, err := os.Stat(filepath.Join(a.Root, "F_TEST", "Pa.md")); !os.IsNotExist(err) {
		t.Errorf("md written despite malformed header: %v", err)
	}
	pd, err := a.ReadPage("F_TEST", "Pa")
	if err != nil {
		t.Fatal(err)
	}
	if pd.Titles[0].Analysis != nil || pd.Links[0].Analysis != nil {
		t.Errorf("names written despite malformed header: %+v %+v", pd.Titles[0].Analysis, pd.Links[0].Analysis)
	}
}

func TestEditorFromEnv(t *testing.T) {
	t.Setenv("VISUAL", "visual-editor")
	t.Setenv("EDITOR", "plain-editor")
	if ed, err := EditorFromEnv(); err != nil || ed != "visual-editor" {
		t.Errorf("VISUAL wins: got %q, %v", ed, err)
	}
	t.Setenv("VISUAL", "")
	if ed, err := EditorFromEnv(); err != nil || ed != "plain-editor" {
		t.Errorf("EDITOR fallback: got %q, %v", ed, err)
	}
	t.Setenv("EDITOR", "")
	if _, err := EditorFromEnv(); err == nil {
		t.Error("expected error with neither VISUAL nor EDITOR")
	}
}
