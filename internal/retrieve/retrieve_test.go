package retrieve_test

import (
	"path/filepath"
	"reflect"
	"strings"
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
	if _, err := a.Write(n, m); err != nil {
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
					{Rect: snote.Rect{X: 5}, Kind: snote.LinkNote, TargetPageID: "Pb", TargetFileID: "F_TEST"},                      // internal note
					{Rect: snote.Rect{X: 6}, Kind: snote.LinkNote, TargetPageID: "Pz", TargetFileID: "F_OTHER", Name: "other-note"}, // external note
					{Rect: snote.Rect{X: 7}, Kind: snote.LinkWeb, Target: "https://example.com", Name: "https://example.com"},       // web
					{Rect: snote.Rect{X: 8}, Kind: snote.LinkFile, Target: "/x/Book.pdf", TargetDocPage: 12, Name: "Book.pdf"},      // file → doc page 12
				},
			},
			{ID: "Pb", Number: 2},
		},
	}
	writeNote(t, a, n, map[string]string{"Pa": "<svg>a</svg>", "Pb": "<svg>b</svg>"})

	// Argument order is irrelevant: pages come back in placement order.
	res, err := retrieve.Get(a, []string{"Pb", "Pa"})
	if err != nil {
		t.Fatal(err)
	}
	// The result carries the absolute archive root the svg paths resolve against.
	if !filepath.IsAbs(res.Archive) || filepath.Clean(res.Archive) != filepath.Clean(root) {
		t.Errorf("archive = %q, want absolute %q", res.Archive, root)
	}
	views := res.Notes
	if len(views) != 1 {
		t.Fatalf("views = %d want 1", len(views))
	}
	view := views[0]

	if view.FileID != "F_TEST" || view.Source != "note.note" {
		t.Errorf("note metadata = %+v", view)
	}
	if len(view.Pages) != 2 || view.Pages[0].PageID != "Pa" || view.Pages[1].PageID != "Pb" {
		t.Fatalf("pages not in placement order: %+v", view.Pages)
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
	if len(p.Links) != 4 || !p.Links[0].Internal || p.Links[1].Internal || p.Links[2].Internal || p.Links[3].Internal {
		t.Errorf("link internal flags wrong: %+v", p.Links)
	}
	if p.Links[0].TargetPageID != "Pb" || p.Links[1].TargetPageID != "Pz" {
		t.Errorf("link target page ids = %q, %q", p.Links[0].TargetPageID, p.Links[1].TargetPageID)
	}
	if p.Links[1].Name != "other-note" {
		t.Errorf("link name = %q want other-note", p.Links[1].Name)
	}
	// Kind and (for web) the decoded target surface in the view.
	if p.Links[0].Kind != "note" || p.Links[2].Kind != "web" || p.Links[3].Kind != "file" {
		t.Errorf("link kinds = %q, %q, %q want note, web, file", p.Links[0].Kind, p.Links[2].Kind, p.Links[3].Kind)
	}
	if p.Links[2].Target != "https://example.com" {
		t.Errorf("web link target = %q want https://example.com", p.Links[2].Target)
	}
	// A file link carries the page within the linked document; others carry 0.
	if p.Links[3].Target != "/x/Book.pdf" || p.Links[3].TargetDocPage != 12 {
		t.Errorf("file link target=%q docpage=%d want /x/Book.pdf, 12", p.Links[3].Target, p.Links[3].TargetDocPage)
	}
	if p.Links[0].TargetDocPage != 0 {
		t.Errorf("note link should carry no doc page, got %d", p.Links[0].TargetDocPage)
	}
	// Target ids are note-link-only; web/file links leave them empty (omitted).
	if p.Links[2].TargetFileID != "" || p.Links[2].TargetPageID != "" ||
		p.Links[3].TargetFileID != "" || p.Links[3].TargetPageID != "" {
		t.Errorf("web/file links should carry no target ids, got %+v %+v", p.Links[2], p.Links[3])
	}
}

