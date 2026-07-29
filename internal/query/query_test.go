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
	// Transcribed content for a couple of pages; Pd stays without an md.
	if err := a.WriteAnalysisMD("F_A", "Pa", "meeting notes about foo"); err != nil {
		t.Fatal(err)
	}
	if err := a.WriteAnalysisMD("F_B", "Pc", "grocery list"); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestPagesContentRegexp(t *testing.T) {
	a := seedArchive(t)

	// Matches the word in Pa's transcription only.
	ms, err := query.Pages(a, query.Content(a, regexp.MustCompile("meeting")))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"Pa"}; !reflect.DeepEqual(pageIDs(ms), want) {
		t.Errorf("content /meeting/ = %v, want %v", pageIDs(ms), want)
	}

	// A pattern present in no transcription matches nothing (and a page with no
	// md, like Pd, never matches).
	ms, err = query.Pages(a, query.Content(a, regexp.MustCompile("nonexistent")))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 0 {
		t.Errorf("expected no matches, got %v", pageIDs(ms))
	}
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

func TestPagesAll(t *testing.T) {
	a := seedArchive(t)
	ms, err := query.Pages(a, query.All)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"Pa", "Pb", "Pc", "Pd"}; !reflect.DeepEqual(pageIDs(ms), want) {
		t.Errorf("all = %v, want %v", pageIDs(ms), want)
	}
}

func TestPagesInNote(t *testing.T) {
	a := seedArchive(t)
	ms, err := query.Pages(a, query.InNote("F_B"))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"Pc", "Pd"}; !reflect.DeepEqual(pageIDs(ms), want) {
		t.Errorf("note F_B = %v, want %v", pageIDs(ms), want)
	}
}

func TestPagesUnanalyzed(t *testing.T) {
	a := seedArchive(t)
	// Analyze Pa: it must drop out of the unanalyzed set.
	pd, err := a.ReadPage("F_A", "Pa")
	if err != nil {
		t.Fatal(err)
	}
	pd.Analysis = &archive.PageAnalysis{SourceHash: "abc"}
	if err := a.WritePage("F_A", pd); err != nil {
		t.Fatal(err)
	}

	ms, err := query.Pages(a, query.Unanalyzed)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"Pb", "Pc", "Pd"}; !reflect.DeepEqual(pageIDs(ms), want) {
		t.Errorf("unanalyzed = %v, want %v", pageIDs(ms), want)
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

// dateArchive: one note with pages dated across three days (PAGEID = "P" +
// YYYYMMDDHHMMSS + tail) plus one id that carries no date.
func dateArchive(t *testing.T) *archive.Archive {
	t.Helper()
	a := archive.New(t.TempDir())
	writeNote(t, a, &snote.Note{
		FileID: "F_D",
		Pages: []snote.Page{
			{ID: "P20260701090000AB", Number: 1},
			{ID: "P20260715120000CD", Number: 2},
			{ID: "P20260722080000EF", Number: 3},
			{ID: "Pnodate", Number: 4},
		},
	})
	return a
}

func TestDate(t *testing.T) {
	a := dateArchive(t)
	cases := []struct {
		name     string
		from, to string
		want     []string
	}{
		{"exact day", "20260715", "20260715", []string{"P20260715120000CD"}},
		{"range", "20260701", "20260715", []string{"P20260701090000AB", "P20260715120000CD"}},
		{"open from", "", "20260715", []string{"P20260701090000AB", "P20260715120000CD"}},
		{"open to", "20260715", "", []string{"P20260715120000CD", "P20260722080000EF"}},
		{"no match", "20250101", "20250101", []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ms, err := query.Pages(a, query.Date(tc.from, tc.to))
			if err != nil {
				t.Fatal(err)
			}
			if got := pageIDs(ms); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Date(%q,%q) = %v, want %v", tc.from, tc.to, got, tc.want)
			}
		})
	}
}

func TestNot(t *testing.T) {
	a := seedArchive(t)

	// Not inverts a filter: the non-starred pages are Pb, Pd.
	ms, err := query.Pages(a, query.Not(query.Starred))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"Pb", "Pd"}; !reflect.DeepEqual(pageIDs(ms), want) {
		t.Errorf("not starred = %v, want %v", pageIDs(ms), want)
	}

	// Under piping (And ∩ InSet) it means "candidates minus matched".
	pred := query.And(query.InSet([]string{"Pa", "Pb", "Pc"}), query.Not(query.Starred))
	ms, err = query.Pages(a, pred)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"Pb"}; !reflect.DeepEqual(pageIDs(ms), want) {
		t.Errorf("InSet∩not starred = %v, want %v", pageIDs(ms), want)
	}
}

func TestInSetAnd(t *testing.T) {
	a := seedArchive(t)
	// InSet restricts to the given ids; And intersects with a filter.
	pred := query.And(query.InSet([]string{"Pa", "Pc", "Pd"}), query.Starred)
	ms, err := query.Pages(a, pred)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"Pa", "Pc"}; !reflect.DeepEqual(pageIDs(ms), want) {
		t.Errorf("InSet∩Starred = %v, want %v", pageIDs(ms), want)
	}

	// Empty set matches nothing.
	ms, err = query.Pages(a, query.InSet(nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 0 {
		t.Errorf("InSet(nil) = %v, want none", pageIDs(ms))
	}
}
