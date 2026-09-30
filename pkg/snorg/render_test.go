package snorg

import (
	"bytes"
	"image/png"
	"testing"
)

// TestRenderRegion: the library hands back a real PNG of exactly the requested
// rect, in either form, and refuses a rect that is not on the page.
func TestRenderRegion(t *testing.T) {
	c := seedArchive(t)
	box := Rect{X: 100, Y: 100, W: 400, H: 400} // the fixture's filled contour

	for _, styled := range []bool{false, true} {
		b, err := c.RenderRegion("P1", box, styled)
		if err != nil {
			t.Fatalf("styled=%v: %v", styled, err)
		}
		img, err := png.Decode(bytes.NewReader(b))
		if err != nil {
			t.Fatalf("styled=%v: decode: %v", styled, err)
		}
		if got := img.Bounds(); got.Dx() != box.W || got.Dy() != box.H {
			t.Errorf("styled=%v: bounds %v, want %dx%d", styled, got, box.W, box.H)
		}
		// The contour fills the box, so its centre is ink in both forms.
		r, _, _, _ := img.At(img.Bounds().Min.X+box.W/2, img.Bounds().Min.Y+box.H/2).RGBA()
		if r>>8 != 0x00 {
			t.Errorf("styled=%v: centre red = %#x, want black ink", styled, r>>8)
		}
	}

	if _, err := c.RenderRegion("P1", Rect{X: 5000, Y: 5000, W: 10, H: 10}, false); err == nil {
		t.Error("an off-page rect must error")
	}
	if _, err := c.RenderRegion("nope", box, false); err == nil {
		t.Error("an unknown PAGEID must error")
	}
}
