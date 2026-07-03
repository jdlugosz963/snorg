package archive

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jdlugosz963/snorg/internal/snote"
)

// note builds a synthetic note with the given page ids in order.
func note(ids ...string) *snote.Note {
	n := &snote.Note{FileID: "F_TEST"}
	for i, id := range ids {
		n.Pages = append(n.Pages, snote.Page{ID: id, Number: i + 1})
	}
	return n
}

func svgMap(m map[string]string) map[string][]byte {
	out := make(map[string][]byte, len(m))
	for k, v := range m {
		out[k] = []byte(v)
	}
	return out
}

// TestWritePreservesAnalysisOnReingest checks that re-ingesting a note keeps a
// page's derived AI analysis (ingest itself never produces one, so a naive
// rewrite would wipe it).
func TestWritePreservesAnalysisOnReingest(t *testing.T) {
	a := New(t.TempDir())
	n := note("Pa")
	if err := a.Write(n, svgMap(map[string]string{"Pa": "<svg/>"})); err != nil {
		t.Fatal(err)
	}

	// Simulate the analyze command writing an analysis into the page.
	pd, err := a.ReadPage("F_TEST", "Pa")
	if err != nil {
		t.Fatal(err)
	}
	pd.Analysis = &PageAnalysis{SourceHash: "abc", Fields: map[string]string{"summary": "hello"}}
	if err := a.WritePage("F_TEST", pd); err != nil {
		t.Fatal(err)
	}
	if err := a.WriteAnalysisMD("F_TEST", "Pa", "# hello"); err != nil {
		t.Fatal(err)
	}

	// Re-ingest the same note.
	if err := a.Write(n, svgMap(map[string]string{"Pa": "<svg/>"})); err != nil {
		t.Fatal(err)
	}

	got, err := a.ReadPage("F_TEST", "Pa")
	if err != nil {
		t.Fatal(err)
	}
	if got.Analysis == nil || got.Analysis.SourceHash != "abc" || got.Analysis.Fields["summary"] != "hello" {
		t.Errorf("analysis not preserved across re-ingest: %+v", got.Analysis)
	}
	if md, err := a.ReadAnalysisMD("F_TEST", "Pa"); err != nil || md != "# hello\n" {
		t.Errorf("sidecar not preserved across re-ingest: %q, %v", md, err)
	}
}

// TestWriteCarriesRegionAnalysesByRect checks the reconcile carry-over of
// per-title/per-link transcriptions: same rect keeps its analysis, a moved rect
// drops it, and duplicate rects each consume one old analysis.
func TestWriteCarriesRegionAnalysesByRect(t *testing.T) {
	a := New(t.TempDir())
	r1 := snote.Rect{X: 10, Y: 20, W: 300, H: 100}
	r2 := snote.Rect{X: 10, Y: 400, W: 300, H: 100}
	page := snote.Page{
		ID: "Pa", Number: 1,
		Titles: []snote.Title{{Rect: r1, Level: 1}},
		Links:  []snote.Link{{Rect: r2, TargetPageID: "Pz", TargetFileID: "F_TEST"}},
	}
	n := &snote.Note{FileID: "F_TEST", Pages: []snote.Page{page}}
	if err := a.Write(n, svgMap(map[string]string{"Pa": "<svg/>"})); err != nil {
		t.Fatal(err)
	}

	pd, err := a.ReadPage("F_TEST", "Pa")
	if err != nil {
		t.Fatal(err)
	}
	pd.Titles[0].Analysis = &TitleAnalysis{Name: "Essay"}
	pd.Links[0].Analysis = &LinkAnalysis{Name: "see also"}
	if err := a.WritePage("F_TEST", pd); err != nil {
		t.Fatal(err)
	}

	// Re-ingest: same title rect, moved link rect.
	moved := page
	moved.Links = []snote.Link{{Rect: snote.Rect{X: 500, Y: 400, W: 300, H: 100}, TargetPageID: "Pz", TargetFileID: "F_TEST"}}
	n2 := &snote.Note{FileID: "F_TEST", Pages: []snote.Page{moved}}
	if err := a.Write(n2, svgMap(map[string]string{"Pa": "<svg/>"})); err != nil {
		t.Fatal(err)
	}

	got, err := a.ReadPage("F_TEST", "Pa")
	if err != nil {
		t.Fatal(err)
	}
	if got.Titles[0].Analysis == nil || got.Titles[0].Analysis.Name != "Essay" {
		t.Errorf("unmoved title lost its analysis: %+v", got.Titles[0])
	}
	if got.Links[0].Analysis != nil {
		t.Errorf("moved link kept a stale analysis: %+v", got.Links[0].Analysis)
	}
}

