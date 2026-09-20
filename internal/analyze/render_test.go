package analyze

import (
	"bytes"
	"image/png"
	"testing"

	"github.com/jdlugosz963/snorg/internal/snote"
)

// decodePNG decodes b and fails the test if it is not a PNG.
func decodePNG(t *testing.T, b []byte) (int, int, func(x, y int) (uint32, uint32, uint32)) {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("decode png: %v", err)
	}
	r := img.Bounds()
	return r.Dx(), r.Dy(), func(x, y int) (uint32, uint32, uint32) {
		cr, cg, cb, _ := img.At(r.Min.X+x, r.Min.Y+y).RGBA()
		return cr >> 8, cg >> 8, cb >> 8
	}
}

// pageSVG wraps elements in a root carrying the page's own dimensions, the way a
// real archive SVG does — rasterizing it directly (the styled path) needs them,
// while canonicalSVG supplies its own.
func pageSVG(inner ...string) []byte {
	s := `<svg xmlns="http://www.w3.org/2000/svg" width="1920" height="2560" viewBox="0 0 1920 2560">`
	for _, e := range inner {
		s += e
	}
	return []byte(s + `</svg>`)
}

// TestRenderRegion: the crop is the rect's own pixel size, the canonical render
// forces every stroke black (what the model sees, invariant under recolor) while
// the styled one keeps the SVG's own pen shade.
func TestRenderRegion(t *testing.T) {
	grey := pageSVG(pathEl(filledRect(200, 200, 400, 400), `fill="#9d9d9d"`))
	box := snote.Rect{X: 200, Y: 200, W: 400, H: 400}

	for _, tc := range []struct {
		name      string
		canonical bool
		wantR     uint32
	}{
		{"canonical", true, 0x00},
		{"styled", false, 0x9d},
	} {
		png, err := RenderRegion(grey, box, tc.canonical)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		w, h, at := decodePNG(t, png)
		if w != box.W || h != box.H {
			t.Errorf("%s: got %dx%d, want %dx%d", tc.name, w, h, box.W, box.H)
		}
		if r, _, _ := at(box.W/2, box.H/2); r != tc.wantR {
			t.Errorf("%s: centre pixel red = %#x, want %#x", tc.name, r, tc.wantR)
		}
	}

	// A blank corner of the same page still renders — an image, just white.
	blank, err := RenderRegion(grey, snote.Rect{X: 1000, Y: 2000, W: 100, H: 100}, true)
	if err != nil {
		t.Fatal(err)
	}
	_, _, at := decodePNG(t, blank)
	if r, _, _ := at(50, 50); r != 0xff {
		t.Error("a blank crop must render white")
	}

	// A rect entirely off the page has nothing to render.
	if _, err := RenderRegion(grey, snote.Rect{X: 5000, Y: 5000, W: 10, H: 10}, true); err == nil {
		t.Error("an off-page rect must error")
	}
}
