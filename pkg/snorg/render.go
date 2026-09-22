package snorg

import "github.com/jdlugosz963/snorg/internal/analyze"

// RenderRegion renders rect of a page as PNG bytes at native page resolution —
// the 1920x2560 pixel space title, link and template-box rects are expressed in,
// so a rect read from a PageDoc or the templates: config crops exactly its
// region. Pass Rect{W: 1920, H: 2560} for the whole page.
//
// styled picks the image. The default (false) is the canonical black-on-white
// render: every stroke forced black, nav/link overlays dropped, invariant under
// the ingest.svg styling config — byte-identical to the crop Analyze sends to the
// vision model, which is what makes it reproducible. true rasterizes the
// archive's stored SVG as-is, keeping the pen shades and baked link/nav overlays
// a reader sees. Neither draws the template background image.
func (c *Client) RenderRegion(fileID, pageID string, rect Rect, styled bool) ([]byte, error) {
	svg, err := c.arch.ReadSVG(fileID, pageID)
	if err != nil {
		return nil, err
	}
	return analyze.RenderRegion(svg, rect, !styled)
}
