package ingest_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
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
	if _, err := exec.LookPath(sntool.Binary); err != nil {
		t.Skipf("%s not in PATH", sntool.Binary)
	}
	notePath := filepath.Join("..", "..", "note.note")
	if _, err := os.Stat(notePath); err != nil {
		t.Skipf("sample note not found: %v", err)
	}

	root := t.TempDir()
	note, err := ingest.Run(sntool.New(), archive.New(root), notePath)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if note.FileID == "" {
		t.Fatal("empty file id")
	}
	if len(note.Pages) != 6 {
		t.Fatalf("pages = %d want 6", len(note.Pages))
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

	// idempotent: re-ingest yields identical note.json
	before, _ := os.ReadFile(filepath.Join(dir, "note.json"))
	if _, err := ingest.Run(sntool.New(), archive.New(root), notePath); err != nil {
		t.Fatalf("re-ingest: %v", err)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "note.json"))
	if string(before) != string(after) {
		t.Error("re-ingest changed note.json")
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
	if _, err := exec.LookPath(sntool.Binary); err != nil {
		t.Skipf("%s not in PATH", sntool.Binary)
	}
	repo := filepath.Join("..", "..")
	for _, f := range []string{"note.note", "note2.note", "note3.note"} {
		if _, err := os.Stat(filepath.Join(repo, f)); err != nil {
			t.Skipf("fixture %s not found: %v", f, err)
		}
	}

	root := t.TempDir()
	note, err := ingest.Run(sntool.New(), archive.New(root), filepath.Join(repo, "note.note"))
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
	if _, err := ingest.Run(sntool.New(), archive.New(root), filepath.Join(repo, "note2.note")); err != nil {
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
	if _, err := ingest.Run(sntool.New(), archive.New(root), filepath.Join(repo, "note3.note")); err != nil {
		t.Fatalf("ingest note3: %v", err)
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

// fakeSource is an in-memory snote.Source for the concurrency tests, so they need
// no supernote-tool. Each path becomes a one-page note keyed by its basename;
// paths in failOn error from Read. It records peak concurrent RenderSVGs calls.
type fakeSource struct {
	failOn    map[string]bool
	active    int32
	maxActive int32
}

func (f *fakeSource) Read(path string) (*snote.Note, error) {
	if f.failOn[path] {
		return nil, fmt.Errorf("boom: %s", path)
	}
	id := "F_" + strings.TrimSuffix(filepath.Base(path), ".note")
	return &snote.Note{FileID: id, Pages: []snote.Page{{ID: "P1", Number: 1}}}, nil
}

func (f *fakeSource) RenderSVGs(path string) ([][]byte, error) {
	n := atomic.AddInt32(&f.active, 1)
	for {
		m := atomic.LoadInt32(&f.maxActive)
		if n <= m || atomic.CompareAndSwapInt32(&f.maxActive, m, n) {
			break
		}
	}
	time.Sleep(5 * time.Millisecond)
	atomic.AddInt32(&f.active, -1)
	return [][]byte{[]byte("<svg/>")}, nil
}

func TestRunManyArchivesAllInOrder(t *testing.T) {
	root := t.TempDir()
	a := archive.New(root)
	paths := []string{"a.note", "b.note", "c.note", "d.note"}

	results := ingest.RunMany(&fakeSource{}, a, paths, 2)
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

func TestRunManyContinuesOnError(t *testing.T) {
	root := t.TempDir()
	src := &fakeSource{failOn: map[string]bool{"b.note": true}}
	paths := []string{"a.note", "b.note", "c.note"}

	results := ingest.RunMany(src, archive.New(root), paths, 0) // 0 -> NumCPU
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

func TestRunManyRespectsJobLimit(t *testing.T) {
	src := &fakeSource{}
	paths := make([]string, 8)
	for i := range paths {
		paths[i] = fmt.Sprintf("n%d.note", i)
	}
	ingest.RunMany(src, archive.New(t.TempDir()), paths, 2)
	if src.maxActive > 2 {
		t.Errorf("peak concurrency = %d want <= 2", src.maxActive)
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
