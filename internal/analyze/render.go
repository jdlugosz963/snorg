package analyze

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"

	"github.com/jdlugosz963/snorg/internal/snote"
	"github.com/srwiley/oksvg"
	"github.com/srwiley/rasterx"
)

// Pages render in a fixed pixel space; all rects are expressed in it (see
// docs/supernote-format.md).
const (
	pageW = 1920
	pageH = 2560
)

// rasterize draws the page SVG onto a white RGBA canvas at the native page
// resolution. The embedded template background (<image href="backgrounds/...">)
// is not resolved by oksvg, so only the handwriting paths are drawn — which is
// exactly what we want to transcribe, on a clean white page.
func rasterize(svg []byte) (*image.RGBA, error) {
	icon, err := oksvg.ReadIconStream(bytes.NewReader(svg), oksvg.IgnoreErrorMode)
	if err != nil {
		return nil, fmt.Errorf("parse svg: %w", err)
	}
	icon.SetTarget(0, 0, float64(pageW), float64(pageH))

	img := image.NewRGBA(image.Rect(0, 0, pageW, pageH))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)

	scanner := rasterx.NewScannerGV(pageW, pageH, img, img.Bounds())
	raster := rasterx.NewDasher(pageW, pageH, scanner)
	icon.Draw(raster, 1.0)
	return img, nil
}

// crop returns the rect region of img as PNG bytes. The rect is clamped to the
// page bounds so stray coordinates can never panic.
func crop(img *image.RGBA, r snote.Rect) ([]byte, error) {
	region := image.Rect(r.X, r.Y, r.X+r.W, r.Y+r.H).Intersect(img.Bounds())
	if region.Empty() {
		return nil, fmt.Errorf("rect %+v is empty within the page", r)
	}
	return toPNG(img.SubImage(region))
}

func toPNG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("encode png: %w", err)
	}
	return buf.Bytes(), nil
}
