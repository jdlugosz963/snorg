package sntool

import (
	"testing"

	"github.com/jdlugosz963/snorg/internal/snote"
)

func TestParseRectCSV(t *testing.T) {
	r, ok := parseRectCSV("356,98,336,208")
	if !ok || r != (snote.Rect{X: 356, Y: 98, W: 336, H: 208}) {
		t.Fatalf("got %+v ok=%v", r, ok)
	}
	if _, ok := parseRectCSV("1,2,3"); ok {
		t.Fatal("expected failure on 3-field rect")
	}
}

func TestKeySuffixGroups(t *testing.T) {
	g, ok := keySuffixGroups("TITLE_00010098035602080336", prefixTitle)
	if !ok {
		t.Fatal("expected ok")
	}
	want := []int{1, 98, 356, 208, 336}
	if len(g) != len(want) {
		t.Fatalf("len %d != %d", len(g), len(want))
	}
	for i := range want {
		if g[i] != want[i] {
			t.Fatalf("group %d = %d want %d", i, g[i], want[i])
		}
	}
	if _, ok := keySuffixGroups("DIRTY", prefixTitle); ok {
		t.Fatal("non-title key should not match")
	}
}

func TestPageByPoint(t *testing.T) {
	keys := []string{
		"TITLE_00010098035602080336", // 5-group page,y,x,h,w
		"LINKO_00050732048400870235",
		"LINKO_00060748065001311125",
		"DIRTY", "PAGE1", "STYLE_style_5mm_grid_a5x2",
	}
	titles := pageByPoint(keys, prefixTitle)
	if got := titles[point{X: 356, Y: 98}]; got != 1 {
		t.Fatalf("title page = %d want 1", got)
	}
	links := pageByPoint(keys, prefixLink)
	if got := links[point{X: 484, Y: 732}]; got != 5 {
		t.Fatalf("link p5 = %d want 5", got)
	}
	if got := links[point{X: 650, Y: 748}]; got != 6 {
		t.Fatalf("link p6 = %d want 6", got)
	}
}

// Newer firmware emits a 3-group title key (page,y,x with no size); it must still
// associate to the same page as the 5-group form via the (x,y) point.
func TestPageByPoint3Group(t *testing.T) {
	titles := pageByPoint([]string{"TITLE_000100980356"}, prefixTitle)
	if got := titles[point{X: 356, Y: 98}]; got != 1 {
		t.Fatalf("3-group title page = %d want 1", got)
	}
	r, _ := parseRectCSV("356,98,336,208")
	if got := titles[pointOf(r)]; got != 1 {
		t.Fatalf("record point lookup = %d want 1", got)
	}
}

func TestLinkName(t *testing.T) {
	cases := map[string]string{
		// base64 of "/storage/emulated/0/Note/linked-note.note"
		"L3N0b3JhZ2UvZW11bGF0ZWQvMC9Ob3RlL2xpbmtlZC1ub3RlLm5vdGU=": "linked-note",
		// base64 of "/storage/emulated/0/Note/library/external-note.note"
		"L3N0b3JhZ2UvZW11bGF0ZWQvMC9Ob3RlL2xpYnJhcnkvZXh0ZXJuYWwtbm90ZS5ub3Rl": "external-note",
		"":         "",
		"not!b64$": "",
	}
	for in, want := range cases {
		if got := linkName(in); got != want {
			t.Errorf("linkName(%q) = %q want %q", in, got, want)
		}
	}
}

func TestKeywordPages(t *testing.T) {
	keys := []string{"KEYWORD_00040098", "DIRTY", "KEYWORD_00020055"}
	pages := keywordPages(keys)
	// sorted by key suffix: "00020055" before "00040098"
	want := []int{2, 4}
	if len(pages) != len(want) {
		t.Fatalf("len %d != %d", len(pages), len(want))
	}
	for i := range want {
		if pages[i] != want[i] {
			t.Fatalf("page %d = %d want %d", i, pages[i], want[i])
		}
	}
}
