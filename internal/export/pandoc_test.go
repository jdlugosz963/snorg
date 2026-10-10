package export

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/jdlugosz963/snorg/internal/retrieve"
)

func renderContent(t *testing.T, content, tpl string) (string, error) {
	t.Helper()
	view := &retrieve.NoteView{
		Pages: []retrieve.PageView{{Number: 1, Analysis: &retrieve.PageAnalysisView{Content: content}}},
	}
	return Render(&retrieve.Result{Notes: []*retrieve.NoteView{view}}, tpl)
}

func needPandoc(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("pandoc"); err != nil {
		t.Skip("pandoc not found on PATH")
	}
}

func TestPandocOrg(t *testing.T) {
	needPandoc(t)
	got, err := renderContent(t, "# Heading\n\nsome **bold** text",
		`{{ notes.0.pages.0.analysis.content|pandoc:"org"|nestheadings:"*" }}`)
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
	if strings.Contains(got, ":PROPERTIES:") {
		t.Errorf("heading carries an auto-identifier drawer:\n%s", got)
	}
}

func TestPandocHTML(t *testing.T) {
	needPandoc(t)
	got, err := renderContent(t, "# Heading\n\nsome **bold** text",
		`{{ notes.0.pages.0.analysis.content|pandoc:"html" }}`)
	if err != nil {
		t.Fatal(err)
	}
	// The filter marks the result safe, so the tags must survive unescaped.
	if !strings.Contains(got, "<h1>Heading</h1>") {
		t.Errorf("markdown heading not converted to a bare <h1>:\n%s", got)
	}
	if !strings.Contains(got, "<strong>bold</strong>") {
		t.Errorf("bold not converted to <strong>:\n%s", got)
	}
}

// TestPandocEmptyInput: unanalyzed pages flow "" through the filter and must
// not invoke pandoc (the export then works on a pandoc-less machine too).
func TestPandocEmptyInput(t *testing.T) {
	view := &retrieve.NoteView{Pages: []retrieve.PageView{{Number: 1}}}
	got, err := Render(&retrieve.Result{Notes: []*retrieve.NoteView{view}},
		`<{{ notes.0.pages.0.analysis.content|pandoc:"org" }}>`)
	if err != nil {
		t.Fatal(err)
	}
	if got != "<>" {
		t.Errorf("empty content through pandoc = %q, want <>", got)
	}
}

func TestPandocMissingFormat(t *testing.T) {
	for _, tpl := range []string{
		`{{ notes.0.pages.0.analysis.content|pandoc }}`,
		`{{ notes.0.pages.0.analysis.content|pandoc:"" }}`,
	} {
		if _, err := renderContent(t, "text", tpl); err == nil || !strings.Contains(err.Error(), "output format") {
			t.Errorf("%s: err = %v, want missing output format", tpl, err)
		}
	}
}
