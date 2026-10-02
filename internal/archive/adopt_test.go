package archive

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/jdlugosz963/snorg/internal/snote"
)

// seedPage gives an archived page the state only snorg produces: an analysis, a tag,
// a transcription and a hand-edit. This is exactly what must survive a page moving to
// another note — everything else is rebuilt from the .note.
func seedPage(t *testing.T, a *Archive, fileID, pageID, md string) {
	t.Helper()
	pd, err := a.ReadPage(fileID, pageID)
	if err != nil {
		t.Fatal(err)
	}
	pd.Analysis = &PageAnalysis{SourceHash: "hash-" + pageID, Fields: map[string]string{"summary": "s"}}
	pd.Tags = []string{"work"}
	if _, err := a.WritePage(fileID, pd, false); err != nil {
		t.Fatal(err)
	}
	if err := a.WriteAnalysisMD(fileID, pageID, md); err != nil {
		t.Fatal(err)
	}
	if err := a.writeEditDiff(fileID, pageID, "--- a\n+++ b\n"); err != nil {
		t.Fatal(err)
	}
}

func svgs(ids ...string) map[string][]byte {
	out := map[string][]byte{}
	for _, id := range ids {
		out[id] = []byte("<svg>" + id + "</svg>")
	}
	return out
}

// TestWriteAdoptsPageFromAnotherNote is the core case: a page moved to another note
// on the device must move within the archive too, carrying its analysis, tags and
// transcription, leaving nothing behind.
func TestWriteAdoptsPageFromAnotherNote(t *testing.T) {
	a := New(t.TempDir())
	if _, err := a.Write(noteIn("F_A", "Pa", "Pb", "Pc"), svgs("Pa", "Pb", "Pc")); err != nil {
		t.Fatal(err)
	}
	seedPage(t, a, "F_A", "Pc", "moved transcription")

	rep, err := a.Write(noteIn("F_B", "Pc"), svgs("Pc"))
	if err != nil {
		t.Fatal(err)
	}

	pc := findPageReport(t, rep, "Pc")
	if pc.New {
		t.Error("adopted page reported as New; it already had a transcription")
	}
	if want := []string{"F_A"}; !reflect.DeepEqual(pc.AdoptedFrom, want) {
		t.Errorf("AdoptedFrom = %v want %v", pc.AdoptedFrom, want)
	}

	pd, err := a.ReadPage("F_B", "Pc")
	if err != nil {
		t.Fatal(err)
	}
	if pd.Analysis == nil || pd.Analysis.SourceHash != "hash-Pc" {
		t.Errorf("analysis not carried: %+v", pd.Analysis)
	}
	if !reflect.DeepEqual(pd.Tags, []string{"work"}) {
		t.Errorf("tags = %v want [work]", pd.Tags)
	}
	md, err := a.ReadAnalysisMD("F_B", "Pc")
	if err != nil {
		t.Fatal(err)
	}
	if md != "moved transcription\n" {
		t.Errorf("md = %q want the donor's transcription", md)
	}
	if diff, err := a.readEditDiff("F_B", "Pc"); err != nil || diff == "" {
		t.Errorf("edit diff not carried: %q %v", diff, err)
	}

	for _, ext := range []string{".json", ".md", ".md.diff", ".svg"} {
		mustNotExist(t, filepath.Join(a.Root, "F_A", "Pc"+ext))
	}
	owner, err := a.FindPage("Pc")
	if err != nil || owner != "F_B" {
		t.Errorf("FindPage(Pc) = %q, %v want F_B", owner, err)
	}
}

// TestWriteAdoptionRepairsDonorNote: the note the page left must stop listing it
// immediately — query and retrieve iterate note.json and hard-error on a page file
// that is not there. The renumbering is what makes the repair converge: the donor's
// own next ingest then writes the same bytes and reports no change.
func TestWriteAdoptionRepairsDonorNote(t *testing.T) {
	a := New(t.TempDir())
	if _, err := a.Write(noteIn("F_A", "Pa", "Pb", "Pc"), svgs("Pa", "Pb", "Pc")); err != nil {
		t.Fatal(err)
	}
	seedPage(t, a, "F_A", "Pc", "text")

	rep, err := a.Write(noteIn("F_B", "Pc"), svgs("Pc"))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"F_A"}; !reflect.DeepEqual(rep.Repaired, want) {
		t.Errorf("Repaired = %v want %v", rep.Repaired, want)
	}
	nd, err := a.ReadNote("F_A")
	if err != nil {
		t.Fatal(err)
	}
	want := []NotePageRef{{ID: "Pa", Number: 1}, {ID: "Pb", Number: 2}}
	if !reflect.DeepEqual(nd.Pages, want) {
		t.Fatalf("donor pages = %+v want %+v", nd.Pages, want)
	}

	// The device's own view of F_A is now exactly this, so re-ingesting it changes
	// nothing. A gap left where Pc was would make this a second, spurious diff.
	rep, err = a.Write(noteIn("F_A", "Pa", "Pb"), svgs("Pa", "Pb"))
	if err != nil {
		t.Fatal(err)
	}
	if rep.NoteChanged {
		t.Error("re-ingesting the donor rewrote note.json; the repair did not converge")
	}
}

