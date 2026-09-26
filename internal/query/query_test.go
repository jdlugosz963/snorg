package query_test

import (
	"reflect"
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
	if _, err := a.Write(n, svgs); err != nil {
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

// The walk visits every page in List order, then note.json page order.
func TestPagesWalksTheWholeArchive(t *testing.T) {
	a := seedArchive(t)
	ms, err := query.Pages(a, func(archive.NoteDoc, archive.PageDoc) (bool, error) { return true, nil })
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"Pa", "Pb", "Pc", "Pd"}; !reflect.DeepEqual(pageIDs(ms), want) {
		t.Errorf("walk = %v, want %v", pageIDs(ms), want)
	}
	if ms[0].FileID != "F_A" || ms[3].FileID != "F_B" {
		t.Errorf("owning notes = %s…%s, want F_A…F_B", ms[0].FileID, ms[3].FileID)
	}
}

// The match function sees the owning note alongside the page, which is what lets a
// predicate reason about note-level state (inherited tags) without a second read.
func TestPagesPassesBothDocuments(t *testing.T) {
	a := seedArchive(t)
	ms, err := query.Pages(a, func(nd archive.NoteDoc, pd archive.PageDoc) (bool, error) {
		return nd.FileID == "F_A" && pd.Starred, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"Pa"}; !reflect.DeepEqual(pageIDs(ms), want) {
		t.Errorf("starred pages of F_A = %v, want %v", pageIDs(ms), want)
	}
}

func TestPagesNoMatches(t *testing.T) {
	a := seedArchive(t)
	ms, err := query.Pages(a, func(archive.NoteDoc, archive.PageDoc) (bool, error) { return false, nil })
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 0 {
		t.Errorf("expected no matches, got %v", pageIDs(ms))
	}
}
