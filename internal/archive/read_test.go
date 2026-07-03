package archive

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/jdlugosz963/snorg/internal/snote"
)

func TestListReturnsSortedNoteDirs(t *testing.T) {
	root := t.TempDir()
	a := New(root)
	for _, id := range []string{"F_B", "F_A"} {
		n := &snote.Note{FileID: id, Pages: []snote.Page{{ID: "Pa", Number: 1}}}
		if err := a.Write(n, svgMap(map[string]string{"Pa": "<svg/>"})); err != nil {
			t.Fatal(err)
		}
	}
	// A stray non-note dir and a loose file must be ignored.
	if err := os.MkdirAll(filepath.Join(root, "not-a-note"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "loose.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	ids, err := a.List()
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"F_A", "F_B"}; !reflect.DeepEqual(ids, want) {
		t.Errorf("List() = %v want %v", ids, want)
	}
}

func TestReadNoteAndPageRoundTrip(t *testing.T) {
	root := t.TempDir()
	a := New(root)
	n := &snote.Note{
		FileID:    "F_TEST",
		Signature: "sig",
		Device:    "dev",
		Source:    "note.note",
		Pages: []snote.Page{
			{ID: "Pa", Number: 1, Keywords: []snote.Keyword{{Text: "hello"}}},
		},
	}
	if err := a.Write(n, svgMap(map[string]string{"Pa": "<svg/>"})); err != nil {
		t.Fatal(err)
	}

	nd, err := a.ReadNote("F_TEST")
	if err != nil {
		t.Fatal(err)
	}
	if nd.FileID != "F_TEST" || nd.Source != "note.note" || len(nd.Pages) != 1 || nd.Pages[0].ID != "Pa" {
		t.Errorf("ReadNote = %+v", nd)
	}

	pd, err := a.ReadPage("F_TEST", "Pa")
	if err != nil {
		t.Fatal(err)
	}
	if pd.PageID != "Pa" || len(pd.Keywords) != 1 || pd.Keywords[0].Text != "hello" {
		t.Errorf("ReadPage = %+v", pd)
	}
}

func TestAnalysisMDRoundTrip(t *testing.T) {
	a := New(t.TempDir())
	n := &snote.Note{FileID: "F_TEST", Pages: []snote.Page{{ID: "Pa", Number: 1}}}
	if err := a.Write(n, svgMap(map[string]string{"Pa": "<svg/>"})); err != nil {
		t.Fatal(err)
	}

	// Missing sidecar reads as empty, not as an error.
	if md, err := a.ReadAnalysisMD("F_TEST", "Pa"); err != nil || md != "" {
		t.Errorf("missing sidecar = %q, %v; want \"\", nil", md, err)
	}

	// Content is normalized to exactly one trailing newline.
	if err := a.WriteAnalysisMD("F_TEST", "Pa", "# Title\n\nbody\n\n\n"); err != nil {
		t.Fatal(err)
	}
	if md, err := a.ReadAnalysisMD("F_TEST", "Pa"); err != nil || md != "# Title\n\nbody\n" {
		t.Errorf("sidecar = %q, %v", md, err)
	}

	// Pruning the page removes the sidecar with the rest of <PAGEID>.*.
	empty := &snote.Note{FileID: "F_TEST", Pages: []snote.Page{{ID: "Pb", Number: 1}}}
	if err := a.Write(empty, svgMap(map[string]string{"Pb": "<svg/>"})); err != nil {
		t.Fatal(err)
	}
	mustNotExist(t, filepath.Join(a.Root, "F_TEST", "Pa.md"))
}

func TestSVGRelIsArchiveRelativeSlash(t *testing.T) {
	a := New("/anywhere")
	if got, want := a.SVGRel("F_TEST", "Pa"), "F_TEST/Pa.svg"; got != want {
		t.Errorf("SVGRel = %q want %q", got, want)
	}
}
