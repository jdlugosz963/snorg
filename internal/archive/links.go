package archive

import (
	"fmt"
	"strings"

	"github.com/jdlugosz963/snorg/internal/snote"
)

// injectLinks rewrites a rendered page SVG so each resolvable tap-target becomes a
// real clickable hyperlink: an invisible <a>-wrapped <rect> at the link's pixel
// rect (links are already in the SVG's 1920x2560 space, so no transform). The
// overlay is transparent (fill="none", pointer-events="all"), leaving the note's
// appearance untouched while making the region navigable in a browser/viewer.
//
// fromFileID is the note owning this page; inNote is that note's page-id set (its
// SVGs are all written in the same Write call). Links that no handler can resolve
// yet — e.g. a target note not ingested, or a kind we don't handle — are skipped.
// Each overlay sits on its own line and the output is deterministic, so
// writeFileIfChanged stays churn-free across re-ingest.
func (a *Archive) injectLinks(svg []byte, fromFileID string, inNote map[string]bool, links []snote.Link) []byte {
	if len(links) == 0 {
		return svg
	}
	s := string(svg)
	end := strings.LastIndex(s, "</svg>")
	if end < 0 { // self-closing <svg/> or malformed: nothing to anchor to
		return svg
	}
	var b strings.Builder
	for _, l := range links {
		href, ok := a.linkHref(fromFileID, inNote, l)
		if !ok {
			continue
		}
		fmt.Fprintf(&b, "\n<a xlink:href=%q><rect x=\"%d\" y=\"%d\" width=\"%d\" height=\"%d\" fill=\"none\" pointer-events=\"all\" /></a>",
			href, l.Rect.X, l.Rect.Y, l.Rect.W, l.Rect.H)
	}
	if b.Len() == 0 {
		return svg
	}
	return []byte(s[:end] + b.String() + s[end:])
}

// linkHref resolves a tap-target to an href usable from the page SVG at
// <fromFileID>/<page>.svg, or returns ok=false when nothing resolves it yet.
// Resolution is a small ordered pipeline so new target kinds (e.g. web links,
// external resources) can be added without touching callers; today only links to
// another note page resolve.
func (a *Archive) linkHref(fromFileID string, inNote map[string]bool, l snote.Link) (string, bool) {
	if href, ok := a.noteSVGHref(fromFileID, inNote, l); ok {
		return href, true
	}
	// future: web links, external resources, ...
	return "", false
}

// noteSVGHref resolves a link to another page's SVG, as a path relative to the
// page SVG at <fromFileID>/. A same-note jump resolves iff the target page is part
// of this note (its SVG is written in the same Write). A cross-note jump always
// resolves to the deterministic archive-relative path — the target note need not be
// ingested yet; the href simply dangles until it is (no ingest-order dependency).
func (a *Archive) noteSVGHref(fromFileID string, inNote map[string]bool, l snote.Link) (string, bool) {
	if l.TargetFileID == "" || l.TargetPageID == "" {
		return "", false
	}
	if l.TargetFileID == fromFileID {
		if !inNote[l.TargetPageID] {
			return "", false
		}
		return l.TargetPageID + ".svg", true
	}
	return "../" + l.TargetFileID + "/" + l.TargetPageID + ".svg", true
}
