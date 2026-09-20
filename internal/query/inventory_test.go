package query_test

import (
	"reflect"
	"regexp"
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

func TestTagPredicate(t *testing.T) {
	a := seedTagged(t)

	ms, err := query.Pages(a, query.Tag(regexp.MustCompile("^todo$")))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"Pa", "Pb"}; !reflect.DeepEqual(pageIDs(ms), want) {
		t.Errorf("tag todo = %v, want %v", pageIDs(ms), want)
	}

	ms, err = query.Pages(a, query.Tag(regexp.MustCompile("^important$")))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"Pa"}; !reflect.DeepEqual(pageIDs(ms), want) {
		t.Errorf("tag important = %v, want %v", pageIDs(ms), want)
	}
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
