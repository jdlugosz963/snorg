package query_test

import (
	"reflect"
	"regexp"
	"testing"

	"github.com/jdlugosz963/snorg/internal/archive"
	"github.com/jdlugosz963/snorg/internal/query"
	"github.com/jdlugosz963/snorg/internal/snote"
)

func writeNote(t *testing.T, a *archive.Archive, n *snote.Note) {
	t.Helper()
	svgs := make(map[string][]byte, len(n.Pages))
	for _, p := range n.Pages {
		svgs[p.ID] = []byte("<svg/>")
	}
	if err := a.Write(n, svgs); err != nil {
		t.Fatal(err)
	}
}

// pageIDs collects the matched PAGEIDs in result order.
func pageIDs(ms []query.Match) []string {
	ids := make([]string, len(ms))
	for i, m := range ms {
		ids[i] = m.PageID
	}
	return ids
}

func seedArchive(t *testing.T) *archive.Archive {
	t.Helper()
	a := archive.New(t.TempDir())
	// F_A: Pa starred + keyword "foo", Pb keyword "foobar".
	writeNote(t, a, &snote.Note{
		FileID: "F_A",
		Pages: []snote.Page{
			{ID: "Pa", Number: 1, Starred: true, Keywords: []snote.Keyword{{Text: "foo"}}},
			{ID: "Pb", Number: 2, Keywords: []snote.Keyword{{Text: "foobar"}}},
		},
	})
	// F_B: Pc starred, no keywords; Pd plain.
	writeNote(t, a, &snote.Note{
		FileID: "F_B",
		Pages: []snote.Page{
			{ID: "Pc", Number: 1, Starred: true},
			{ID: "Pd", Number: 2},
		},
	})
	return a
}

func TestPagesStarred(t *testing.T) {
	a := seedArchive(t)
	ms, err := query.Pages(a, query.Starred)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"Pa", "Pc"}; !reflect.DeepEqual(pageIDs(ms), want) {
		t.Errorf("starred = %v, want %v", pageIDs(ms), want)
	}
}

func TestPagesKeywordRegexp(t *testing.T) {
	a := seedArchive(t)

	// Substring-style: both "foo" and "foobar" contain "foo".
	ms, err := query.Pages(a, query.Keyword(regexp.MustCompile("foo")))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"Pa", "Pb"}; !reflect.DeepEqual(pageIDs(ms), want) {
		t.Errorf("keyword /foo/ = %v, want %v", pageIDs(ms), want)
	}

	// Anchored: only the exact "foo" matches.
	ms, err = query.Pages(a, query.Keyword(regexp.MustCompile("^foo$")))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"Pa"}; !reflect.DeepEqual(pageIDs(ms), want) {
		t.Errorf("keyword /^foo$/ = %v, want %v", pageIDs(ms), want)
	}
}

func TestPagesNoMatch(t *testing.T) {
	a := seedArchive(t)
	ms, err := query.Pages(a, query.Keyword(regexp.MustCompile("nope")))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 0 {
		t.Errorf("expected no matches, got %v", pageIDs(ms))
	}
}
