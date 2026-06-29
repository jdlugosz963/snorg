package archive

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
)

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
