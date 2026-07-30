package export

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/jdlugosz963/snorg/internal/retrieve"
)

func TestNestOrgHeadings(t *testing.T) {
	view := &retrieve.NoteView{
		Pages: []retrieve.PageView{{
			Number:   1,
			Analysis: &retrieve.PageAnalysisView{Content: "* H1\ntext with * inside\n** H2\n- list item"},
		}},
	}
	got, err := Render(&retrieve.Result{Notes: []*retrieve.NoteView{view}}, "{{ notes.0.pages.0.analysis.content|nestorgheadings:2 }}")
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
	got, err := Render(&retrieve.Result{Notes: []*retrieve.NoteView{view}}, "{{ notes.0.pages.0.analysis.content|nestorgheadings }}")
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
	got, err := Render(&retrieve.Result{Notes: []*retrieve.NoteView{view}}, "{{ notes.0.pages.0.analysis.content|org|nestorgheadings:1 }}")
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

// TestOrgFilterEmptyInput: unanalyzed pages flow "" through the filter and must
// not invoke pandoc (the export then works on a pandoc-less machine too).
func TestOrgFilterEmptyInput(t *testing.T) {
	view := &retrieve.NoteView{Pages: []retrieve.PageView{{Number: 1}}}
	got, err := Render(&retrieve.Result{Notes: []*retrieve.NoteView{view}}, "<{{ notes.0.pages.0.analysis.content|org }}>")
	if err != nil {
		t.Fatal(err)
	}
	if got != "<>" {
		t.Errorf("empty content through org = %q, want <>", got)
	}
}
