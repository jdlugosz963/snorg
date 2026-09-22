package query_test

import (
	"reflect"
	"testing"

	"github.com/jdlugosz963/snorg/internal/archive"
	"github.com/jdlugosz963/snorg/internal/query"
	"github.com/jdlugosz963/snorg/internal/snote"
)

// tagPage stamps snorg tags on an already-written page.
func tagPage(t *testing.T, a *archive.Archive, fileID, pageID string, tags ...string) {
	t.Helper()
	pd, err := a.ReadPage(fileID, pageID)
	if err != nil {
		t.Fatal(err)
	}
	pd.Tags = tags
	if _, err := a.WritePage(fileID, pd); err != nil {
		t.Fatal(err)
	}
}

func seedTagged(t *testing.T) *archive.Archive {
	t.Helper()
	a := archive.New(t.TempDir())
	writeNote(t, a, &snote.Note{
		FileID: "F_A",
		Pages: []snote.Page{
			{ID: "Pa", Number: 1, Keywords: []snote.Keyword{{Text: "work"}}},
			{ID: "Pb", Number: 2, Keywords: []snote.Keyword{{Text: "work"}}},
		},
	})
	writeNote(t, a, &snote.Note{
		FileID: "F_B",
		Pages:  []snote.Page{{ID: "Pc", Number: 1, Keywords: []snote.Keyword{{Text: "home"}}}},
	})
	tagPage(t, a, "F_A", "Pa", "important", "todo")
	tagPage(t, a, "F_A", "Pb", "todo")
	// Pc left untagged.
	return a
}

func TestTagsInventory(t *testing.T) {
	a := seedTagged(t)

	got, err := query.Tags(a)
	if err != nil {
		t.Fatal(err)
	}
	// Sorted by value; "important" on one page, "todo" on two.
	want := []query.ValueCount{{Value: "important", Count: 1}, {Value: "todo", Count: 2}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Tags() = %v, want %v", got, want)
	}
}

func TestKeywordsInventory(t *testing.T) {
	a := seedTagged(t)

	got, err := query.Keywords(a)
	if err != nil {
		t.Fatal(err)
	}
	// "home" on Pc, "work" on Pa+Pb; sorted by value.
	want := []query.ValueCount{{Value: "home", Count: 1}, {Value: "work", Count: 2}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Keywords() = %v, want %v", got, want)
	}
}

// The inventory counts effective tags per page: a note tag counts once for each of
// its pages, and only once for a page that carries it itself too.
func TestTagsInventoryCountsInherited(t *testing.T) {
	a := seedTagged(t)
	if _, err := a.TagNote("F_A", "todo", false); err != nil {
		t.Fatal(err)
	}
	if _, err := a.TagNote("F_B", "shared", false); err != nil {
		t.Fatal(err)
	}

	got, err := query.Tags(a)
	if err != nil {
		t.Fatal(err)
	}
	// "todo": F_A's two pages (Pa already carried it — still one each). "shared":
	// F_B's single page. "important": Pa's own.
	want := []query.ValueCount{
		{Value: "important", Count: 1},
		{Value: "shared", Count: 1},
		{Value: "todo", Count: 2},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Tags() = %v, want %v", got, want)
	}
}
