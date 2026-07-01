package archive

import (
	"strings"
	"testing"

	"github.com/jdlugosz963/snorg/internal/snote"
)

// linkedNote is a note whose page src holds a link to (targetFileID,targetPageID).
func linkedNote(fileID, src, targetFileID, targetPageID string) *snote.Note {
	return &snote.Note{
		FileID: fileID,
		Pages: []snote.Page{{
			ID:     src,
			Number: 1,
			Links: []snote.Link{{
				Rect:         snote.Rect{X: 10, Y: 20, W: 30, H: 40},
				TargetPageID: targetPageID,
				TargetFileID: targetFileID,
			}},
		}},
	}
}

func TestInjectLinksSameNote(t *testing.T) {
	a := New(t.TempDir())
	n := linkedNote("F_TEST", "Pa", "F_TEST", "Pb")
	n.Pages = append(n.Pages, snote.Page{ID: "Pb", Number: 2})
	if err := a.Write(n, svgMap(map[string]string{"Pa": "<svg>a</svg>", "Pb": "<svg>b</svg>"})); err != nil {
		t.Fatal(err)
	}
	svg, err := a.ReadSVG("F_TEST", "Pa")
	if err != nil {
		t.Fatal(err)
	}
	want := `<a xlink:href="Pb.svg"><rect x="10" y="20" width="30" height="40" fill="none" pointer-events="all" /></a>`
	if !strings.Contains(string(svg), want) {
		t.Fatalf("missing same-note link overlay\nwant: %s\ngot:  %s", want, svg)
	}
}

func TestInjectLinksCrossNote(t *testing.T) {
	a := New(t.TempDir())
	// Target note must exist on disk first.
	if err := a.Write(note("Pb"), svgMap(map[string]string{"Pb": "<svg>b</svg>"})); err != nil {
		t.Fatal(err)
	}
	n := linkedNote("F_SRC", "Pa", "F_TEST", "Pb")
	if err := a.Write(n, svgMap(map[string]string{"Pa": "<svg>a</svg>"})); err != nil {
		t.Fatal(err)
	}
	svg, err := a.ReadSVG("F_SRC", "Pa")
	if err != nil {
		t.Fatal(err)
	}
	if want := `xlink:href="../F_TEST/Pb.svg"`; !strings.Contains(string(svg), want) {
		t.Fatalf("missing cross-note link overlay %q in:\n%s", want, svg)
	}
}

func TestInjectLinksMissingTargetSkipped(t *testing.T) {
	a := New(t.TempDir())
	n := linkedNote("F_SRC", "Pa", "F_GONE", "Pb")
	if err := a.Write(n, svgMap(map[string]string{"Pa": "<svg>a</svg>"})); err != nil {
		t.Fatal(err)
	}
	svg, err := a.ReadSVG("F_SRC", "Pa")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(svg), "<a ") {
		t.Fatalf("dangling cross-note link should be skipped, got:\n%s", svg)
	}
}

func TestInjectLinksSelfClosingUnchanged(t *testing.T) {
	a := New(t.TempDir())
	n := linkedNote("F_TEST", "Pa", "F_TEST", "Pa") // self-target, page in note
	if err := a.Write(n, svgMap(map[string]string{"Pa": "<svg/>"})); err != nil {
		t.Fatal(err)
	}
	svg, err := a.ReadSVG("F_TEST", "Pa")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(svg), "<a ") {
		t.Fatalf("self-closing <svg/> has no </svg> anchor, should be unchanged, got:\n%s", svg)
	}
}

func TestInjectLinksIdempotent(t *testing.T) {
	a := New(t.TempDir())
	n := linkedNote("F_TEST", "Pa", "F_TEST", "Pb")
	n.Pages = append(n.Pages, snote.Page{ID: "Pb", Number: 2})
	svgs := map[string]string{"Pa": "<svg>a</svg>", "Pb": "<svg>b</svg>"}
	if err := a.Write(n, svgMap(svgs)); err != nil {
		t.Fatal(err)
	}
	first, err := a.ReadSVG("F_TEST", "Pa")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Write(n, svgMap(svgs)); err != nil {
		t.Fatal(err)
	}
	second, err := a.ReadSVG("F_TEST", "Pa")
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("re-ingest changed the SVG:\nfirst:  %s\nsecond: %s", first, second)
	}
	if n := strings.Count(string(second), "<a "); n != 1 {
		t.Fatalf("expected exactly one link overlay, got %d:\n%s", n, second)
	}
}
