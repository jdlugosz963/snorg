package serve

import (
	"regexp"
	"strings"
)

// svgLinkRe matches a baked page-link/nav href in a page SVG: the archive stores
// same-note jumps as "PID.svg" and cross-note jumps as "../FID/PID.svg" (see
// internal/archive/links.go and nav.go). It deliberately only matches ".svg"
// targets, so the extracted background reference (an <image> to "backgrounds/….png")
// is left untouched.
var svgLinkRe = regexp.MustCompile(`xlink:href="(?:\.\./([^"/]+)/)?([^"/]+)\.svg"`)

// rewriteViewerLinks retargets every baked page link so that, inside the viewer's
// enlarged (lightbox) SVG, a tap enlarges the target page. Each
// `xlink:href="…PID.svg"` becomes `target="_top" xlink:href="<href(fid,pid)>"`;
// target="_top" breaks the navigation out of the <object> embedding up to the top
// window, and the destination's `?page=` reader opens the lightbox there. The
// concrete route is supplied by href, which the active layout provides (grouped:
// /note/{fid}?page={pid}; flat: /?page={pid}) — so this function knows nothing of
// the mode. fromFid supplies the note for same-note links (no "../FID/").
func rewriteViewerLinks(svg []byte, fromFid string, href func(fid, pid string) string) []byte {
	return svgLinkRe.ReplaceAllFunc(svg, func(m []byte) []byte {
		sub := svgLinkRe.FindSubmatch(m)
		fid := string(sub[1])
		if fid == "" {
			fid = fromFid
		}
		return []byte(`target="_top" xlink:href="` + href(fid, string(sub[2])) + `"`)
	})
}

var (
	svgRootRe    = regexp.MustCompile(`<svg[^>]*>`)
	svgWidthRe   = regexp.MustCompile(`\swidth="[0-9.]+"`)
	svgHeightRe  = regexp.MustCompile(`\sheight="[0-9.]+"`)
	svgNumWidth  = regexp.MustCompile(`\swidth="([0-9.]+)"`)
	svgNumHeight = regexp.MustCompile(`\sheight="([0-9.]+)"`)
)

// responsiveSVGRoot makes the page SVG scale to its container. supernote-tool
// emits a root <svg> with a fixed pixel width/height (1920x2560); embedded in an
// <object> that renders it as a document (not scaled like an <img>), those fixed
// dimensions make it draw at native size and clip to a corner. Giving the root a
// viewBox (so it has intrinsic coordinates) and width/height="100%" lets it fill —
// and scale within — the object's box. Only the root tag is touched; the fixed
// dimensions become the viewBox when one isn't already present.
func responsiveSVGRoot(svg []byte) []byte {
	loc := svgRootRe.FindIndex(svg)
	if loc == nil {
		return svg
	}
	tag := string(svg[loc[0]:loc[1]])
	if !strings.Contains(tag, "viewBox=") {
		w, h := "1920", "2560"
		if m := svgNumWidth.FindStringSubmatch(tag); m != nil {
			w = m[1]
		}
		if m := svgNumHeight.FindStringSubmatch(tag); m != nil {
			h = m[1]
		}
		tag = strings.Replace(tag, "<svg", `<svg viewBox="0 0 `+w+` `+h+`"`, 1)
	}
	tag = svgWidthRe.ReplaceAllString(tag, ` width="100%"`)
	tag = svgHeightRe.ReplaceAllString(tag, ` height="100%"`)
	if !strings.Contains(tag, `width="100%"`) {
		tag = strings.Replace(tag, "<svg", `<svg width="100%"`, 1)
	}
	if !strings.Contains(tag, `height="100%"`) {
		tag = strings.Replace(tag, "<svg", `<svg height="100%"`, 1)
	}
	out := make([]byte, 0, len(svg)+16)
	out = append(out, svg[:loc[0]]...)
	out = append(out, tag...)
	out = append(out, svg[loc[1]:]...)
	return out
}