// TestWriteAdoptionCarriesRegionAnalyses: a title's transcription is matched by exact
// rect, and a move does not change the handwriting, so it must survive.
func TestWriteAdoptionCarriesRegionAnalyses(t *testing.T) {
	a := New(t.TempDir())
	r := snote.Rect{X: 10, Y: 20, W: 30, H: 40}
	src := noteWithTitle("Pa", r)
	src.FileID = "F_A"
	if _, err := a.Write(src, svgs("Pa")); err != nil {
		t.Fatal(err)
	}
	pd, err := a.ReadPage("F_A", "Pa")
	if err != nil {
		t.Fatal(err)
	}
	pd.Titles[0].Analysis = &TitleAnalysis{Name: "Heading"}
	if _, err := a.WritePage("F_A", pd, false); err != nil {
		t.Fatal(err)
	}

	dst := noteWithTitle("Pa", r)
	dst.FileID = "F_B"
	rep, err := a.Write(dst, svgs("Pa"))
	if err != nil {
		t.Fatal(err)
	}
	if got := findPageReport(t, rep, "Pa"); len(got.DroppedRegions) != 0 {
		t.Errorf("DroppedRegions = %+v want none", got.DroppedRegions)
	}
	moved, err := a.ReadPage("F_B", "Pa")
	if err != nil {
		t.Fatal(err)
	}
	if moved.Titles[0].Analysis == nil || moved.Titles[0].Analysis.Name != "Heading" {
		t.Errorf("title analysis not carried: %+v", moved.Titles[0].Analysis)
	}
}

// TestWriteAdoptionPurgesOtherCopies: a page sitting in more than one note collapses
// onto the note ingesting it, and the copy holding the transcription is the one whose
// state is kept.
func TestWriteAdoptionPurgesOtherCopies(t *testing.T) {
	a := New(t.TempDir())
	if _, err := a.Write(noteIn("F_A", "Pc"), svgs("Pc")); err != nil {
		t.Fatal(err)
	}
	seedPage(t, a, "F_A", "Pc", "the real transcription")
	// F_B holds a bare second copy, as a stray hand-copied file would.
	if _, err := a.Write(noteIn("F_B", "Pc"), svgs("Pc")); err != nil {
		t.Fatal(err)
	}
	// Undo the adoption F_B's write just performed, to reconstruct the broken state.
	if _, err := a.Write(noteIn("F_A", "Pc"), svgs("Pc")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a.Root, "F_B", "Pc.json"), mustJSON(t, PageDoc{SchemaVersion: CurrentSchemaVersion, PageID: "Pc"}), 0o644); err != nil {
		t.Fatal(err)
	}
	nd, err := a.ReadNote("F_B")
	if err != nil {
		t.Fatal(err)
	}
	nd.Pages = []NotePageRef{{ID: "Pc", Number: 1}}
	if _, err := a.WriteNote(nd); err != nil {
		t.Fatal(err)
	}

	rep, err := a.Write(noteIn("F_C", "Pc"), svgs("Pc"))
	if err != nil {
		t.Fatal(err)
	}
	pc := findPageReport(t, rep, "Pc")
	if want := []string{"F_A", "F_B"}; !reflect.DeepEqual(pc.AdoptedFrom, want) {
		t.Errorf("AdoptedFrom = %v want %v (both copies purged)", pc.AdoptedFrom, want)
	}
	md, err := a.ReadAnalysisMD("F_C", "Pc")
	if err != nil {
		t.Fatal(err)
	}
	if md != "the real transcription\n" {
		t.Errorf("md = %q; the copy holding the text should have been preferred", md)
	}
	owner, err := a.FindPage("Pc")
	if err != nil || owner != "F_C" {
		t.Errorf("FindPage(Pc) = %q, %v want F_C — the copies did not collapse", owner, err)
	}
}

