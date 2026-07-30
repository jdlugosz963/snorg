package export

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/jdlugosz963/snorg/internal/config"
	"github.com/jdlugosz963/snorg/internal/retrieve"
)

func TestHTMLFilter(t *testing.T) {
	if _, err := exec.LookPath("pandoc"); err != nil {
		t.Skip("pandoc not found on PATH")
	}
	view := &retrieve.NoteView{
		Pages: []retrieve.PageView{{
			Number:   1,
			Analysis: &retrieve.PageAnalysisView{Content: "# Heading\n\nsome **bold** text"},
		}},
	}
	got, err := Render(&retrieve.Result{Notes: []*retrieve.NoteView{view}}, "{{ notes.0.pages.0.analysis.content|html }}")
	if err != nil {
		t.Fatal(err)
	}
	// Loose assertions: pandoc's html output varies across versions. The filter
	// marks the result safe, so the tags must survive unescaped.
	if !strings.Contains(got, "<h1") || !strings.Contains(got, "Heading</h1>") {
		t.Errorf("markdown heading not converted to <h1>:\n%s", got)
	}
	if !strings.Contains(got, "<strong>bold</strong>") {
		t.Errorf("bold not converted to <strong>:\n%s", got)
	}
}

// TestHTMLFilterEmptyInput: unanalyzed pages flow "" through the filter and must
// not invoke pandoc (the export then works on a pandoc-less machine too).
func TestHTMLFilterEmptyInput(t *testing.T) {
	view := &retrieve.NoteView{Pages: []retrieve.PageView{{Number: 1}}}
	got, err := Render(&retrieve.Result{Notes: []*retrieve.NoteView{view}}, "<{{ notes.0.pages.0.analysis.content|html }}>")
	if err != nil {
		t.Fatal(err)
	}
	if got != "<>" {
		t.Errorf("empty content through html = %q, want <>", got)
	}
}

// TestWebNoteExample renders the shipped examples/web/note.yaml over a realistic
// single-note view: per-page section (⭐ on starred), the page SVG <img> at the
// archive-relative path, keyword chips, the analysis as an HTML fragment and an
// internal link to the target note's <FILE_ID>.html.
func TestWebNoteExample(t *testing.T) {
	if _, err := exec.LookPath("pandoc"); err != nil {
		t.Skip("pandoc not found on PATH")
	}
	cfg, err := config.Load([]string{"../../examples/web/note.yaml"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Export.Template == "" {
		t.Fatal("examples/web/note.yaml has no export.template")
	}

	view := &retrieve.NoteView{
		FileID: "F20260414171729084889FDefCgWZgV3D",
		Source: "biofizyka.note",
		Pages: []retrieve.PageView{{
			Number:   1,
			PageID:   "P20260414171730000001aaaaaa",
			Starred:  true,
			SVG:      "F20260414171729084889FDefCgWZgV3D/P20260414171730000001aaaaaa.svg",
			Keywords: []retrieve.KeywordView{{Text: "neuron"}},
			Links: []retrieve.LinkView{{
				Name:         "komorki.note",
				TargetFileID: "F20260629154102100593mO9IZI46DNYe",
				TargetPageID: "P20260629154103000001bbbbbb",
			}},
			Analysis: &retrieve.PageAnalysisView{Content: "# Potencjał\n\nnotatki z **wykładu**"},
		}},
	}

	got, err := Render(&retrieve.Result{Notes: []*retrieve.NoteView{view}}, cfg.Export.Template)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"biofizyka",         // note title in the page
		`<a href="#page1">`, // table-of-contents entry (numeric anchor kept)
		`<section class="page" id="P20260414171730000001aaaaaa">`,         // section anchored by page id
		`<span class="anchor" id="page1">`,                                // numeric anchor still resolves
		`<h2><a class="page-anchor" href="#P20260414171730000001aaaaaa">`, // heading links to its own #pageid
		`<img src="F20260414171729084889FDefCgWZgV3D/P20260414171730000001aaaaaa.svg"`,
		"⭐",
		"neuron",                       // keyword chip
		"<details class=\"analysis\">", // transcription collapsed behind a toggle
		"<strong>wykładu</strong>",     // analysis rendered as html
		`href="F20260629154102100593mO9IZI46DNYe.html#P20260629154103000001bbbbbb"`, // internal link -> target note page + page anchor
		`href="index.html"`, // back-link
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q\n--- got ---\n%s", want, got)
		}
	}
}

// TestWebIndexExample renders examples/web/index.yaml over pages spanning two
// notes: one listing with a link + page count per note.
func TestWebIndexExample(t *testing.T) {
	cfg, err := config.Load([]string{"../../examples/web/index.yaml"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Export.Template == "" {
		t.Fatal("examples/web/index.yaml has no export.template")
	}

	views := []*retrieve.NoteView{
		{
			FileID: "F20260414171729084889FDefCgWZgV3D",
			Source: "biofizyka.note",
			Pages: []retrieve.PageView{
				{Number: 1, PageID: "P1"}, {Number: 2, PageID: "P2"},
			},
		},
		{
			FileID: "F20260629154102100593mO9IZI46DNYe",
			Source: "komorki.note",
			Pages:  []retrieve.PageView{{Number: 5, PageID: "P3"}},
		},
	}

	got, err := Render(&retrieve.Result{Notes: views}, cfg.Export.Template)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`href="F20260414171729084889FDefCgWZgV3D.html"`,
		"biofizyka",
		`href="F20260629154102100593mO9IZI46DNYe.html"`,
		"komorki",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q\n--- got ---\n%s", want, got)
		}
	}
}
