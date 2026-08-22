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
				Kind:         snote.LinkNote,
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
	if _, err := a.Write(n, svgMap(map[string]string{"Pa": "<svg>a</svg>", "Pb": "<svg>b</svg>"})); err != nil {
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
	// Cross-note links resolve unconditionally — the target note need not exist yet.
	n := linkedNote("F_SRC", "Pa", "F_TEST", "Pb")
	if _, err := a.Write(n, svgMap(map[string]string{"Pa": "<svg>a</svg>"})); err != nil {
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

func TestInjectLinksMissingTargetStillBaked(t *testing.T) {
	a := New(t.TempDir())
	// A cross-note link to a not-yet-ingested note is baked anyway (dangling href).
	n := linkedNote("F_SRC", "Pa", "F_GONE", "Pb")
	if _, err := a.Write(n, svgMap(map[string]string{"Pa": "<svg>a</svg>"})); err != nil {
		t.Fatal(err)
	}
	svg, err := a.ReadSVG("F_SRC", "Pa")
	if err != nil {
		t.Fatal(err)
	}
	if want := `xlink:href="../F_GONE/Pb.svg"`; !strings.Contains(string(svg), want) {
		t.Fatalf("dangling cross-note link should still be baked %q in:\n%s", want, svg)
	}
}

// noteWithLink is a one-page note whose page holds a single link l.
func noteWithLink(fileID, pageID string, l snote.Link) *snote.Note {
	return &snote.Note{
		FileID: fileID,
		Pages:  []snote.Page{{ID: pageID, Number: 1, Links: []snote.Link{l}}},
	}
}

func TestInjectLinksWebBakedExternal(t *testing.T) {
	a := New(t.TempDir())
	n := noteWithLink("F_SRC", "Pa", snote.Link{
		Rect:   snote.Rect{X: 10, Y: 20, W: 30, H: 40},
		Kind:   snote.LinkWeb,
		Target: "https://example.com/x?a=1&b=2",
		Name:   "https://example.com/x?a=1&b=2",
	})
	if _, err := a.Write(n, svgMap(map[string]string{"Pa": "<svg>a</svg>"})); err != nil {
		t.Fatal(err)
	}
	svg, err := a.ReadSVG("F_SRC", "Pa")
	if err != nil {
		t.Fatal(err)
	}
	// URL opens externally and its & is XML-escaped in the attribute.
	want := `<a xlink:href="https://example.com/x?a=1&amp;b=2" target="_blank">`
	if !strings.Contains(string(svg), want) {
		t.Fatalf("missing external web link overlay\nwant: %s\ngot:  %s", want, svg)
	}
}

func TestInjectLinksFileAndUnknownNotBaked(t *testing.T) {
	for _, kind := range []snote.LinkKind{snote.LinkFile, snote.LinkUnknown} {
		a := New(t.TempDir())
		n := noteWithLink("F_SRC", "Pa", snote.Link{
			Rect:   snote.Rect{X: 10, Y: 20, W: 30, H: 40},
			Kind:   kind,
			Target: "/storage/emulated/0/Document/Book.pdf",
			Name:   "Book.pdf",
		})
		if _, err := a.Write(n, svgMap(map[string]string{"Pa": "<svg>a</svg>"})); err != nil {
			t.Fatal(err)
		}
		svg, err := a.ReadSVG("F_SRC", "Pa")
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(svg), "<a ") {
			t.Fatalf("kind %q is device-local and must not be baked, got:\n%s", kind, svg)
		}
	}
}

func TestInjectLinksSelfClosingUnchanged(t *testing.T) {
	a := New(t.TempDir())
	n := linkedNote("F_TEST", "Pa", "F_TEST", "Pa") // self-target, page in note
	if _, err := a.Write(n, svgMap(map[string]string{"Pa": "<svg/>"})); err != nil {
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
	if _, err := a.Write(n, svgMap(svgs)); err != nil {
		t.Fatal(err)
	}
	first, err := a.ReadSVG("F_TEST", "Pa")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Write(n, svgMap(svgs)); err != nil {
		t.Fatal(err)
	}
	second, err := a.ReadSVG("F_TEST", "Pa")
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("re-ingest changed the SVG:\nfirst:  %s\nsecond: %s", first, second)
	}
	// One link overlay (by its rect) plus one nav zone (next=Pb), nothing doubled.
	if n := strings.Count(string(second), `<rect x="10"`); n != 1 {
		t.Fatalf("expected exactly one link overlay, got %d:\n%s", n, second)
	}
	if n := strings.Count(string(second), "<a "); n != 2 {
		t.Fatalf("expected link + nav anchors (2), got %d:\n%s", n, second)
	}
}
