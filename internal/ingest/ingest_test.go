package ingest_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jdlugosz963/snorg/internal/archive"
	"github.com/jdlugosz963/snorg/internal/ingest"
	"github.com/jdlugosz963/snorg/internal/snote"
	"github.com/jdlugosz963/snorg/internal/snote/sntool"
)

// End-to-end ingest against the sample note.note at the repo root, asserting the
// known ground truth (see docs/supernote-format.md).
func TestIngestSampleNote(t *testing.T) {
	notePath := filepath.Join("..", "..", "note.note")
	if _, err := os.Stat(notePath); err != nil {
		t.Skipf("sample note not found: %v", err)
	}

	root := t.TempDir()
	store := archive.New(root)
	note, report, err := ingest.Run(sntool.New(), store, notePath)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if note.FileID == "" {
		t.Fatal("empty file id")
	}
	if len(note.Pages) != 6 {
		t.Fatalf("pages = %d want 6", len(note.Pages))
	}
	// First ingest reports every page newly written.
	if !report.NoteChanged || len(report.Pages) != 6 {
		t.Fatalf("first-ingest report = %+v, want note changed + 6 pages", report)
	}
	for _, p := range report.Pages {
		if !p.New {
			t.Errorf("first-ingest page %s not marked New: %+v", p.PageID, p)
		}
	}

	dir := filepath.Join(root, note.FileID)

	var nd archive.NoteDoc
	readJSON(t, filepath.Join(dir, "note.json"), &nd)
	if len(nd.Pages) != 6 {
		t.Fatalf("note.json pages = %d want 6", len(nd.Pages))
	}
	for i, pr := range nd.Pages {
		var pd archive.PageDoc
		readJSON(t, filepath.Join(dir, pr.ID+".json"), &pd)
		if i == 2 && !pd.Starred {
			t.Error("page 3 should be starred")
		}
		if i != 2 && pd.Starred {
			t.Errorf("page %d unexpectedly starred", pr.Number)
		}
	}

	// each page has a .json and a non-empty .svg
	for _, pr := range nd.Pages {
		svg := filepath.Join(dir, pr.ID+".svg")
		info, err := os.Stat(svg)
		if err != nil || info.Size() == 0 {
			t.Errorf("page %s svg missing/empty: %v", pr.ID, err)
		}
	}

	// page 1: one title at rect 356,98,336,208
	var p1 archive.PageDoc
	readJSON(t, filepath.Join(dir, nd.Pages[0].ID+".json"), &p1)
	if len(p1.Titles) != 1 || p1.Titles[0].Rect != (snote.Rect{X: 356, Y: 98, W: 336, H: 208}) {
		t.Errorf("page1 titles = %+v", p1.Titles)
	}

	// page 4: keyword "fizyka"
	var p4 archive.PageDoc
	readJSON(t, filepath.Join(dir, nd.Pages[3].ID+".json"), &p4)
	if len(p4.Keywords) != 1 || p4.Keywords[0].Text != "fizyka" {
		t.Errorf("page4 keywords = %+v", p4.Keywords)
	}

	// page 5: internal link to page 1 (target page id == page 1's id), name decoded from LINKFILE
	var p5 archive.PageDoc
	readJSON(t, filepath.Join(dir, nd.Pages[4].ID+".json"), &p5)
	if len(p5.Links) != 1 || p5.Links[0].TargetFileID != nd.FileID || p5.Links[0].TargetPageID != nd.Pages[0].ID {
		t.Errorf("page5 links = %+v", p5.Links)
	}
	if p5.Links[0].Name != "linked-note" {
		t.Errorf("page5 link name = %q want linked-note", p5.Links[0].Name)
	}

	// page 6: external link to another note
	var p6 archive.PageDoc
	readJSON(t, filepath.Join(dir, nd.Pages[5].ID+".json"), &p6)
	if len(p6.Links) != 1 || p6.Links[0].TargetFileID == "" || p6.Links[0].TargetFileID == nd.FileID {
		t.Errorf("page6 links = %+v", p6.Links)
	}
	if p6.Links[0].Name != "external-note" {
		t.Errorf("page6 link name = %q want external-note", p6.Links[0].Name)
	}

	// idempotent: re-ingest yields identical note.json and an empty change report.
	before, _ := os.ReadFile(filepath.Join(dir, "note.json"))
	_, report2, err := ingest.Run(sntool.New(), store, notePath)
	if err != nil {
		t.Fatalf("re-ingest: %v", err)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "note.json"))
	if string(before) != string(after) {
		t.Error("re-ingest changed note.json")
	}
	if report2.NoteChanged || len(report2.Pages) != 0 || len(report2.Pruned) != 0 {
		t.Errorf("unchanged re-ingest report = %+v, want empty", report2)
	}
}

