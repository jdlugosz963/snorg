package archive

import (
	"fmt"
	"strings"
)

// Page render size in pixels; every *RECT and the nav split below live in this
// coordinate space.
const (
	navPageW = 1920
	navPageH = 2560
)

// injectNav rewrites a rendered page SVG so tapping the left half jumps to the
// previous page and the right half to the next one, as invisible <a>-wrapped
// <rect> overlays (same technique as injectLinks). An empty neighbor id emits no
// anchor (first/last page). Nav anchors must be emitted BEFORE the note's link
// overlays: SVG hit-testing picks the last element in document order on overlap,
// so the links stay clickable and navigation acts as the background layer.
func injectNav(svg []byte, prevID, nextID string) []byte {
	if prevID == "" && nextID == "" {
		return svg
	}
	s := string(svg)
	end := strings.LastIndex(s, "</svg>")
	if end < 0 { // self-closing <svg/> or malformed: nothing to anchor to
		return svg
	}
	var b strings.Builder
	half := navPageW / 2
	if prevID != "" {
		fmt.Fprintf(&b, "\n<a xlink:href=%q><rect x=\"0\" y=\"0\" width=\"%d\" height=\"%d\" fill=\"none\" pointer-events=\"all\" /></a>",
			prevID+".svg", half, navPageH)
	}
	if nextID != "" {
		fmt.Fprintf(&b, "\n<a xlink:href=%q><rect x=\"%d\" y=\"0\" width=\"%d\" height=\"%d\" fill=\"none\" pointer-events=\"all\" /></a>",
			nextID+".svg", half, navPageW-half, navPageH)
	}
	return []byte(s[:end] + b.String() + s[end:])
}