// TestGetGroupsByNote: PAGEIDs spanning notes come back as one NoteView per
// owning note (archive List order), holding only the requested pages;
// duplicates are deduplicated.
func TestGetGroupsByNote(t *testing.T) {
	root := t.TempDir()
	a := archive.New(root)
	writeNote(t, a, &snote.Note{FileID: "F_B", Source: "b.note", Pages: []snote.Page{
		{ID: "Pb1", Number: 1}, {ID: "Pb2", Number: 2},
	}}, map[string]string{"Pb1": "<svg/>", "Pb2": "<svg/>"})
	writeNote(t, a, &snote.Note{FileID: "F_A", Source: "a.note", Pages: []snote.Page{
		{ID: "Pa1", Number: 1},
	}}, map[string]string{"Pa1": "<svg/>"})

	res, err := retrieve.Get(a, []string{"Pb2", "Pa1", "Pb2"})
	if err != nil {
		t.Fatal(err)
	}
	views := res.Notes
	if len(views) != 2 || views[0].FileID != "F_A" || views[1].FileID != "F_B" {
		t.Fatalf("views = %+v want F_A then F_B", views)
	}
	if len(views[0].Pages) != 1 || views[0].Pages[0].PageID != "Pa1" {
		t.Errorf("F_A pages = %+v", views[0].Pages)
	}
	if len(views[1].Pages) != 1 || views[1].Pages[0].PageID != "Pb2" {
		t.Errorf("F_B pages = %+v want only the requested Pb2", views[1].Pages)
	}
}

func TestGetUnknownPageIDFails(t *testing.T) {
	root := t.TempDir()
	a := archive.New(root)
	writeNote(t, a, &snote.Note{FileID: "F_TEST", Pages: []snote.Page{{ID: "Pa", Number: 1}}},
		map[string]string{"Pa": "<svg/>"})

	_, err := retrieve.Get(a, []string{"Pa", "P_MISSING"})
	if err == nil || !strings.Contains(err.Error(), "P_MISSING") {
		t.Fatalf("err = %v, want a not-found error naming P_MISSING", err)
	}
}

// TestGetAssemblesAnalysis: an analyzed page surfaces its sidecar content and
// fields under analysis, with per-region transcriptions nested on titles/links —
// the same structure the archive stores on disk.
func TestGetAssemblesAnalysis(t *testing.T) {
	root := t.TempDir()
	a := archive.New(root)
	n := &snote.Note{
		FileID: "F_TEST",
		Pages: []snote.Page{{
			ID: "Pa", Number: 1,
			Titles: []snote.Title{{Rect: snote.Rect{X: 1}, Level: 1}},
			Links:  []snote.Link{{Rect: snote.Rect{X: 2}, TargetPageID: "Pb", TargetFileID: "F_TEST"}},
		}},
	}
	writeNote(t, a, n, map[string]string{"Pa": "<svg/>"})

	pd, err := a.ReadPage("F_TEST", "Pa")
	if err != nil {
		t.Fatal(err)
	}
	pd.Titles[0].Analysis = &archive.TitleAnalysis{Name: "Chapter", Edited: true}
	pd.Links[0].Analysis = &archive.LinkAnalysis{Name: "see also", Edited: true}
	pd.Analysis = &archive.PageAnalysis{SourceHash: "abc", Fields: map[string]string{"description": "short"}}
	if _, err := a.WritePage("F_TEST", pd); err != nil {
		t.Fatal(err)
	}
	if err := a.WriteAnalysisMD("F_TEST", "Pa", "# Chapter\n\nbody"); err != nil {
		t.Fatal(err)
	}

	res, err := retrieve.Get(a, []string{"Pa"})
	if err != nil {
		t.Fatal(err)
	}
	views := res.Notes
	p := views[0].Pages[0]
	if p.Analysis == nil || p.Analysis.Content != "# Chapter\n\nbody" {
		t.Errorf("analysis = %+v, want content from sidecar without trailing newline", p.Analysis)
	}
	if p.Analysis.Fields["description"] != "short" {
		t.Errorf("fields = %+v", p.Analysis.Fields)
	}
	if p.Titles[0].Analysis == nil || p.Titles[0].Analysis.Name != "Chapter" {
		t.Errorf("title analysis = %+v", p.Titles[0].Analysis)
	}
	if p.Links[0].Analysis == nil || p.Links[0].Analysis.Name != "see also" {
		t.Errorf("link analysis = %+v", p.Links[0].Analysis)
	}
}

