package analyze

import (
	"crypto/sha256"
	"encoding/hex"
	"hash"
	"image"
	"strconv"
	"strings"

	"github.com/jdlugosz963/snorg/internal/snote"
)

// Fingerprinting is raster-based and unified: the page is rendered to a single
// canonical black-on-white image, thresholded to a 1-bit ink mask, and hashed.
// The whole-page hash fingerprints the page; a pixel crop of the mask fingerprints
// a template box. Cropping in pixel space splits ink at the box boundary exactly —
// a stroke crossing an edge contributes only its in-box pixels, so editing one box
// never perturbs a neighbour (the flaw of the old whole-contour subpath rule).
//
// "Canonical" means the mask depends only on the handwriting geometry: every
// <path> is forced black and everything else (the template <image> background, the
// nav/link overlays) is dropped, so the fingerprint is invariant under recolor,
// background mode, link/nav injection and formatSVG whitespace — a restyle never
// re-transcribes. The same canonical image is reused for the LLM crops, so a Page
// call rasterizes exactly once.

// canonicalSVG builds a paths-only, black-on-white SVG from svg's handwriting: the
// d of every <path> in document order, each forced fill="#000000". It reads the
// same path data the fingerprint depends on, so recolor/background/nav/link/format
// leave the canonical bytes (and thus the mask) unchanged.
func canonicalSVG(svg []byte) []byte {
	var b strings.Builder
	b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" width="`)
	b.WriteString(strconv.Itoa(pageW))
	b.WriteString(`" height="`)
	b.WriteString(strconv.Itoa(pageH))
	b.WriteString(`" viewBox="0 0 `)
	b.WriteString(strconv.Itoa(pageW))
	b.WriteByte(' ')
	b.WriteString(strconv.Itoa(pageH))
	b.WriteString(`">`)
	forEachPathData(svg, func(d string) {
		b.WriteString(`<path fill="#000000" d="`)
		b.WriteString(d)
		b.WriteString(`"/>`)
	})
	b.WriteString(`</svg>`)
	return []byte(b.String())
}

// mask is a 1-bit ink map of the canonical page render: bit (y*w+x) set iff that
// pixel is ink. Packed into bytes, so a full page is ~w*h/8 bytes.
type mask struct {
	w, h int
	ink  []byte
}

// newMask thresholds the canonical render to ink (any pixel darker than mid-grey).
// The canonical image is pure black on white, so the red channel alone decides;
// the mid-grey cutoff keeps anti-aliased stroke edges stable.
func newMask(img *image.RGBA) *mask {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	m := &mask{w: w, h: h, ink: make([]byte, (w*h+7)/8)}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			off := img.PixOffset(img.Rect.Min.X+x, img.Rect.Min.Y+y)
			if img.Pix[off] < 128 { // R < 128 ⇒ ink
				i := y*w + x
				m.ink[i/8] |= 1 << uint(i%8)
			}
		}
	}
	return m
}

// at reports whether pixel (x,y) is ink; out-of-bounds is not ink.
func (m *mask) at(x, y int) bool {
	if x < 0 || y < 0 || x >= m.w || y >= m.h {
		return false
	}
	i := y*m.w + x
	return m.ink[i/8]&(1<<uint(i%8)) != 0
}

// hash fingerprints the whole page: the mask dimensions followed by its ink bits.
func (m *mask) hash() string {
	h := sha256.New()
	writeDims(h, m.w, m.h)
	h.Write(m.ink)
	return hex.EncodeToString(h.Sum(nil))
}

// regionHash fingerprints one template box: the box rect clamped to the page, then
// its ink bits row by row (repacked from origin so the hash is position-relative).
// An empty (all-white) region hashes to a stable value — its dims plus zero bits.
func (m *mask) regionHash(box snote.Rect) string {
	x0, y0 := clamp(box.X, m.w), clamp(box.Y, m.h)
	x1, y1 := clamp(box.X+box.W, m.w), clamp(box.Y+box.H, m.h)
	rw, rh := x1-x0, y1-y0
	h := sha256.New()
	writeDims(h, rw, rh)
	if rw <= 0 || rh <= 0 {
		return hex.EncodeToString(h.Sum(nil))
	}
	row := make([]byte, (rw+7)/8)
	for y := y0; y < y1; y++ {
		for i := range row {
			row[i] = 0
		}
		for x := x0; x < x1; x++ {
			if m.at(x, y) {
				i := x - x0
				row[i/8] |= 1 << uint(i%8)
			}
		}
		h.Write(row)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// writeDims hashes two ints as a fixed-width prefix so differently sized regions
// with the same packed bits can never collide.
func writeDims(h hash.Hash, a, b int) {
	var buf [16]byte
	putInt(buf[0:8], a)
	putInt(buf[8:16], b)
	h.Write(buf[:])
}

func putInt(b []byte, v int) {
	u := uint64(v)
	for i := 0; i < 8; i++ {
		b[i] = byte(u >> (8 * uint(i)))
	}
}

// clamp bounds v to [0, max].
func clamp(v, max int) int {
	if v < 0 {
		return 0
	}
	if v > max {
		return max
	}
	return v
}

// blank reports whether the page holds no ink at all — nothing was drawn, so
// there is nothing to transcribe. Bits past w*h are never set, so scanning the
// packed bytes is exact.
func (m *mask) blank() bool {
	for _, b := range m.ink {
		if b != 0 {
			return false
		}
	}
	return true
}

// blankRegion reports whether box holds no ink, clamping it to the page exactly
// like regionHash — so blankness and the fingerprint agree on where a box begins
// and ends, and a stroke crossing the edge counts only its in-box pixels.
func (m *mask) blankRegion(box snote.Rect) bool {
	x0, y0 := clamp(box.X, m.w), clamp(box.Y, m.h)
	x1, y1 := clamp(box.X+box.W, m.w), clamp(box.Y+box.H, m.h)
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			if m.at(x, y) {
				return false
			}
		}
	}
	return true
}
