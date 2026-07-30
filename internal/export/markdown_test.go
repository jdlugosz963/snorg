package export

import (
	"strings"
	"testing"

	"github.com/jdlugosz963/snorg/internal/config"
	"github.com/jdlugosz963/snorg/internal/retrieve"
)

func TestNestMDHeadings(t *testing.T) {
	view := &retrieve.NoteView{
		Pages: []retrieve.PageView{{
			Number:   1,
			Analysis: &retrieve.PageAnalysisView{Content: "# H1\ntext with # inside\n## H2\n- list item"},
		}},
	}
	got, err := Render(&retrieve.Result{Notes: []*retrieve.NoteView{view}}, "{{ notes.0.pages.0.analysis.content|nestmdheadings:2 }}")
	if err != nil {
		t.Fatal(err)
	}
	want := "### H1\ntext with # inside\n#### H2\n- list item"
	if got != want {
		t.Errorf("nestmdheadings = %q, want %q", got, want)
	}
}

func TestNestMDHeadingsDefaultsToOne(t *testing.T) {
	view := &retrieve.NoteView{
		Pages: []retrieve.PageView{{Number: 1, Analysis: &retrieve.PageAnalysisView{Content: "# H"}}},
	}
	got, err := Render(&retrieve.Result{Notes: []*retrieve.NoteView{view}}, "{{ notes.0.pages.0.analysis.content|nestmdheadings }}")
	if err != nil {
		t.Fatal(err)
	}
	if got != "## H" {
		t.Errorf("nestmdheadings = %q, want ## H", got)
	}
}

// TestMarkdownExample renders the shipped examples/config.yaml over a realistic
// view: note title, per-page headings (starred marker), keyword chips, page SVG,
// demoted content headings and note links. Needs no pandoc (pure Markdown).
func TestMarkdownExample(t *testing.T) {
	cfg, err := config.Load([]string{"../../examples/config.yaml"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Export.Template == "" {
		t.Fatal("examples/config.yaml has no export.template")
	}

	view := &retrieve.NoteView{
		FileID: "F20260414171729084889FDefCgWZgV3D",
		Source: "biofizyka.note",
		Pages: []retrieve.PageView{
			{
				Number:   1,
				PageID:   "P20260414171730000001aaaaaa",
				Starred:  true,
				SVG:      "F20260414171729084889FDefCgWZgV3D/P20260414171730000001aaaaaa.svg",
				Keywords: []retrieve.KeywordView{{Text: "biofizyka"}, {Text: "neurony"}},
				Links: []retrieve.LinkView{{
					Name:         "komorki.note",
					TargetPageID: "P20260629154103000001bbbbbb",
					Analysis:     &retrieve.NameAnalysisView{Name: "biologia komórki"},
				}},
				Analysis: &retrieve.PageAnalysisView{Content: "# Potencjał czynnościowy\n\nnotatki z wykładu"},
			},
			{
				// Unanalyzed, unstarred page with no keywords/links.
				Number: 2,
				PageID: "P20260414171731000002cccccc",
				SVG:    "F20260414171729084889FDefCgWZgV3D/P20260414171731000002cccccc.svg",
			},
		},
	}

	got, err := Render(&retrieve.Result{Notes: []*retrieve.NoteView{view}}, cfg.Export.Template)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"# biofizyka",
		"## ⭐ Page 1",
		"`biofizyka` `neurony`",
		"![Page 1](F20260414171729084889FDefCgWZgV3D/P20260414171730000001aaaaaa.svg)",
		"### Potencjał czynnościowy", // markdown # demoted by 2 under the page heading
		"**Links**",
		"- biologia komórki",
		"## Page 2",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q\n--- got ---\n%s", want, got)
		}
	}
	// Page 2 has no keyword chips / links section (no run-together artifacts).
	if strings.Contains(got, "## ⭐ Page 2") {
		t.Errorf("unstarred page 2 wrongly marked:\n%s", got)
	}
	if strings.Contains(got, "\n\n\n") {
		t.Errorf("output has runs of blank lines:\n%s", got)
	}
}
