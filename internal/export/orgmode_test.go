package export

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/jdlugosz963/snorg/internal/config"
	"github.com/jdlugosz963/snorg/internal/retrieve"
)

func TestNestOrgHeadings(t *testing.T) {
	view := &retrieve.NoteView{
		Pages: []retrieve.PageView{{
			Number:   1,
			Analysis: &retrieve.PageAnalysisView{Content: "* H1\ntext with * inside\n** H2\n- list item"},
		}},
	}
	got, err := Render([]*retrieve.NoteView{view}, "{{ notes.0.pages.0.analysis.content|nestorgheadings:2 }}")
	if err != nil {
		t.Fatal(err)
	}
	want := "*** H1\ntext with * inside\n**** H2\n- list item"
	if got != want {
		t.Errorf("nestorgheadings = %q, want %q", got, want)
	}
}

func TestNestOrgHeadingsDefaultsToOne(t *testing.T) {
	view := &retrieve.NoteView{
		Pages: []retrieve.PageView{{Number: 1, Analysis: &retrieve.PageAnalysisView{Content: "* H"}}},
	}
	got, err := Render([]*retrieve.NoteView{view}, "{{ notes.0.pages.0.analysis.content|nestorgheadings }}")
	if err != nil {
		t.Fatal(err)
	}
	if got != "** H" {
		t.Errorf("nestorgheadings = %q, want ** H", got)
	}
}

func TestOrgFilter(t *testing.T) {
	if _, err := exec.LookPath("pandoc"); err != nil {
		t.Skip("pandoc not found on PATH")
	}
	view := &retrieve.NoteView{
		Pages: []retrieve.PageView{{
			Number:   1,
			Analysis: &retrieve.PageAnalysisView{Content: "# Heading\n\nsome **bold** text"},
		}},
	}
	got, err := Render([]*retrieve.NoteView{view}, "{{ notes.0.pages.0.analysis.content|org|nestorgheadings:1 }}")
	if err != nil {
		t.Fatal(err)
	}
	// Loose assertions: pandoc's org output varies across versions.
	if !strings.Contains(got, "** Heading") {
		t.Errorf("markdown heading not converted+demoted to ** Heading:\n%s", got)
	}
	if !strings.Contains(got, "*bold*") {
		t.Errorf("bold not converted to org emphasis:\n%s", got)
	}
}

// TestOrgmodeExample renders the shipped examples/emacs/orgmode.yaml over a realistic
// view: per-page headings (title transcription or Page N fallback), page properties,
// the page SVG snorg link, demoted content headings and denote-snorg links. (The
// denote header and tags are produced by snorg.el in elisp, not this template.)
func TestOrgmodeExample(t *testing.T) {
	if _, err := exec.LookPath("pandoc"); err != nil {
		t.Skip("pandoc not found on PATH")
	}
	cfg, err := config.Load([]string{"../../examples/emacs/orgmode.yaml"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Export.Template == "" {
		t.Fatal("examples/emacs/orgmode.yaml has no export.template")
	}

	view := &retrieve.NoteView{
		FileID: "F20260414171729084889FDefCgWZgV3D",
		Source: "biofizyka.note",
		Pages: []retrieve.PageView{
			{
				Number:  1,
				PageID:  "P20260414171730000001aaaaaa",
				Starred: true,
				SVG:     "F20260414171729084889FDefCgWZgV3D/P20260414171730000001aaaaaa.svg",
				Titles:  []retrieve.TitleView{{Level: 1, Analysis: &retrieve.NameAnalysisView{Name: "Neurony"}}},
				Links: []retrieve.LinkView{{
					Name:         "komorki.note",
					TargetFileID: "F20260629154102100593mO9IZI46DNYe",
					TargetPageID: "P20260629154103000001bbbbbb",
					Analysis:     &retrieve.NameAnalysisView{Name: "biologia komórki"},
				}},
				Analysis: &retrieve.PageAnalysisView{
					Content: "# Potencjał czynnościowy\n\nnotatki z wykładu",
				},
			},
			{
				// Unanalyzed page: heading falls back to the page number.
				Number: 2,
				PageID: "P20260414171731000002cccccc",
				SVG:    "F20260414171729084889FDefCgWZgV3D/P20260414171731000002cccccc.svg",
			},
		},
	}

	got, err := Render([]*retrieve.NoteView{view}, cfg.Export.Template)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"** Neurony",
		":SNORG_PAGEID: P20260414171730000001aaaaaa",
		":SNORG_SVGP: F20260414171729084889FDefCgWZgV3D/P20260414171730000001aaaaaa.svg",
		":STARRED: t",
		"[[snorg:F20260414171729084889FDefCgWZgV3D/P20260414171730000001aaaaaa.svg][page 1]]",
		"*** Potencjał czynnościowy", // markdown # -> org * demoted 2 under the page heading
		"- [[denote-snorg:20260629T154102::P20260629154103000001bbbbbb][biologia komórki]]",
		"** Page 2",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q\n--- got ---\n%s", want, got)
		}
	}
	if strings.Contains(got, "\n\n\n") {
		t.Errorf("output has runs of blank lines:\n%s", got)
	}
}

// TestOrgFilterEmptyInput: unanalyzed pages flow "" through the filter and must
// not invoke pandoc (the export then works on a pandoc-less machine too).
func TestOrgFilterEmptyInput(t *testing.T) {
	view := &retrieve.NoteView{Pages: []retrieve.PageView{{Number: 1}}}
	got, err := Render([]*retrieve.NoteView{view}, "<{{ notes.0.pages.0.analysis.content|org }}>")
	if err != nil {
		t.Fatal(err)
	}
	if got != "<>" {
		t.Errorf("empty content through org = %q, want <>", got)
	}
}
