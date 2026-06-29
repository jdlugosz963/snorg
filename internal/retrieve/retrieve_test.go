package retrieve_test

import (
	"reflect"
	"testing"

	"github.com/jdlugosz963/snorg/internal/archive"
	"github.com/jdlugosz963/snorg/internal/retrieve"
	"github.com/jdlugosz963/snorg/internal/snote"
)

func writeNote(t *testing.T, a *archive.Archive, n *snote.Note, svgs map[string]string) {
	t.Helper()
	m := make(map[string][]byte, len(svgs))
	for k, v := range svgs {
		m[k] = []byte(v)
	}
	if err := a.Write(n, m); err != nil {
		t.Fatal(err)
	}
}

func TestGetAssemblesOrderedView(t *testing.T) {
	root := t.TempDir()
	a := archive.New(root)
	n := &snote.Note{
		FileID: "F_TEST",
		Source: "note.note",
		Pages: []snote.Page{
			{
				ID: "Pa", Number: 1, Starred: true,
				Titles:   []snote.Title{{Rect: snote.Rect{X: 1, Y: 2, W: 3, H: 4}, Level: 2}},
				Keywords: []snote.Keyword{{Text: "foo"}},
				Links: []snote.Link{
					{Rect: snote.Rect{X: 5}, TargetPageID: "Pb", TargetFileID: "F_TEST"},                      // internal
					{Rect: snote.Rect{X: 6}, TargetPageID: "Pz", TargetFileID: "F_OTHER", Name: "other-note"}, // external
				},
			},
			{ID: "Pb", Number: 2},
		},
	}
	writeNote(t, a, n, map[string]string{"Pa": "<svg>a</svg>", "Pb": "<svg>b</svg>"})

	view, err := retrieve.Get(a, "F_TEST")
	if err != nil {
		t.Fatal(err)
	}

	if view.FileID != "F_TEST" || view.Source != "note.note" {
		t.Errorf("note metadata = %+v", view)
	}
	if len(view.Pages) != 2 || view.Pages[0].PageID != "Pa" || view.Pages[1].PageID != "Pb" {
		t.Fatalf("pages not in order: %+v", view.Pages)
	}

	p := view.Pages[0]
	if !p.Starred || p.Number != 1 {
		t.Errorf("page0 placement = %+v", p)
	}
	if p.SVG != "F_TEST/Pa.svg" {
		t.Errorf("svg = %q want archive-relative F_TEST/Pa.svg", p.SVG)
	}
	if want := []retrieve.TitleView{{Rect: snote.Rect{X: 1, Y: 2, W: 3, H: 4}, Level: 2}}; !reflect.DeepEqual(p.Titles, want) {
		t.Errorf("titles = %+v want %+v", p.Titles, want)
	}
	if len(p.Keywords) != 1 || p.Keywords[0].Text != "foo" {
		t.Errorf("keywords = %+v", p.Keywords)
	}
	if len(p.Links) != 2 || !p.Links[0].Internal || p.Links[1].Internal {
		t.Errorf("link internal flags wrong: %+v", p.Links)
	}
	if p.Links[0].TargetPageID != "Pb" || p.Links[1].TargetPageID != "Pz" {
		t.Errorf("link target page ids = %q, %q", p.Links[0].TargetPageID, p.Links[1].TargetPageID)
	}
	if p.Links[1].Name != "other-note" {
		t.Errorf("link name = %q want other-note", p.Links[1].Name)
	}
}

func TestListEnumeratesNotes(t *testing.T) {
	root := t.TempDir()
	a := archive.New(root)
	for _, id := range []string{"F_B", "F_A"} {
		writeNote(t, a, &snote.Note{FileID: id, Pages: []snote.Page{{ID: "Pa", Number: 1}}},
			map[string]string{"Pa": "<svg/>"})
	}
	ids, err := retrieve.List(a)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"F_A", "F_B"}; !reflect.DeepEqual(ids, want) {
		t.Errorf("List = %v want %v", ids, want)
	}
}
