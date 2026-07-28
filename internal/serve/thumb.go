package serve

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"

	"github.com/srwiley/oksvg"
	"github.com/srwiley/rasterx"
)

// Pages render in a fixed pixel space (see docs/supernote-format.md); the
// thumbnail keeps that 3:4 aspect but at a fraction of the resolution.
const (
	pageW  = 1920
	pageH  = 2560
	thumbW = 400
	thumbH = thumbW * pageH / pageW
)

// thumbnailPNG rasterizes the page SVG to a small PNG for use as a gallery
// thumbnail: far fewer bytes than the vector SVG (which CSS can only scale, not
// shrink) and instant to decode. Like analyze's rasterize, oksvg does not
// resolve the template background (<image href=…>), so the thumbnail shows just
// the handwriting on white — which is what a note thumbnail wants. The full SVG
// (background, nav, links) is still served for the enlarged lightbox view.
func thumbnailPNG(svg []byte) ([]byte, error) {
	icon, err := oksvg.ReadIconStream(bytes.NewReader(svg), oksvg.IgnoreErrorMode)
	if err != nil {
		return nil, fmt.Errorf("parse svg: %w", err)
	}
	icon.SetTarget(0, 0, float64(thumbW), float64(thumbH))

	img := image.NewRGBA(image.Rect(0, 0, thumbW, thumbH))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)

	scanner := rasterx.NewScannerGV(thumbW, thumbH, img, img.Bounds())
	raster := rasterx.NewDasher(thumbW, thumbH, scanner)
	icon.Draw(raster, 1.0)

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("encode png: %w", err)
	}
	return buf.Bytes(), nil
}
