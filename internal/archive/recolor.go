package archive

import "strings"

// defaultShadeFills maps each configurable pen-shade name to the exact fill=
// value the renderer (supernotelib's default grayscale palette) emits for it.
// These are the only strings recolor rewrites.
var defaultShadeFills = map[string]string{
	"black":    "#000000",
	"darkgray": "#9d9d9d",
	"gray":     "#c9c9c9",
	"white":    "#fefefe",
}

// recolor remaps the renderer's four default pen shades to configured colors by
// substituting their fill= attribute value verbatim (a CSS name or hex). Only the
// exact default fills are matched, so the link/nav overlays (fill="none") are
// never touched. Substitution is a single non-overlapping pass (strings.Replacer),
// so a shade recolored to another shade's default is not re-substituted and the
// result is independent of map iteration order. An empty/nil map is a no-op. The
// renderer always emits the defaults, so recolor is deterministic and idempotent —
// writeFileIfChanged stays churn-free across re-ingest.
func recolor(svg []byte, colors map[string]string) []byte {
	if len(colors) == 0 {
		return svg
	}
	var pairs []string
	for shade, to := range colors {
		from, ok := defaultShadeFills[shade]
		if !ok || to == "" {
			continue
		}
		pairs = append(pairs, `fill="`+from+`"`, `fill="`+to+`"`)
	}
	if len(pairs) == 0 {
		return svg
	}
	return []byte(strings.NewReplacer(pairs...).Replace(string(svg)))
}