// TestCarryRegionAnalysesDuplicateRects: two titles on the same rect each keep
// their own analysis (consumed in order, at most once).
func TestCarryRegionAnalysesDuplicateRects(t *testing.T) {
	r := snote.Rect{X: 1, Y: 2, W: 3, H: 4}
	old := PageDoc{Titles: []TitleDoc{
		{Rect: r, Analysis: &TitleAnalysis{Name: "first"}},
		{Rect: r, Analysis: &TitleAnalysis{Name: "second"}},
	}}
	pd := PageDoc{Titles: []TitleDoc{{Rect: r}, {Rect: r}, {Rect: r}}}
	carryRegionAnalyses(&pd, old)
	if pd.Titles[0].Analysis == nil || pd.Titles[0].Analysis.Name != "first" {
		t.Errorf("titles[0] = %+v, want first", pd.Titles[0].Analysis)
	}
	if pd.Titles[1].Analysis == nil || pd.Titles[1].Analysis.Name != "second" {
		t.Errorf("titles[1] = %+v, want second", pd.Titles[1].Analysis)
	}
	if pd.Titles[2].Analysis != nil {
		t.Errorf("titles[2] = %+v, want nil (old analyses exhausted)", pd.Titles[2].Analysis)
	}
}

func mustExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
}

func mustNotExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected %s to be gone, err=%v", path, err)
	}
}

