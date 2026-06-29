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

func TestWriteReorderOnlyTouchesNoteJSON(t *testing.T) {
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
	paBefore, _ := os.Stat(filepath.Join(dir, "Pa.svg"))

	// Swap order.
	if err := a.Write(note("Pb", "Pa"), svgs); err != nil {
		t.Fatal(err)
	}
	paAfter, _ := os.Stat(filepath.Join(dir, "Pa.svg"))
	if !paAfter.ModTime().Equal(paBefore.ModTime()) {
		t.Error("reorder rewrote Pa.svg")
	}
	mustExist(t, analysis)

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