// Known fixture page ids (same FILE_ID across note*.note):
//
//	note.note  : XXVq HWtV C92b aIIv ItW1 CNFn
//	note2.note : XXVq HWtV      aIIv ItW1 CNFn   (C92b removed)
//	note3.note : XXVq HWtV      aIIv ItW1 Q5Fob CNFn   (C92b removed, Q5Fob inserted)
const (
	pageRemoved = "P20260629173349306737ntUCgwqdC92b" // dropped in note2/note3
	pageNew     = "P20260629192854941441Q5FobOsuKQrE" // added in note3
	pageKept    = "P20260629174154738204HApvodRQCNFn" // present in all three
)

// End-to-end incremental update: re-ingesting an edited note (same FILE_ID) must
// reconcile the archive in place — prune removed pages with their artifacts, add
// new pages, reorder — while preserving expensive per-page analyses of pages that
// carried over.
func TestIngestUpdatesArchive(t *testing.T) {
	repo := filepath.Join("..", "..")
	for _, f := range []string{"note.note", "note2.note", "note3.note"} {
		if _, err := os.Stat(filepath.Join(repo, f)); err != nil {
			t.Skipf("fixture %s not found: %v", f, err)
		}
	}

	root := t.TempDir()
	// Each ingest runs a day after the previous one, so a stamp names the run that wrote it.
	clock := time.Date(2026, time.July, 1, 12, 0, 0, 0, time.UTC)
	arch := func() *archive.Archive {
		a := archive.New(root)
		a.Now = func() time.Time { return clock }
		clock = clock.Add(24 * time.Hour)
		return a
	}
	note, _, err := ingest.Run(sntool.New(), arch(), filepath.Join(repo, "note.note"))
	if err != nil {
		t.Fatalf("ingest note: %v", err)
	}
	dir := filepath.Join(root, note.FileID)

	// Record the kept page's metadata, and drop sentinel "analyses" on a kept page
	// and on the page that will be removed.
	var keptBefore archive.PageDoc
	readJSON(t, filepath.Join(dir, pageKept+".json"), &keptBefore)
	keptAnalysis := filepath.Join(dir, pageKept+".analysis.json")
	removedAnalysis := filepath.Join(dir, pageRemoved+".analysis.json")
	for _, p := range []string{keptAnalysis, removedAnalysis} {
		if err := os.WriteFile(p, []byte(`{"llm":"x"}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Update to note2.note: page C92b removed.
	if _, _, err := ingest.Run(sntool.New(), arch(), filepath.Join(repo, "note2.note")); err != nil {
		t.Fatalf("ingest note2: %v", err)
	}
	for _, suffix := range []string{".json", ".svg", ".analysis.json"} {
		if _, err := os.Stat(filepath.Join(dir, pageRemoved+suffix)); !os.IsNotExist(err) {
			t.Errorf("removed page artifact %s%s still present (err=%v)", pageRemoved, suffix, err)
		}
	}
	if _, err := os.Stat(keptAnalysis); err != nil {
		t.Errorf("kept page analysis was lost: %v", err)
	}
	var keptAfter archive.PageDoc
	readJSON(t, filepath.Join(dir, pageKept+".json"), &keptAfter)
	if !reflect.DeepEqual(keptAfter, keptBefore) {
		t.Errorf("unchanged page metadata drifted: %+v -> %+v", keptBefore, keptAfter)
	}
	var nd2 archive.NoteDoc
	readJSON(t, filepath.Join(dir, "note.json"), &nd2)
	if len(nd2.Pages) != 5 {
		t.Fatalf("note2 note.json pages = %d want 5", len(nd2.Pages))
	}
	for _, pr := range nd2.Pages {
		if pr.ID == pageRemoved {
			t.Error("removed page still listed in note.json")
		}
	}

	// Update to note3.note: new page Q5Fob inserted before the last page.
	if _, _, err := ingest.Run(sntool.New(), arch(), filepath.Join(repo, "note3.note")); err != nil {
		t.Fatalf("ingest note3: %v", err)
	}
	// The kept page gained a new neighbour: its baked nav moved (an archive change),
	// its handwriting did not (no device change).
	var kept3 archive.PageDoc
	readJSON(t, filepath.Join(dir, pageKept+".json"), &kept3)
	if !kept3.ModifiedAt.After(keptBefore.ModifiedAt) {
		t.Errorf("kept page ModifiedAt = %v, want after %v (its nav SVG changed)", kept3.ModifiedAt, keptBefore.ModifiedAt)
	}
	if !kept3.DeviceModifiedAt.Equal(keptBefore.DeviceModifiedAt) || kept3.DeviceHash != keptBefore.DeviceHash {
		t.Errorf("kept page device stamp moved: %v (%s) -> %v (%s)",
			keptBefore.DeviceModifiedAt, keptBefore.DeviceHash, kept3.DeviceModifiedAt, kept3.DeviceHash)
	}
	var newPage archive.PageDoc
	readJSON(t, filepath.Join(dir, pageNew+".json"), &newPage)
	if newPage.PageID != pageNew {
		t.Errorf("new page id = %q want %q", newPage.PageID, pageNew)
	}
	if info, err := os.Stat(filepath.Join(dir, pageNew+".svg")); err != nil || info.Size() == 0 {
		t.Errorf("new page svg missing/empty: %v", err)
	}
	if _, err := os.Stat(keptAnalysis); err != nil {
		t.Errorf("kept page analysis lost after note3: %v", err)
	}
	var nd3 archive.NoteDoc
	readJSON(t, filepath.Join(dir, "note.json"), &nd3)
	if len(nd3.Pages) != 6 {
		t.Fatalf("note3 note.json pages = %d want 6", len(nd3.Pages))
	}
	if nd3.Pages[4].ID != pageNew || nd3.Pages[5].ID != pageKept {
		t.Errorf("note3 order wrong: pos5=%s pos6=%s", nd3.Pages[4].ID, nd3.Pages[5].ID)
	}
}

func readJSON(t *testing.T, path string, v any) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatalf("unmarshal %s: %v", path, err)
	}
}

// fakeSource is an in-memory snote.Source, so the batch tests need no real .note
// parsing. Each path becomes a note whose FILE_ID is "F_<basename>"; its pages are
// the ids in pages[path], defaulting to one page "P_<basename>" — distinct per
// path, since a PAGEID is unique archive-wide. Paths in failOn error from Read.
type fakeSource struct {
	failOn map[string]bool
	pages  map[string][]string
}

// base is the note name a path maps to: its basename without the .note extension.
func base(path string) string { return strings.TrimSuffix(filepath.Base(path), ".note") }

func (f *fakeSource) pageIDs(path string) []string {
	if ids, ok := f.pages[path]; ok {
		return ids
	}
	return []string{"P_" + base(path)}
}

func (f *fakeSource) Read(path string) (*snote.Note, error) {
	if f.failOn[path] {
		return nil, fmt.Errorf("boom: %s", path)
	}
	n := &snote.Note{FileID: "F_" + base(path)}
	for i, id := range f.pageIDs(path) {
		n.Pages = append(n.Pages, snote.Page{ID: id, Number: i + 1})
	}
	return n, nil
}

func (f *fakeSource) RenderSVGs(path string) ([][]byte, error) {
	ids := f.pageIDs(path)
	svgs := make([][]byte, len(ids))
	for i := range ids {
		svgs[i] = []byte("<svg/>")
	}
	return svgs, nil
}

// runMany is ingest.RunMany under a live context, where the batch itself cannot fail.
func runMany(t *testing.T, src snote.Source, a *archive.Archive, paths []string, opts ingest.Options) []ingest.Result {
	t.Helper()
	results, err := ingest.RunMany(context.Background(), src, a, paths, opts)
	if err != nil {
		t.Fatal(err)
	}
	return results
}

func TestRunManyArchivesAllInOrder(t *testing.T) {
	root := t.TempDir()
	a := archive.New(root)
	paths := []string{"a.note", "b.note", "c.note", "d.note"}

	results := runMany(t, &fakeSource{}, a, paths, ingest.Options{})
	if len(results) != len(paths) {
		t.Fatalf("results = %d want %d", len(results), len(paths))
	}
	for i, r := range results {
		if r.Path != paths[i] {
			t.Errorf("result %d path = %q want %q (order not preserved)", i, r.Path, paths[i])
		}
		if r.Err != nil || r.Note == nil {
			t.Errorf("result %d: note=%v err=%v", i, r.Note, r.Err)
		}
	}
	ids, err := a.List()
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"F_a", "F_b", "F_c", "F_d"}; !reflect.DeepEqual(ids, want) {
		t.Errorf("archived ids = %v want %v", ids, want)
	}
}

// TestRunManyCancelled: a cancelled context stops the batch before the next note,
// returning what landed so far and the context's error.
func TestRunManyCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var landed []string
	results, err := ingest.RunMany(ctx, &fakeSource{}, archive.New(t.TempDir()), []string{"a.note", "b.note"}, ingest.Options{
		OnResult: func(r ingest.Result) {
			landed = append(landed, r.Path)
			cancel()
		},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if len(results) != 1 || !reflect.DeepEqual(landed, []string{"a.note"}) {
		t.Errorf("results = %d, landed = %v; want only a.note", len(results), landed)
	}
}

func TestRunManyContinuesOnError(t *testing.T) {
	root := t.TempDir()
	src := &fakeSource{failOn: map[string]bool{"b.note": true}}
	paths := []string{"a.note", "b.note", "c.note"}

	results := runMany(t, src, archive.New(root), paths, ingest.Options{})
	if results[0].Err != nil || results[2].Err != nil {
		t.Errorf("ok notes errored: %v, %v", results[0].Err, results[2].Err)
	}
	if results[1].Err == nil {
		t.Error("b.note should have failed")
	}
	if results[1].Note != nil {
		t.Error("failed result should carry no note")
	}
}

func TestNoteFiles(t *testing.T) {
	root := t.TempDir()
	for _, rel := range []string{"a.note", "sub/b.note", "sub/deep/c.note", "sub/notes.txt", "d.NOTE"} {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := ingest.NoteFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		filepath.Join(root, "a.note"),
		filepath.Join(root, "d.NOTE"),
		filepath.Join(root, "sub/b.note"),
		filepath.Join(root, "sub/deep/c.note"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("NoteFiles = %v want %v", got, want)
	}
}

// TestRunManyStreamsResults: OnResult fires once per path as each note lands, so a
// front-end can report progress mid-batch, in the same order as the returned slice.
func TestRunManyStreamsResults(t *testing.T) {
	root := t.TempDir()
	paths := []string{"a.note", "b.note", "c.note", "d.note"}
	src := &fakeSource{}

	streamed := map[string]int{}
	var order []string
	results := runMany(t, src, archive.New(root), paths, ingest.Options{
		OnResult: func(r ingest.Result) {
			streamed[r.Path]++
			order = append(order, r.Path)
		},
	})

	if !reflect.DeepEqual(order, paths) {
		t.Errorf("OnResult order = %v, want %v (input order)", order, paths)
	}

	if len(streamed) != len(paths) {
		t.Errorf("OnResult saw %d distinct paths, want %d", len(streamed), len(paths))
	}
	for _, p := range paths {
		if streamed[p] != 1 {
			t.Errorf("OnResult fired %d times for %s, want once", streamed[p], p)
		}
	}
	for i, p := range paths {
		if results[i].Path != p {
			t.Errorf("results[%d].Path = %s, want %s (input order)", i, results[i].Path, p)
		}
	}
}

// TestIngestOrderIndependent is the reason the orphan store exists. A page moved to
// another note on the device must end up in that note with its transcription intact,
// no matter which of the two notes snorg sees first — and it must never end up in
// both. Ingesting the source note first used to delete the transcription before the
// destination note could ever claim it.
func TestIngestOrderIndependent(t *testing.T) {
	// After the move: a.note kept P1/P2, b.note gained P3.
	moved := map[string][]string{
		"a.note": {"P1", "P2"},
		"b.note": {"P3"},
	}
	cases := []struct {
		name string
		runs [][]string // one RunMany call per element
	}{
		{"destination first", [][]string{{"b.note"}, {"a.note"}}},
		{"source first", [][]string{{"a.note"}, {"b.note"}}},
		{"one batch, destination first", [][]string{{"b.note", "a.note"}}},
		{"one batch, source first", [][]string{{"a.note", "b.note"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := archive.New(t.TempDir())

			// Before the move: a.note holds all three pages, and P3 has been analyzed.
			before := &fakeSource{pages: map[string][]string{"a.note": {"P1", "P2", "P3"}}}
			if res := runMany(t, before, a, []string{"a.note"}, ingest.Options{}); res[0].Err != nil {
				t.Fatal(res[0].Err)
			}
			if err := a.WriteAnalysisMD("F_a", "P3", "handwritten notes"); err != nil {
				t.Fatal(err)
			}
			pd, err := a.ReadPage("F_a", "P3")
			if err != nil {
				t.Fatal(err)
			}
			pd.Analysis = &archive.PageAnalysis{SourceHash: "h3"}
			pd.Tags = []string{"keep"}
			if _, err := a.WritePage("F_a", pd, false); err != nil {
				t.Fatal(err)
			}

			src := &fakeSource{pages: moved}
			for _, paths := range tc.runs {
				for _, r := range runMany(t, src, a, paths, ingest.Options{}) {
					if r.Err != nil {
						t.Fatalf("ingest %s: %v", r.Path, r.Err)
					}
				}
			}

			owner, err := a.FindPage("P3")
			if err != nil {
				t.Fatalf("P3 not uniquely owned: %v", err)
			}
			if owner != "F_b" {
				t.Errorf("P3 owned by %s, want F_b", owner)
			}
			md, err := a.ReadAnalysisMD("F_b", "P3")
			if err != nil {
				t.Fatal(err)
			}
			if md != "handwritten notes\n" {
				t.Errorf("transcription = %q, want it carried over with the page", md)
			}
			moved, err := a.ReadPage("F_b", "P3")
			if err != nil {
				t.Fatal(err)
			}
			if moved.Analysis == nil || moved.Analysis.SourceHash != "h3" {
				t.Errorf("analysis = %+v, want it carried over (else analyze pays to re-transcribe)", moved.Analysis)
			}
			if len(moved.Tags) != 1 || moved.Tags[0] != "keep" {
				t.Errorf("tags = %v, want [keep]", moved.Tags)
			}
			nd, err := a.ReadNote("F_a")
			if err != nil {
				t.Fatal(err)
			}
			for _, ref := range nd.Pages {
				if ref.ID == "P3" {
					t.Error("the source note still lists the page it lost")
				}
			}
		})
	}
}
