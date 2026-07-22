package export

import (
	"strings"
	"testing"

	"github.com/jdlugosz963/snorg/internal/archive"
	"github.com/jdlugosz963/snorg/internal/retrieve"
)

func TestRender(t *testing.T) {
	view := &retrieve.NoteView{
		FileID: "F_TEST",
		Pages: []retrieve.PageView{
			{
				Number:   1,
				PageID:   "Pa",
				SVG:      "F_TEST/Pa.svg",
				Titles:   []retrieve.TitleView{{Level: 1, Analysis: &archive.TitleAnalysis{Name: "Chapter 1"}}},
				Keywords: []retrieve.KeywordView{{Text: "alpha"}},
				Links: []retrieve.LinkView{{
					Name:         "biofizyka",
					TargetFileID: "F20260414171729084889FDefCgWZgV3D",
					Internal:     true,
					Analysis:     &archive.LinkAnalysis{Name: "potencjał czynnościowy"},
				}},
				Analysis: &retrieve.PageAnalysisView{Content: "first page text"},
			},
			{
				// No analysis: page.analysis.content must render blank, not error,
				// and a link without analysis must leave link.analysis.name blank.
				Number: 2,
				PageID: "Pb",
				SVG:    "F_TEST/Pb.svg",
				Links:  []retrieve.LinkView{{Name: "other", TargetFileID: "F20260629154102100593mO9IZI46DNYe"}},
			},
		},
	}

	const tmpl = "{% for p in notes.0.pages %}* Page {{ p.number }} [{{ p.page_id }}]\n" +
		"{{ p.analysis.content }}\n" +
		"{% for t in p.titles %}- L{{ t.level }} {{ t.analysis.name }}\n{% endfor %}" +
		"{% for k in p.keywords %}kw:{{ k.text }}\n{% endfor %}" +
		"{% for link in p.links %}link:[[denote:{{ link.target_file_id|denote }}]] {{ link.name }} <{{ link.analysis.name }}>\n{% endfor %}" +
		"{% endfor %}"

	got, err := Render([]*retrieve.NoteView{view}, tmpl)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, want := range []string{
		"* Page 1 [Pa]",
		"first page text",
		"- L1 Chapter 1",
		"kw:alpha",
		"link:[[denote:20260414T171729]] biofizyka <potencjał czynnościowy>",
		"* Page 2 [Pb]",
		"link:[[denote:20260629T154102]] other <>", // no analysis -> blank
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q\n--- got ---\n%s", want, got)
		}
	}
}

func TestRenderBadTemplate(t *testing.T) {
	if _, err := Render(nil, "{% for %}"); err == nil {
		t.Fatal("expected parse error for malformed template")
	}
}

// TestRenderSpansNotes: one render sees every note, so a template can emit a
// shared root once and pages from many notes flat under it.
func TestRenderSpansNotes(t *testing.T) {
	views := []*retrieve.NoteView{
		{Source: "a.note", Pages: []retrieve.PageView{{Number: 1, PageID: "Pa"}}},
		{Source: "b.note", Pages: []retrieve.PageView{{Number: 3, PageID: "Pb"}}},
	}
	const tmpl = "* root\n" +
		"{% for note in notes %}\n" +
		"{% for p in note.pages %}\n" +
		"** {{ note.source }} {{ p.page_id }}\n" +
		"{% endfor %}\n" +
		"{% endfor %}"
	got, err := Render(views, tmpl)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if want := "* root\n** a.note Pa\n** b.note Pb\n"; got != want {
		t.Errorf("Render = %q, want %q", got, want)
	}
}

// TestRenderTrimsBlocks proves trim_blocks + lstrip_blocks are active: a readable
// multi-line loop (tags on their own indented lines) renders with no blank lines and no
// leading indentation leaked from the scaffolding.
func TestRenderTrimsBlocks(t *testing.T) {
	view := &retrieve.NoteView{Pages: []retrieve.PageView{{Number: 1}, {Number: 2}}}
	// Block tags on their own indented lines (including if/endif) — the clean style.
	const tmpl = "{% for p in notes.0.pages %}\n" +
		"  {% if p.number %}\n" +
		"- {{ p.number }}\n" +
		"  {% endif %}\n" +
		"{% endfor %}"
	got, err := Render([]*retrieve.NoteView{view}, tmpl)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if want := "- 1\n- 2\n"; got != want {
		t.Errorf("Render = %q, want %q", got, want)
	}
}

func TestDenoteID(t *testing.T) {
	cases := map[string]string{
		"F20260414171729084889FDefCgWZgV3D": "20260414T171729",
		"F20260629154102100593mO9IZI46DNYe": "20260629T154102",
		"P20260414171729084889abcdef":       "20260414T171729", // PAGEIDs share the layout
		"not-an-id":                         "not-an-id",       // no leading digits -> unchanged
		"F12345":                            "F12345",          // fewer than 14 digits -> unchanged
		"P12345":                            "P12345",
	}
	for in, want := range cases {
		if got := denoteID(in); got != want {
			t.Errorf("denoteID(%q) = %q, want %q", in, got, want)
		}
	}
}