// TestWriteAbortsOnStaleDonorSchema: the donor is read through the gated reader in
// the preflight, so a stale archive aborts before anything moves. A half-completed
// adoption would lose the transcription outright.
func TestWriteAbortsOnStaleDonorSchema(t *testing.T) {
	a := New(t.TempDir())
	if _, err := a.Write(noteIn("F_A", "Pc"), svgs("Pc")); err != nil {
		t.Fatal(err)
	}
	seedPage(t, a, "F_A", "Pc", "text")
	stale := mustJSON(t, PageDoc{SchemaVersion: CurrentSchemaVersion - 1, PageID: "Pc"})
	if err := os.WriteFile(filepath.Join(a.Root, "F_A", "Pc.json"), stale, 0o644); err != nil {
		t.Fatal(err)
	}
	noteBefore, err := os.ReadFile(filepath.Join(a.Root, "F_A", "note.json"))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := a.Write(noteIn("F_B", "Pc"), svgs("Pc")); !errors.Is(err, ErrSchemaVersion) {
		t.Fatalf("Write err = %v want ErrSchemaVersion", err)
	}
	mustNotExist(t, filepath.Join(a.Root, "F_B", "Pc.json"))
	mustExist(t, filepath.Join(a.Root, "F_A", "Pc.md"))
	after, err := os.ReadFile(filepath.Join(a.Root, "F_A", "note.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(noteBefore) {
		t.Error("donor note.json was rewritten despite the aborted write")
	}
}

// TestWriteAdoptsSeveralPagesFromOneNote: each repair of the donor must build on the
// previous one. Rewriting note.json from the doc read before the first drop would put
// the earlier page back, leaving it listed but with no file — the state query and
// retrieve hard-error on.
func TestWriteAdoptsSeveralPagesFromOneNote(t *testing.T) {
	a := New(t.TempDir())
	if _, err := a.Write(noteIn("F_A", "Pa", "Pb", "Pc"), svgs("Pa", "Pb", "Pc")); err != nil {
		t.Fatal(err)
	}
	seedPage(t, a, "F_A", "Pb", "second page")
	seedPage(t, a, "F_A", "Pc", "third page")

	rep, err := a.Write(noteIn("F_B", "Pb", "Pc"), svgs("Pb", "Pc"))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"F_A"}; !reflect.DeepEqual(rep.Repaired, want) {
		t.Errorf("Repaired = %v want %v (one note, listed once)", rep.Repaired, want)
	}
	nd, err := a.ReadNote("F_A")
	if err != nil {
		t.Fatal(err)
	}
	want := []NotePageRef{{ID: "Pa", Number: 1}}
	if !reflect.DeepEqual(nd.Pages, want) {
		t.Fatalf("donor pages = %+v want %+v", nd.Pages, want)
	}
	for _, id := range []string{"Pb", "Pc"} {
		mustNotExist(t, filepath.Join(a.Root, "F_A", id+".json"))
		if md, err := a.ReadAnalysisMD("F_B", id); err != nil || md == "" {
			t.Errorf("%s transcription not carried: %q %v", id, md, err)
		}
	}
}

// TestWriteAdoptionSkipsUnrepairedDonor: Repaired names the notes this write
// rewrote, so a donor with nothing left to drop must not appear. That is the state a
// crash between the donor's note.json repair and the removal of its page files
// leaves behind — donors are found by filename, so the stale files still make the
// note a donor on the next write, but its note.json is already correct.
func TestWriteAdoptionSkipsUnrepairedDonor(t *testing.T) {
	a := New(t.TempDir())
	if _, err := a.Write(noteIn("F_A", "Pc"), svgs("Pc")); err != nil {
		t.Fatal(err)
	}
	seedPage(t, a, "F_A", "Pc", "the transcription")
	// F_B needs a note.json of its own or List skips it and it is never a donor.
	if _, err := a.Write(noteIn("F_B", "Pz"), svgs("Pz")); err != nil {
		t.Fatal(err)
	}
	// The residue: F_B holds a stray copy of Pc's files while its note.json — the
	// record query and retrieve read — never mentions Pc.
	if err := os.WriteFile(filepath.Join(a.Root, "F_B", "Pc.json"), mustJSON(t, PageDoc{SchemaVersion: CurrentSchemaVersion, PageID: "Pc"}), 0o644); err != nil {
		t.Fatal(err)
	}

	rep, err := a.Write(noteIn("F_C", "Pc"), svgs("Pc"))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"F_A"}; !reflect.DeepEqual(rep.Repaired, want) {
		t.Errorf("Repaired = %v want %v (F_B's note.json never listed Pc, so nothing was rewritten)", rep.Repaired, want)
	}
	// Reported or not, the stray copy is gone: the invariant is what matters.
	mustNotExist(t, filepath.Join(a.Root, "F_B", "Pc.json"))
	owner, err := a.FindPage("Pc")
	if err != nil || owner != "F_C" {
		t.Errorf("FindPage(Pc) = %q, %v want F_C", owner, err)
	}
}
