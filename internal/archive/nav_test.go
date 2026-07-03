package archive

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jdlugosz963/snorg/internal/snote"
)

func readSVG(t *testing.T, root, pageID string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, "F_TEST", pageID+".svg"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestWriteInjectsNavZones(t *testing.T) {
	root := t.TempDir()
	a := New(root)
	svgs := svgMap(map[string]string{
		"Pa": "<svg>a</svg>", "Pb": "<svg>b</svg>", "Pc": "<svg>c</svg>",
	})
	if err := a.Write(note("Pa", "Pb", "Pc"), svgs); err != nil {
		t.Fatal(err)
	}

	pa := readSVG(t, root, "Pa")
	if strings.Contains(pa, `x="0" y="0" width="960"`) {
		t.Error("first page has a prev zone")
	}
	if !strings.Contains(pa, `xlink:href="Pb.svg"><rect x="960"`) {
		t.Errorf("first page missing next zone:\n%s", pa)
	}

	pb := readSVG(t, root, "Pb")
	if !strings.Contains(pb, `xlink:href="Pa.svg"><rect x="0"`) {
		t.Errorf("middle page missing prev zone:\n%s", pb)
	}
	if !strings.Contains(pb, `xlink:href="Pc.svg"><rect x="960"`) {
		t.Errorf("middle page missing next zone:\n%s", pb)
	}

	pc := readSVG(t, root, "Pc")
	if !strings.Contains(pc, `xlink:href="Pb.svg"><rect x="0"`) {
		t.Errorf("last page missing prev zone:\n%s", pc)
	}
	if strings.Contains(pc, `<rect x="960"`) {
		t.Error("last page has a next zone")
	}
}

func TestWriteSinglePageHasNoNavZones(t *testing.T) {
	root := t.TempDir()
	a := New(root)
	if err := a.Write(note("Pa"), svgMap(map[string]string{"Pa": "<svg>a</svg>"})); err != nil {
		t.Fatal(err)
	}
	if s := readSVG(t, root, "Pa"); strings.Contains(s, "<a ") {
		t.Errorf("single page got nav anchors:\n%s", s)
	}
}

// TestNavZonesSitBehindLinkOverlays: the note's link overlays must be emitted
// after the nav zones — SVG hit-testing picks the later element on overlap, so
// this ordering keeps handwriting links clickable over the half-page zones.
func TestNavZonesSitBehindLinkOverlays(t *testing.T) {
	root := t.TempDir()
	a := New(root)
	n := note("Pa", "Pb")
	n.Pages[0].Links = []snote.Link{{
		Rect:         snote.Rect{X: 100, Y: 100, W: 300, H: 100},
		TargetPageID: "Pb", TargetFileID: "F_TEST",
	}}
	if err := a.Write(n, svgMap(map[string]string{"Pa": "<svg>a</svg>", "Pb": "<svg>b</svg>"})); err != nil {
		t.Fatal(err)
	}
	pa := readSVG(t, root, "Pa")
	nav := strings.Index(pa, `<rect x="960"`)
	link := strings.Index(pa, `<rect x="100"`)
	if nav < 0 || link < 0 {
		t.Fatalf("missing nav or link overlay:\n%s", pa)
	}
	if nav > link {
		t.Errorf("nav zone emitted after link overlay (would steal its clicks):\n%s", pa)
	}
}

func TestInjectNavNoAnchorTarget(t *testing.T) {
	svg := []byte("<svg/>")
	if got := injectNav(svg, "Pa", "Pb"); string(got) != "<svg/>" {
		t.Errorf("self-closing svg was modified: %s", got)
	}
}
