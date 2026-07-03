package archive

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
)

// BackgroundMode selects how Write treats a rendered page's template background:
//
//	extract  lift the inline base64 image into backgrounds/ and reference it (default; visible)
//	inline   leave the inline base64 image in place (visible)
//	blank    replace the image with a white rectangle
//	remove   delete the image (transparent)
//
// None of these changes the analyze fingerprint, which is derived from path
// geometry only.
type BackgroundMode string

const (
	BackgroundExtract BackgroundMode = "extract"
	BackgroundInline  BackgroundMode = "inline"
	BackgroundBlank   BackgroundMode = "blank"
	BackgroundRemove  BackgroundMode = "remove"
)

// applyBackground handles the non-extract background modes (extract is done inline
// in Write via extractBackground). For blank/remove it replaces or deletes the
// inline <image> element; inline (or any other value) leaves the SVG untouched. A
// page with no inline background image is returned unchanged.
func applyBackground(svg []byte, mode BackgroundMode) []byte {
	if mode != BackgroundBlank && mode != BackgroundRemove {
		return svg
	}
	s := string(svg)
	start, end, ok := backgroundImageSpan(s)
	if !ok {
		return svg
	}
	var repl string
	if mode == BackgroundBlank {
		repl = fmt.Sprintf(`<rect x="0" y="0" width="%d" height="%d" fill="#ffffff" />`, navPageW, navPageH)
	}
	return []byte(s[:start] + repl + s[end:])
}

// backgroundImageSpan locates the inline data-URI background <image> element in s,
// returning the byte span [start,end) of the whole element (including its closing
// '>'). The base64 payload contains no '>', so the first '>' after the data URI is
// the element's end. ok is false when there is no inline background image.
func backgroundImageSpan(s string) (start, end int, ok bool) {
	h := strings.Index(s, "data:image/")
	if h < 0 {
		return 0, 0, false
	}
	start = strings.LastIndex(s[:h], "<image")
	if start < 0 {
		return 0, 0, false
	}
	rel := strings.IndexByte(s[h:], '>')
	if rel < 0 {
		return 0, 0, false
	}
	return start, h + rel + 1, true
}

// backgroundsDir is the per-note subfolder holding content-addressed page
// backgrounds (<root>/<FILE_ID>/backgrounds/<sha256>.<ext>). supernote-tool embeds
// the same template background inline in every page SVG; extracting it here
// deduplicates that heavy raster (all of a note's pages share one file) and keeps
// the SVG text small.
//
// It lives next to the page SVGs, not at the archive root, so the SVG can reference
// it by a *descendant* path ("backgrounds/<name>"). librsvg-based viewers (Emacs,
// imv, rsvg) refuse to load resources from a parent directory ("../"), so a shared
// root-level folder would render only in browsers — the per-note copy renders
// everywhere at the cost of duplicating the template across notes.
const backgroundsDir = "backgrounds"

// extractBackground finds the inline data-URI background <image> in a rendered
// SVG and replaces its xlink:href with a relative path to a content-addressed
// file in backgroundsDir. It returns the rewritten SVG plus the decoded image
// bytes and its hash-based filename. ok is false when the SVG has no inline data
// image (e.g. a blank page), in which case svg is returned unchanged.
//
// The href is rewritten to "<backgroundsDir>/<name>", a descendant path next to
// the page SVG (see backgroundsDir on why it must not escape upward). The output
// is a pure function of the input bytes (identical PNG -> identical name ->
// identical href), so re-ingest re-renders byte-identically and writeFileIfChanged
// stays churn-free.
func extractBackground(svg []byte) (out []byte, img []byte, name string, ok bool) {
	s := string(svg)
	const marker = `xlink:href="data:image/`
	hi := strings.Index(s, marker)
	if hi < 0 {
		return svg, nil, "", false
	}
	valStart := hi + len(`xlink:href="`) // points at "data:image/..."
	q := strings.IndexByte(s[valStart:], '"')
	if q < 0 {
		return svg, nil, "", false
	}
	uri := s[valStart : valStart+q] // data:image/<ext>;base64,<DATA>

	semi := strings.IndexByte(uri, ';')
	comma := strings.IndexByte(uri, ',')
	if semi < 0 || comma < 0 || comma < semi {
		return svg, nil, "", false
	}
	ext := uri[len("data:image/"):semi]
	if ext == "" {
		ext = "png"
	}
	data, err := base64.StdEncoding.DecodeString(uri[comma+1:])
	if err != nil {
		return svg, nil, "", false
	}

	sum := sha256.Sum256(data)
	name = hex.EncodeToString(sum[:]) + "." + ext
	href := backgroundsDir + "/" + name
	out = []byte(s[:valStart] + href + s[valStart+q:])
	return out, data, name, true
}
