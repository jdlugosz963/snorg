package edit

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/jdlugosz963/snorg/internal/archive"
	"github.com/jdlugosz963/snorg/internal/snote"
)

// editArchive builds an archive holding one note with one page Pa.
func editArchive(t *testing.T) *archive.Archive {
	t.Helper()
	a := archive.New(t.TempDir())
	n := &snote.Note{FileID: "F_TEST", Pages: []snote.Page{{ID: "Pa", Number: 1}}}
	if _, err := a.Write(n, map[string][]byte{"Pa": []byte("<svg/>")}); err != nil {
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
	res, err := Page(a, "Pa", editor)
	if err != nil {
		t.Fatal(err)
	}
	if res.Content.Kind != archive.TextUpdated {
		t.Errorf("content = %+v, want %q", res.Content, archive.TextUpdated)
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

	res, err := Page(a, "Pa", fakeEditor(t, "true"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Content.Kind != archive.TextUnchanged {
		t.Errorf("content = %+v, want %q", res.Content, archive.TextUnchanged)
	}
}

func TestPageRevertRemovesDiff(t *testing.T) {
	a := editArchive(t)
	base := "# ai output\n"
	if err := a.WriteAnalysisMD("F_TEST", "Pa", base); err != nil {
		t.Fatal(err)
	}
	if _, err := Page(a, "Pa", fakeEditor(t, `printf 'edited\n' > "$1"`)); err != nil {
		t.Fatal(err)
	}

	res, err := Page(a, "Pa", fakeEditor(t, `printf '# ai output\n' > "$1"`))
	if err != nil {
		t.Fatal(err)
	}
	if res.Content.Kind != archive.TextReverted {
		t.Errorf("content = %+v, want %q", res.Content, archive.TextReverted)
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

	if _, err := Page(a, "Pa", fakeEditor(t, `printf 'half-finished\n' > "$1"; exit 1`)); err == nil {
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
	res, err := Page(a, "Pa", editor)
	if err != nil {
		t.Fatal(err)
	}
	if res.Content.Kind != archive.TextNew {
		t.Errorf("content = %+v, want %q", res.Content, archive.TextNew)
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
	res, err := Page(a, "Pa", fakeEditor(t, `printf '\n' > "$1"`))
	if err != nil {
		t.Fatal(err)
	}
	if res.Content.Kind != archive.TextUnchanged {
		t.Errorf("content = %+v, want %q", res.Content, archive.TextUnchanged)
	}
	if _, err := os.Stat(filepath.Join(a.Root, "F_TEST", "Pa.md")); !os.IsNotExist(err) {
		t.Errorf("md created for an empty save: %v", err)
	}
}

func TestPageClearingHumanTranscriptionRemovesFiles(t *testing.T) {
	a := editArchive(t)
	if _, err := Page(a, "Pa", fakeEditor(t, `printf 'written by hand\n' > "$1"`)); err != nil {
		t.Fatal(err)
	}

	// Emptying a page that has no AI base removes the transcription entirely:
	// no empty sidecars left behind.
	res, err := Page(a, "Pa", fakeEditor(t, `printf '' > "$1"`))
	if err != nil {
		t.Fatal(err)
	}
	// The page had no AI base, so emptying it is a clear, not a revert — the more
	// informative verdict for the same disk outcome.
	if res.Content.Kind != archive.TextCleared {
		t.Errorf("content = %+v, want %q", res.Content, archive.TextCleared)
	}
	for _, name := range []string{"Pa.md", "Pa.md.diff"} {
		if _, err := os.Stat(filepath.Join(a.Root, "F_TEST", name)); !os.IsNotExist(err) {
			t.Errorf("%s left behind: %v", name, err)
		}
	}
}

func TestPageUnknownPageID(t *testing.T) {
	a := editArchive(t)
	if _, err := Page(a, "Pmissing", fakeEditor(t, "true")); err == nil {
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
	if _, err := a.Write(n, map[string][]byte{"Pa": []byte("<svg/>")}); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestPageEditsNames(t *testing.T) {
	a := regionArchive(t)

	// The content section carries a blank line, which must survive verbatim.
	editor := fakeEditor(t, `printf '<!-- title 1 -->\nFixed Title\n<!-- link 1 -->\nFixed Link\n\n<!-- content -->\n# heading\n\nbody text\n' > "$1"`)
	res, err := Page(a, "Pa", editor)
	if err != nil {
		t.Fatal(err)
	}
	if res.Content.Kind != archive.TextNew {
		t.Errorf("content = %+v, want %q", res.Content, archive.TextNew)
	}
	// The names are reported individually — kind, 1-based index (the buffer's own
	// marker key), what they were and what they became — not just counted.
	wantNames := []archive.NameChange{
		{Kind: "title", Index: 1, Was: "", Now: "Fixed Title", Override: true},
		{Kind: "link", Index: 1, Was: "", Now: "Fixed Link", Override: true},
	}
	if !reflect.DeepEqual(res.Names, wantNames) {
		t.Errorf("names = %+v, want %+v", res.Names, wantNames)
	}
	if !res.JSONChanged {
		t.Error("JSONChanged = false, want the name overrides to have changed the page json")
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
	res, err := Page(a, "Pa", editor)
	if err != nil {
		t.Fatal(err)
	}
	// The content axis and the name axis are independent: nothing moved in the
	// text while a name did.
	if res.Content.Kind != archive.TextUnchanged {
		t.Errorf("content = %+v, want %q", res.Content, archive.TextUnchanged)
	}
	if len(res.Names) != 1 || res.Names[0].Kind != "title" || res.Names[0].Index != 1 {
		t.Errorf("names = %+v, want one title 1 change", res.Names)
	}
	if md, err := a.ReadAnalysisMD("F_TEST", "Pa"); err != nil || md != "the body\n" {
		t.Errorf("content changed: md = %q, %v", md, err)
	}
}

func TestPageBadHeaderSavesNothing(t *testing.T) {
	a := regionArchive(t)

	// Drop the required title marker: parse must fail and nothing is written.
	editor := fakeEditor(t, `printf '<!-- link 1 -->\nX\n<!-- content -->\nbody\n' > "$1"`)
	if _, err := Page(a, "Pa", editor); err == nil {
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

// Apply now calls WriteAnalysisEdit unconditionally rather than guarding the
// unchanged case, so pin that the writers really are byte-level no-ops: an
// untouched buffer must leave both sidecars exactly as they were.
func TestApplyUnchangedTouchesNothing(t *testing.T) {
	a := editArchive(t)
	base := "# ai output\n"
	if err := a.WriteAnalysisMD("F_TEST", "Pa", base); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(a, "Pa", "# ai output\nedited by hand\n"); err != nil {
		t.Fatal(err)
	}

	mdPath := filepath.Join(a.Root, "F_TEST", "Pa.md")
	diffPath := mdPath + ".diff"
	mdBefore, err := os.Stat(mdPath)
	if err != nil {
		t.Fatal(err)
	}
	diffBefore, err := os.Stat(diffPath)
	if err != nil {
		t.Fatalf("precondition: the edit must have written a diff sidecar: %v", err)
	}

	buf, err := Serialize(a, "Pa")
	if err != nil {
		t.Fatal(err)
	}
	res, err := Apply(a, "Pa", buf)
	if err != nil {
		t.Fatal(err)
	}
	if res.Content.Kind != archive.TextUnchanged {
		t.Errorf("content = %+v, want %q", res.Content, archive.TextUnchanged)
	}
	mdAfter, err := os.Stat(mdPath)
	if err != nil {
		t.Fatal(err)
	}
	diffAfter, err := os.Stat(diffPath)
	if err != nil {
		t.Fatalf("diff sidecar removed by a no-op save: %v", err)
	}
	if !mdAfter.ModTime().Equal(mdBefore.ModTime()) {
		t.Error("md rewritten by a no-op save")
	}
	if !diffAfter.ModTime().Equal(diffBefore.ModTime()) {
		t.Error("diff sidecar rewritten by a no-op save")
	}
}

// A text-only edit leaves the page JSON's own fields alone but still moves its
// modified stamp; a no-op save moves nothing.
func TestApplyStampsModified(t *testing.T) {
	a := editArchive(t)
	clock := time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)
	a.Now = func() time.Time { return clock }
	stamp := func() time.Time {
		t.Helper()
		pd, err := a.ReadPage("F_TEST", "Pa")
		if err != nil {
			t.Fatal(err)
		}
		return pd.ModifiedAt
	}

	res, err := Apply(a, "Pa", "hand-written\n")
	if err != nil {
		t.Fatal(err)
	}
	if !res.JSONChanged || !stamp().Equal(clock) {
		t.Errorf("text edit: JSONChanged=%v ModifiedAt=%v, want true/%v", res.JSONChanged, stamp(), clock)
	}

	edited := clock
	clock = clock.Add(24 * time.Hour)
	buf, err := Serialize(a, "Pa")
	if err != nil {
		t.Fatal(err)
	}
	if res, err = Apply(a, "Pa", buf); err != nil {
		t.Fatal(err)
	}
	if res.JSONChanged || !stamp().Equal(edited) {
		t.Errorf("no-op save: JSONChanged=%v ModifiedAt=%v, want false/%v", res.JSONChanged, stamp(), edited)
	}
}
