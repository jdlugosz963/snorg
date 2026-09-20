package archive

import (
	"testing"

	"github.com/jdlugosz963/snorg/internal/snote"
)

// noteWithTitle builds a one-page note whose page carries a single title at rect.
func noteWithTitle(pageID string, r snote.Rect) *snote.Note {
	return &snote.Note{
		FileID: "F_TEST",
		Pages: []snote.Page{{
			ID:     pageID,
			Number: 1,
			Titles: []snote.Title{{Rect: r, Level: 1}},
		}},
	}
}

// findPageReport returns the PageWriteReport for pageID, or fails.
func findPageReport(t *testing.T, r *WriteReport, pageID string) PageWriteReport {
	t.Helper()
	for _, p := range r.Pages {
		if p.PageID == pageID {
			return p
		}
	}
	t.Fatalf("no page report for %s in %+v", pageID, r.Pages)
	return PageWriteReport{}
}

func TestWriteReportFirstIngestMarksNew(t *testing.T) {
	a := New(t.TempDir())
	r, err := a.Write(note("Pa", "Pb"), svgMap(map[string]string{"Pa": "<svg>a</svg>", "Pb": "<svg>b</svg>"}))
	if err != nil {
		t.Fatal(err)
	}
	if !r.NoteChanged {
		t.Error("first ingest should mark note.json changed")
	}
	if len(r.Pages) != 2 {
		t.Fatalf("pages reported = %d want 2 (%+v)", len(r.Pages), r.Pages)
	}
	for _, p := range r.Pages {
		if !p.New || !p.JSONChanged || !p.SVGChanged {
			t.Errorf("page %s: want New/JSON/SVG all true, got %+v", p.PageID, p)
		}
	}
}

func TestWriteReportUnchangedReingestIsEmpty(t *testing.T) {
	a := New(t.TempDir())
	n := note("Pa")
	svgs := svgMap(map[string]string{"Pa": "<svg>a</svg>"})
	if _, err := a.Write(n, svgs); err != nil {
		t.Fatal(err)
	}
	r, err := a.Write(n, svgs)
	if err != nil {
		t.Fatal(err)
	}
	if r.NoteChanged || len(r.Pages) != 0 || len(r.Pruned) != 0 {
		t.Errorf("unchanged re-ingest should report nothing, got %+v", r)
	}
}

func TestWriteReportSVGChange(t *testing.T) {
	a := New(t.TempDir())
	n := note("Pa")
	if _, err := a.Write(n, svgMap(map[string]string{"Pa": "<svg>a</svg>"})); err != nil {
		t.Fatal(err)
	}
	r, err := a.Write(n, svgMap(map[string]string{"Pa": "<svg>EDITED</svg>"}))
	if err != nil {
		t.Fatal(err)
	}
	p := findPageReport(t, r, "Pa")
	if p.New {
		t.Error("re-ingested page should not be New")
	}
	if !p.SVGChanged {
		t.Error("edited SVG should report SVGChanged")
	}
	if p.JSONChanged {
		t.Error("unchanged page metadata should not report JSONChanged")
	}
}

func TestWriteReportPrune(t *testing.T) {
	a := New(t.TempDir())
	svgs := svgMap(map[string]string{"Pa": "<svg>a</svg>", "Pb": "<svg>b</svg>"})
	if _, err := a.Write(note("Pa", "Pb"), svgs); err != nil {
		t.Fatal(err)
	}
	r, err := a.Write(note("Pa"), svgMap(map[string]string{"Pa": "<svg>a</svg>"}))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Pruned) != 1 || r.Pruned[0] != "Pb" {
		t.Errorf("Pruned = %v want [Pb]", r.Pruned)
	}
}

// A title that carried an analysis but whose rect moved on re-ingest is reported as
// a dropped region (its analysis cannot be carried forward).
func TestWriteReportDroppedRegion(t *testing.T) {
	a := New(t.TempDir())
	oldRect := snote.Rect{X: 10, Y: 10, W: 100, H: 40}
	if _, err := a.Write(noteWithTitle("Pa", oldRect), svgMap(map[string]string{"Pa": "<svg/>"})); err != nil {
		t.Fatal(err)
	}

	// Simulate analyze giving the title an analysis.
	pd, err := a.ReadPage("F_TEST", "Pa")
	if err != nil {
		t.Fatal(err)
	}
	pd.Titles[0].Analysis = &TitleAnalysis{Name: "Chapter"}
	if _, err := a.WritePage("F_TEST", pd); err != nil {
		t.Fatal(err)
	}

	// Re-ingest with the title moved: its analysis is dropped.
	newRect := snote.Rect{X: 500, Y: 600, W: 100, H: 40}
	r, err := a.Write(noteWithTitle("Pa", newRect), svgMap(map[string]string{"Pa": "<svg/>"}))
	if err != nil {
		t.Fatal(err)
	}
	p := findPageReport(t, r, "Pa")
	if len(p.DroppedRegions) != 1 {
		t.Fatalf("DroppedRegions = %+v want one", p.DroppedRegions)
	}
	if p.DroppedRegions[0].Kind != "title" || p.DroppedRegions[0].Rect != oldRect {
		t.Errorf("dropped = %+v want title at %+v", p.DroppedRegions[0], oldRect)
	}
}