func TestWritePreservesUnchangedAndAnalyses(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "F_TEST")
	a := New(root)
	svgs := svgMap(map[string]string{"Pa": "<svg>a</svg>", "Pb": "<svg>b</svg>"})
	if err := a.Write(note("Pa", "Pb"), svgs); err != nil {
		t.Fatal(err)
	}

	// Simulate an expensive per-page analysis artifact next to each page.
	analysis := filepath.Join(dir, "Pa.analysis.json")
	if err := os.WriteFile(analysis, []byte(`{"llm":"x"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	svgPath := filepath.Join(dir, "Pa.svg")
	before, err := os.Stat(svgPath)
	if err != nil {
		t.Fatal(err)
	}

	// Re-write identical note: nothing should be rewritten, analysis preserved.
	if err := a.Write(note("Pa", "Pb"), svgs); err != nil {
		t.Fatal(err)
	}
	mustExist(t, analysis)
	after, err := os.Stat(svgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Error("unchanged Pa.svg was rewritten (mtime changed)")
	}
}

func TestWriteChangedPageKeepsAnalysis(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "F_TEST")
	a := New(root)
	if err := a.Write(note("Pa"), svgMap(map[string]string{"Pa": "<svg>a</svg>"})); err != nil {
		t.Fatal(err)
	}
	analysis := filepath.Join(dir, "Pa.analysis.json")
	if err := os.WriteFile(analysis, []byte(`{"llm":"x"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	// Change the page content.
	if err := a.Write(note("Pa"), svgMap(map[string]string{"Pa": "<svg>EDITED</svg>"})); err != nil {
		t.Fatal(err)
	}
	// Prior analysis must survive a content change (it becomes re-analysis context).
	mustExist(t, analysis)
}

func TestWriteRemovedPagePruned(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "F_TEST")
	a := New(root)
	svgs := svgMap(map[string]string{"Pa": "<svg>a</svg>", "Pb": "<svg>b</svg>"})
	if err := a.Write(note("Pa", "Pb"), svgs); err != nil {
		t.Fatal(err)
	}
	// Analysis on both pages.
	keep := filepath.Join(dir, "Pa.analysis.json")
	drop := filepath.Join(dir, "Pb.analysis.json")
	for _, p := range []string{keep, drop} {
		if err := os.WriteFile(p, []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Re-write without Pb.
	if err := a.Write(note("Pa"), svgMap(map[string]string{"Pa": "<svg>a</svg>"})); err != nil {
		t.Fatal(err)
	}
	mustNotExist(t, filepath.Join(dir, "Pb.json"))
	mustNotExist(t, filepath.Join(dir, "Pb.svg"))
	mustNotExist(t, drop)
	mustExist(t, filepath.Join(dir, "Pa.json"))
	mustExist(t, keep)

	var nd NoteDoc
	b, _ := os.ReadFile(filepath.Join(dir, "note.json"))
	if err := json.Unmarshal(b, &nd); err != nil {
		t.Fatal(err)
	}
	if len(nd.Pages) != 1 || nd.Pages[0].ID != "Pa" {
		t.Errorf("note.json pages = %+v want [Pa]", nd.Pages)
	}
}

// TestWriteDisabledPipelineKeepsSVGVerbatim: with every SVG stage off the
// renderer's bytes land on disk untouched — no anchors, no background
// extraction, no reflow.
func TestWriteDisabledPipelineKeepsSVGVerbatim(t *testing.T) {
	root := t.TempDir()
	a := New(root)
	a.SVG = SVGPipeline{}

	b64 := base64.StdEncoding.EncodeToString([]byte("bg"))
	raw := string(bgSVG(b64))
	n := note("Pa", "Pb")
	n.Pages[0].Links = []snote.Link{{
		Rect:         snote.Rect{X: 1, Y: 2, W: 3, H: 4},
		TargetPageID: "Pb", TargetFileID: "F_TEST",
	}}
	if err := a.Write(n, svgMap(map[string]string{"Pa": raw, "Pb": "<svg>b</svg>"})); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(filepath.Join(root, "F_TEST", "Pa.svg"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != raw {
		t.Errorf("disabled pipeline modified the SVG:\nwant: %s\ngot:  %s", raw, got)
	}
	mustNotExist(t, filepath.Join(root, "F_TEST", "backgrounds"))
}

func TestWriteExtractsAndDedupesBackground(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "F_TEST")
	a := New(root)

	// Two pages share one identical inline background.
	b64 := base64.StdEncoding.EncodeToString([]byte("shared background"))
	sum := sha256.Sum256([]byte("shared background"))
	name := hex.EncodeToString(sum[:]) + ".png"
	svgs := svgMap(map[string]string{"Pa": string(bgSVG(b64)), "Pb": string(bgSVG(b64))})
	if err := a.Write(note("Pa", "Pb"), svgs); err != nil {
		t.Fatal(err)
	}

	// Single deduped background file in the note's backgrounds/ subfolder.
	bg := filepath.Join(dir, "backgrounds", name)
	mustExist(t, bg)
	entries, err := os.ReadDir(filepath.Join(dir, "backgrounds"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 deduped background, got %d", len(entries))
	}

	// Each page SVG references the background by descendant href, no base64 left.
	for _, id := range []string{"Pa", "Pb"} {
		b, err := os.ReadFile(filepath.Join(dir, id+".svg"))
		if err != nil {
			t.Fatal(err)
		}
		s := string(b)
		if !strings.Contains(s, `xlink:href="backgrounds/`+name+`"`) {
			t.Errorf("%s.svg missing descendant href:\n%s", id, s)
		}
		if strings.Contains(s, "../") {
			t.Errorf("%s.svg href escapes upward (librsvg blocks it)", id)
		}
		if strings.Contains(s, "base64") {
			t.Errorf("%s.svg still embeds base64", id)
		}
	}

	// Re-write identical input: background and SVGs are byte-stable (no churn).
	bgBefore, _ := os.Stat(bg)
	svgBefore, _ := os.Stat(filepath.Join(dir, "Pa.svg"))
	if err := a.Write(note("Pa", "Pb"), svgs); err != nil {
		t.Fatal(err)
	}
	bgAfter, _ := os.Stat(bg)
	svgAfter, _ := os.Stat(filepath.Join(dir, "Pa.svg"))
	if !bgAfter.ModTime().Equal(bgBefore.ModTime()) {
		t.Error("unchanged background was rewritten")
	}
	if !svgAfter.ModTime().Equal(svgBefore.ModTime()) {
		t.Error("unchanged Pa.svg was rewritten")
	}
}

// TestWriteReorderUpdatesNavAndNoteJSON: reordering pages renumbers note.json,
// keeps foreign per-page artifacts, and legitimately rewrites the SVGs whose
// prev/next neighbors changed (the baked nav zones must follow the new order).
func TestWriteReorderUpdatesNavAndNoteJSON(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "F_TEST")
	a := New(root)
	svgs := svgMap(map[string]string{"Pa": "<svg>a</svg>", "Pb": "<svg>b</svg>"})
	if err := a.Write(note("Pa", "Pb"), svgs); err != nil {
		t.Fatal(err)
	}
	analysis := filepath.Join(dir, "Pa.analysis.json")
	if err := os.WriteFile(analysis, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Swap order.
	if err := a.Write(note("Pb", "Pa"), svgs); err != nil {
		t.Fatal(err)
	}
	mustExist(t, analysis)
	if pa := readSVG(t, root, "Pa"); !strings.Contains(pa, `xlink:href="Pb.svg"><rect x="0"`) {
		t.Errorf("Pa.svg nav not updated to prev=Pb after reorder:\n%s", pa)
	}

	var nd NoteDoc
	b, _ := os.ReadFile(filepath.Join(dir, "note.json"))
	if err := json.Unmarshal(b, &nd); err != nil {
		t.Fatal(err)
	}
	if len(nd.Pages) != 2 || nd.Pages[0].ID != "Pb" || nd.Pages[1].ID != "Pa" {
		t.Errorf("note.json order = %+v want [Pb Pa]", nd.Pages)
	}
	if nd.Pages[0].Number != 1 || nd.Pages[1].Number != 2 {
		t.Errorf("note.json numbers not renumbered: %+v", nd.Pages)
	}
}