// TestGetExposesHumanTranscription: a page never AI-analyzed but transcribed
// by hand (analyze-edit) still surfaces its md as analysis.content — without
// fields, which exist only once the page was AI-analyzed.
func TestGetExposesHumanTranscription(t *testing.T) {
	a := archive.New(t.TempDir())
	n := &snote.Note{FileID: "F_TEST", Pages: []snote.Page{{ID: "Pa", Number: 1}, {ID: "Pb", Number: 2}}}
	writeNote(t, a, n, map[string]string{"Pa": "<svg/>", "Pb": "<svg/>"})
	if err := a.WriteAnalysisMD("F_TEST", "Pa", "written by hand"); err != nil {
		t.Fatal(err)
	}

	res, err := retrieve.Get(a, []string{"Pa", "Pb"})
	if err != nil {
		t.Fatal(err)
	}
	views := res.Notes
	pa, pb := views[0].Pages[0], views[0].Pages[1]
	if pa.Analysis == nil || pa.Analysis.Content != "written by hand" {
		t.Errorf("analysis = %+v, want the hand-written content", pa.Analysis)
	}
	if pa.Analysis != nil && pa.Analysis.Fields != nil {
		t.Errorf("fields = %+v, want none without AI analysis", pa.Analysis.Fields)
	}
	if pb.Analysis != nil {
		t.Errorf("page without md or analysis got %+v, want nil", pb.Analysis)
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

// A note tag surfaces on the NoteView and is unioned into every page's effective
// tags, while the page docs on disk keep only their own.
func TestGetUnionsNoteTags(t *testing.T) {
	a := archive.New(t.TempDir())
	writeNote(t, a, &snote.Note{
		FileID: "F_TAGS",
		Source: "note.note",
		Pages:  []snote.Page{{ID: "Pa", Number: 1}, {ID: "Pb", Number: 2}},
	}, map[string]string{"Pa": "<svg/>", "Pb": "<svg/>"})
	if _, err := a.TagNote("F_TAGS", "work", false); err != nil {
		t.Fatal(err)
	}
	if _, err := a.TagPage("Pa", "todo", false); err != nil {
		t.Fatal(err)
	}

	res, err := retrieve.Get(a, []string{"Pa", "Pb"})
	if err != nil {
		t.Fatal(err)
	}
	note := res.Notes[0]
	if want := []string{"work"}; !reflect.DeepEqual(note.Tags, want) {
		t.Errorf("note tags = %v, want %v", note.Tags, want)
	}
	if want := []string{"todo", "work"}; !reflect.DeepEqual(note.Pages[0].Tags, want) {
		t.Errorf("Pa tags = %v, want the sorted union %v", note.Pages[0].Tags, want)
	}
	if want := []string{"work"}; !reflect.DeepEqual(note.Pages[1].Tags, want) {
		t.Errorf("Pb tags = %v, want the inherited %v", note.Pages[1].Tags, want)
	}
	// Inheritance is a read-time union: Pb's own doc stays tag-free.
	pd, err := a.ReadPage("F_TAGS", "Pb")
	if err != nil {
		t.Fatal(err)
	}
	if len(pd.Tags) != 0 {
		t.Errorf("stored Pb tags = %v, want empty", pd.Tags)
	}
}
