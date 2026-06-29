package sntool

import (
	"sort"
	"strconv"
	"strings"

	"github.com/jdlugosz963/snorg/internal/snote"
)

// Footer index keys encode the page (and, for titles/links, the location) of an
// element in their name as 4-digit, 1-based groups. The first three groups are
// always "page,y,x"; some firmware appends the size as "...,h,w" (a 5-group key
// like "TITLE_00010098035602080336"), others omit it (a 3-group key like
// "TITLE_000100980356" — same element, no size). So association keys on the
// top-left (x,y) point, which every variant carries; keywords use "page,y".
// This is the authoritative source of page association — the structured records and
// KEYWORDPAGE are unreliable (see docs/supernote-format.md).

const (
	prefixTitle   = "TITLE_"
	prefixLink    = "LINKO_"
	prefixKeyword = "KEYWORD_"
)

// parseRectCSV parses a supernote rect string "x,y,w,h".
func parseRectCSV(s string) (snote.Rect, bool) {
	parts := strings.Split(s, ",")
	if len(parts) != 4 {
		return snote.Rect{}, false
	}
	n := make([]int, 4)
	for i, p := range parts {
		v, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			return snote.Rect{}, false
		}
		n[i] = v
	}
	return snote.Rect{X: n[0], Y: n[1], W: n[2], H: n[3]}, true
}

// keySuffixGroups splits the 4-digit numeric groups following prefix in key.
func keySuffixGroups(key, prefix string) ([]int, bool) {
	if !strings.HasPrefix(key, prefix) {
		return nil, false
	}
	digits := key[len(prefix):]
	if len(digits) == 0 || len(digits)%4 != 0 {
		return nil, false
	}
	g := make([]int, 0, len(digits)/4)
	for i := 0; i < len(digits); i += 4 {
		v, err := strconv.Atoi(digits[i : i+4])
		if err != nil {
			return nil, false
		}
		g = append(g, v)
	}
	return g, true
}

// point is an element's top-left (x,y) in page pixel space — the stable key shared
// by both the 3-group ("page,y,x") and 5-group ("page,y,x,h,w") footer-key variants.
type point struct{ X, Y int }

// pointOf is the top-left of a rect, used to match a structured record to its key.
func pointOf(r snote.Rect) point { return point{X: r.X, Y: r.Y} }

// pageByPoint maps an element's top-left (x,y) to its 1-based page for footer keys
// with the given prefix (titles, links). Records are then matched to a page by the
// (x,y) of their authoritative rect (TITLERECT / LINKRECT).
func pageByPoint(keys []string, prefix string) map[point]int {
	m := make(map[point]int)
	for _, k := range keys {
		g, ok := keySuffixGroups(k, prefix)
		if !ok || len(g) < 3 {
			continue
		}
		m[point{X: g[2], Y: g[1]}] = g[0]
	}
	return m
}

// keywordPages returns the 1-based page of each keyword in canonical key order
// (sorted by the numeric suffix = page then y), to be paired by index with the
// structured keyword records. Keywords carry no usable rect, so order is the only
// available association.
func keywordPages(keys []string) []int {
	matched := make([]string, 0)
	for _, k := range keys {
		if _, ok := keySuffixGroups(k, prefixKeyword); ok {
			matched = append(matched, k)
		}
	}
	sort.Strings(matched)
	pages := make([]int, 0, len(matched))
	for _, k := range matched {
		g, _ := keySuffixGroups(k, prefixKeyword)
		pages = append(pages, g[0])
	}
	return pages
}
