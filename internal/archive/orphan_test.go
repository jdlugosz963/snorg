package archive

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func orphanFile(a *Archive, pageID, ext string) string {
	return filepath.Join(a.Root, orphanDir, pageID+ext)
}

// TestPruneParksStateInOrphans: when a page leaves a note, the text snorg cannot
// rebuild must not be deleted — the page may be about to arrive in another note that
// has not been ingested yet, or was ingested first.
func TestPruneParksStateInOrphans(t *testing.T) {
	a := New(t.TempDir())
	if _, err := a.Write(noteIn("F_A", "Pa", "Pb"), svgs("Pa", "Pb")); err != nil {
		t.Fatal(err)
	}
	seedPage(t, a, "F_A", "Pb", "worth keeping")

	rep, err := a.Write(noteIn("F_A", "Pa"), svgs("Pa"))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"Pb"}; !reflect.DeepEqual(rep.Pruned, want) || !reflect.DeepEqual(rep.Parked, want) {
		t.Fatalf("Pruned=%v Parked=%v want %v for both", rep.Pruned, rep.Parked, want)
	}
	for _, ext := range []string{".json", ".md", ".md.diff"} {
		mustExist(t, orphanFile(a, "Pb", ext))
	}
	// The SVG is rebuilt from the .note and its background href only resolves beside
	// a note's backgrounds/, so parking one would keep a file that renders broken.
	mustNotExist(t, orphanFile(a, "Pb", ".svg"))
	for _, ext := range []string{".json", ".md", ".md.diff", ".svg"} {
		mustNotExist(t, filepath.Join(a.Root, "F_A", "Pb"+ext))
	}
}

// TestPruneDeletesBarePage: a page that was never analyzed, edited or tagged has
// nothing to reclaim, so it is deleted as before and the store stays empty.
func TestPruneDeletesBarePage(t *testing.T) {
	a := New(t.TempDir())
	if _, err := a.Write(noteIn("F_A", "Pa", "Pb"), svgs("Pa", "Pb")); err != nil {
		t.Fatal(err)
	}
	rep, err := a.Write(noteIn("F_A", "Pa"), svgs("Pa"))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"Pb"}; !reflect.DeepEqual(rep.Pruned, want) {
		t.Errorf("Pruned = %v want %v", rep.Pruned, want)
	}
	if len(rep.Parked) != 0 {
		t.Errorf("Parked = %v want none", rep.Parked)
	}
	mustNotExist(t, orphanFile(a, "Pb", ".json"))
}

// TestWriteReclaimsOrphan closes the loop: the note that gains the page picks its
// state back up out of the store. This is what makes the outcome independent of
// which note is ingested first.
func TestWriteReclaimsOrphan(t *testing.T) {
	a := New(t.TempDir())
	if _, err := a.Write(noteIn("F_A", "Pa", "Pb"), svgs("Pa", "Pb")); err != nil {
		t.Fatal(err)
	}
	seedPage(t, a, "F_A", "Pb", "survives the round trip")
	if _, err := a.Write(noteIn("F_A", "Pa"), svgs("Pa")); err != nil {
		t.Fatal(err)
	}

	rep, err := a.Write(noteIn("F_B", "Pb"), svgs("Pb"))
	if err != nil {
		t.Fatal(err)
	}
	pb := findPageReport(t, rep, "Pb")
	if want := []string{orphanDir}; !reflect.DeepEqual(pb.AdoptedFrom, want) {
		t.Errorf("AdoptedFrom = %v want %v", pb.AdoptedFrom, want)
	}
	if pb.New {
		t.Error("reclaimed page reported as New; its transcription was right there")
	}
	md, err := a.ReadAnalysisMD("F_B", "Pb")
	if err != nil {
		t.Fatal(err)
	}
	if md != "survives the round trip\n" {
		t.Errorf("md = %q want the parked transcription", md)
	}
	pd, err := a.ReadPage("F_B", "Pb")
	if err != nil {
		t.Fatal(err)
	}
	if pd.Analysis == nil || !reflect.DeepEqual(pd.Tags, []string{"work"}) {
		t.Errorf("analysis/tags not reclaimed: %+v %v", pd.Analysis, pd.Tags)
	}
	for _, ext := range []string{".json", ".md", ".md.diff"} {
		mustNotExist(t, orphanFile(a, "Pb", ext))
	}
}

// TestOwnCopyBeatsStaleOrphan: residue in the store must never overwrite the live
// page. A note that still owns the page keeps its own text and drops the leftover.
func TestOwnCopyBeatsStaleOrphan(t *testing.T) {
	a := New(t.TempDir())
	if _, err := a.Write(noteIn("F_A", "Pa"), svgs("Pa")); err != nil {
		t.Fatal(err)
	}
	seedPage(t, a, "F_A", "Pa", "the live text")
	if err := os.MkdirAll(filepath.Join(a.Root, orphanDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := a.WriteAnalysisMD(orphanDir, "Pa", "stale leftover"); err != nil {
		t.Fatal(err)
	}

	// Gaining a second page is what triggers the archive-wide lookup.
	if _, err := a.Write(noteIn("F_A", "Pa", "Pb"), svgs("Pa", "Pb")); err != nil {
		t.Fatal(err)
	}
	md, err := a.ReadAnalysisMD("F_A", "Pa")
	if err != nil {
		t.Fatal(err)
	}
	if md != "the live text\n" {
		t.Errorf("md = %q; the stale parked copy overwrote the live page", md)
	}
	mustNotExist(t, orphanFile(a, "Pa", ".md"))
}

// TestMigrateWalksOrphanStore: a parked page doc is schema-versioned and adoption
// reads it through the gated reader, so a stale one left by a long-deleted note would
// abort an unrelated ingest unless migrate can reach it. List cannot — it has no
// note.json.
func TestMigrateWalksOrphanStore(t *testing.T) {
	a := New(t.TempDir())
	if err := os.MkdirAll(filepath.Join(a.Root, orphanDir), 0o755); err != nil {
		t.Fatal(err)
	}
	stale := mustJSON(t, PageDoc{SchemaVersion: CurrentSchemaVersion - 1, PageID: "Pz"})
	if err := os.WriteFile(orphanFile(a, "Pz", ".json"), stale, 0o644); err != nil {
		t.Fatal(err)
	}

	results, err := a.MigrateAll(context.Background(), MigrateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var saw bool
	for _, r := range results {
		if r.ID == "Pz" {
			saw = true
			if r.Err != nil {
				t.Errorf("migrating the parked page failed: %v", r.Err)
			}
		}
	}
	if !saw {
		t.Fatal("migrate never visited the orphan store")
	}
	if _, err := a.ReadPage(orphanDir, "Pz"); err != nil {
		t.Errorf("parked page still unreadable after migrate: %v", err)
	}
}
