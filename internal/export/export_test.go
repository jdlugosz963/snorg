package export

import (
	"strings"
	"testing"

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
				Titles:   []retrieve.TitleView{{Level: 1, Analysis: &retrieve.NameAnalysisView{Name: "Chapter 1"}}},
				Keywords: []retrieve.KeywordView{{Text: "alpha"}},
				Links: []retrieve.LinkView{{
					Name:         "biofizyka",
					TargetFileID: "F20260414171729084889FDefCgWZgV3D",
					Internal:     true,
					Analysis:     &retrieve.NameAnalysisView{Name: "potencjał czynnościowy"},
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

	got, err := Render(&retrieve.Result{Notes: []*retrieve.NoteView{view}}, tmpl)
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

// TestRenderExposesArchive: the context is the whole retrieve Result, so a
// template can read the archive root alongside notes (e.g. to build an absolute
// svg path).
func TestRenderExposesArchive(t *testing.T) {
	res := &retrieve.Result{
		Archive: "/home/u/notes/sn",
		Notes:   []*retrieve.NoteView{{Pages: []retrieve.PageView{{SVG: "F/Pa.svg"}}}},
	}
	got, err := Render(res, "{{ archive }}/{{ notes.0.pages.0.svg }}")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if want := "/home/u/notes/sn/F/Pa.svg"; got != want {
		t.Errorf("Render = %q, want %q", got, want)
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
	got, err := Render(&retrieve.Result{Notes: views}, tmpl)
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
	got, err := Render(&retrieve.Result{Notes: []*retrieve.NoteView{view}}, tmpl)
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
		"not-an-id":                         "",                // no leading digits
		"F12345":                            "",                // fewer than 14 digits
		"P12345":                            "",
	}
	for in, want := range cases {
		got, ok := denoteID(in)
		if got != want || ok != (want != "") {
			t.Errorf("denoteID(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
}

func TestDenoteFileID(t *testing.T) {
	cases := map[string]string{
		"20260915T001940--solaris__calibre.epub": "20260915T001940",
		"20260915T001940==sig--title.pdf":        "20260915T001940",
		"20260915T001940.pdf":                    "20260915T001940",
		"20260915T001940":                        "20260915T001940",
		"title@@20260915T001940__kw.epub":        "20260915T001940", // denote 4 reordered components
		"solaris.epub":                           "",
		"2026091500194--x.epub":                  "", // short id
		"20260915T001940x.epub":                  "", // no separator after the id
		"foo20260915T001940--x.epub":             "", // not leading, no @@
		"/storage/20260915T001940--solaris.epub": "", // a path, not a name
	}
	for in, want := range cases {
		got, ok := denoteFileID(in)
		if got != want || ok != (want != "") {
			t.Errorf("denoteFileID(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
}

func TestRenderDenote(t *testing.T) {
	view := &retrieve.NoteView{Pages: []retrieve.PageView{{
		Number: 1,
		Links: []retrieve.LinkView{
			{Kind: "file", Name: "20260915T001940--solaris__calibre.epub",
				Analysis: &retrieve.NameAnalysisView{Name: "Solaris"}},
			{Kind: "file", Name: "title@@20260916T101112.pdf"},
			{Kind: "file", Name: "Tom & Jerry.pdf"},
			{Kind: "note", Name: "x", TargetFileID: "F20260414171729084889FDefCgWZgV3D"},
		},
	}}}
	tmpl := "{% for l in notes.0.pages.0.links %}" +
		"{% with id=l.name|denote %}" +
		"{% if id %}[[denote:{{ id }}][{{ l.analysis.name|default:l.name }}]]\n" +
		"{% else %}{{ l.analysis.name|default:l.name }} {{ l.target_file_id|denote }}\n{% endif %}" +
		"{% endwith %}{% endfor %}"
	got, err := Render(&retrieve.Result{Notes: []*retrieve.NoteView{view}}, tmpl)
	if err != nil {
		t.Fatal(err)
	}
	want := "[[denote:20260915T001940][Solaris]]\n" +
		"[[denote:20260916T101112][title@@20260916T101112.pdf]]\n" +
		"Tom &amp; Jerry.pdf \n" +
		"x 20260414T171729\n"
	if got != want {
		t.Errorf("denote render = %q, want %q", got, want)
	}
}
