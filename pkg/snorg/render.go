package snorg

import "github.com/jdlugosz963/snorg/internal/analyze"

// RenderRegion renders rect of a page as PNG bytes in the 1920x2560 page pixel
// space; Rect{W: 1920, H: 2560} is the whole page. styled false is the canonical
// black-on-white crop Analyze sends to the model; true rasterizes the stored SVG
// as-is. Neither draws the template background image.
func (c *Client) RenderRegion(fileID, pageID string, rect Rect, styled bool) ([]byte, error) {
	svg, err := c.arch.ReadSVG(fileID, pageID)
	if err != nil {
		return nil, err
	}
	return analyze.RenderRegion(svg, rect, !styled)
}
