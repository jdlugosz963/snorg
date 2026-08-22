package analyze

import (
	"fmt"
	"testing"

	"github.com/jdlugosz963/snorg/internal/snote"
)

// filledRect is a path d for a closed, filled rectangle — the fingerprint is
// raster-based, so a fixture must enclose area (a zero-area "M..L" hairline
// rasterizes to no ink). Real archive SVGs are always filled potrace contours.
func filledRect(x, y, w, h int) string {
	return fmt.Sprintf("M %d %d L %d %d L %d %d L %d %d Z", x, y, x+w, y, x+w, y+h, x, y+h)
}

func pathEl(d, extra string) string { return `<path ` + extra + ` d="` + d + `"/>` }

// svgDoc wraps path/other elements in a root svg.
func svgDoc(inner ...string) []byte {
	s := `<svg xmlns="http://www.w3.org/2000/svg">`
	for _, e := range inner {
		s += e
	}
	return []byte(s + `</svg>`)
}

// maskOf renders svg canonically and returns its ink mask.
func maskOf(t *testing.T, svg []byte) *mask {
	t.Helper()
	img, err := rasterize(canonicalSVG(svg))
	if err != nil {
		t.Fatal(err)
	}
	return newMask(img)
}

// TestCanonicalMaskInvariance: the fingerprint follows the handwriting geometry
// only, so recolor, a background <image>, a link/nav overlay and d whitespace all
// leave the mask hash unchanged, while an actual shape edit changes it.
func TestCanonicalMaskInvariance(t *testing.T) {
	shape := filledRect(200, 200, 400, 400)
	base := maskOf(t, svgDoc(pathEl(shape, `fill="#000000"`))).hash()

	same := map[string][]byte{
		"recolor":    svgDoc(pathEl(shape, `fill="#9d9d9d" stroke="red"`)),
		"background": svgDoc(`<image xlink:href="data:image/png;base64,AAAA"/>`, pathEl(shape, `fill="#000000"`)),
		"overlay":    svgDoc(pathEl(shape, `fill="#000000"`), `<a xlink:href="P2.svg"><rect x="0" y="0" width="10" height="10" fill="none"/></a>`),
		"whitespace": svgDoc(pathEl("M 200   200\n L 600 200 L 600 600 L 200 600 Z", `fill="#000000"`)),
	}
	for name, s := range same {
		if got := maskOf(t, s).hash(); got != base {
			t.Errorf("%s changed the page hash", name)
		}
	}

	edited := svgDoc(pathEl(filledRect(200, 200, 500, 400), `fill="#000000"`)) // wider
	if maskOf(t, edited).hash() == base {
		t.Error("an edited shape did not change the page hash")
	}
}

// TestRegionMaskCrop: a box fingerprints only the ink inside its pixel rect, so
// boxes over different bands differ, an empty box is stable, and a full-page box
// differs from a single band.
func TestRegionMaskCrop(t *testing.T) {
	top := filledRect(200, 200, 400, 400)  // y 200..600  → top band
	bot := filledRect(200, 1800, 400, 400) // y 1800..2200 → bottom band
	m := maskOf(t, svgDoc(pathEl(top+" "+bot, `fill="#000000"`)))

	boxTop := snote.Rect{X: 0, Y: 0, W: 1920, H: 1000}
	boxBot := snote.Rect{X: 0, Y: 1600, W: 1920, H: 960}
	boxEmpty := snote.Rect{X: 0, Y: 1000, W: 1920, H: 400} // blank middle band
	whole := snote.Rect{X: 0, Y: 0, W: 1920, H: 2560}

	if m.regionHash(boxTop) == m.regionHash(boxBot) {
		t.Error("boxes over different bands must fingerprint differently")
	}
	// An empty region is stable: a second same-sized blank box hashes the same, and
	// it differs from a box that holds ink.
	if m.regionHash(boxEmpty) != m.regionHash(snote.Rect{X: 0, Y: 1000, W: 1920, H: 400}) {
		t.Error("an empty region hash must be stable")
	}
	if m.regionHash(boxEmpty) == m.regionHash(boxTop) {
		t.Error("an empty region must not match an inked one")
	}
	if m.regionHash(whole) == m.regionHash(boxTop) {
		t.Error("a full-page box holds more ink than one band")
	}
}

// TestRegionCropSplitsAtBoundary is the marquee fix: a single filled shape that
// straddles the border between two adjacent boxes contributes only its in-box
// pixels to each. So editing ink that lives solely in box A leaves box B's
// fingerprint unchanged even though a rendered contour crosses the shared edge —
// the case the old whole-contour subpath rule got wrong (it counted the straddler
// whole in both boxes).
func TestRegionCropSplitsAtBoundary(t *testing.T) {
	boxA := snote.Rect{X: 0, Y: 0, W: 1920, H: 1280}    // top half
	boxB := snote.Rect{X: 0, Y: 1280, W: 1920, H: 1280} // bottom half

	straddler := filledRect(100, 1200, 200, 160) // y 1200..1360, crosses y=1280
	v1 := maskOf(t, svgDoc(pathEl(straddler+" "+filledRect(500, 200, 200, 200), `fill="#000000"`)))
	// Move only the A-exclusive shape, within A, not touching B; straddler untouched.
	v2 := maskOf(t, svgDoc(pathEl(straddler+" "+filledRect(900, 200, 200, 200), `fill="#000000"`)))

	if v1.regionHash(boxB) != v2.regionHash(boxB) {
		t.Error("editing ink only in box A perturbed box B's fingerprint")
	}
	if v1.regionHash(boxA) == v2.regionHash(boxA) {
		t.Error("moving the A-exclusive shape should change box A's fingerprint")
	}
}
