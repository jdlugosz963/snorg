package archive

import (
	"reflect"
	"testing"

	"github.com/jdlugosz963/snorg/internal/snote"
)

func tagTestNote() *snote.Note {
	return &snote.Note{
		FileID: "F_TAG",
		Source: "n.note",
		Pages:  []snote.Page{{ID: "Pa", Number: 1}},
	}
}

func writeTagNote(t *testing.T, a *Archive) {
	t.Helper()
	if _, err := a.Write(tagTestNote(), map[string][]byte{"Pa": []byte("<svg/>")}); err != nil {
		t.Fatal(err)
	}
}

func TestTagPageAddSortDedup(t *testing.T) {
	a := New(t.TempDir())
	writeTagNote(t, a)

	for _, tag := range []string{"todo", "important", "todo"} {
		if _, err := a.TagPage("Pa", tag, false); err != nil {
			t.Fatal(err)
		}
	}
	// A duplicate add is a no-op.
	changed, err := a.TagPage("Pa", "todo", false)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Error("re-adding an existing tag reported a change")
	}

	pd, err := a.ReadPage("F_TAG", "Pa")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"important", "todo"}; !reflect.DeepEqual(pd.Tags, want) {
		t.Errorf("tags = %v, want sorted/deduped %v", pd.Tags, want)
	}
}

func TestTagPageRemove(t *testing.T) {
	a := New(t.TempDir())
	writeTagNote(t, a)
	for _, tag := range []string{"a", "b"} {
		if _, err := a.TagPage("Pa", tag, false); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := a.TagPage("Pa", "a", true); err != nil {
		t.Fatal(err)
	}
	// Removing an absent tag is a no-op.
	changed, err := a.TagPage("Pa", "a", true)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Error("removing an absent tag reported a change")
	}

	pd, err := a.ReadPage("F_TAG", "Pa")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"b"}; !reflect.DeepEqual(pd.Tags, want) {
		t.Errorf("tags = %v, want %v", pd.Tags, want)
	}

	// Removing the last tag clears the slice (omitempty → no JSON key).
	if _, err := a.TagPage("Pa", "b", true); err != nil {
		t.Fatal(err)
	}
	pd, err = a.ReadPage("F_TAG", "Pa")
	if err != nil {
		t.Fatal(err)
	}
	if len(pd.Tags) != 0 {
		t.Errorf("tags after clearing = %v, want empty", pd.Tags)
	}
}

// Re-ingest must preserve snorg tags — they are not sourced from the .note, so
// Write carries them from the previous page doc like Analysis.
func TestTagsSurviveReingest(t *testing.T) {
	a := New(t.TempDir())
	writeTagNote(t, a)
	if _, err := a.TagPage("Pa", "keep", false); err != nil {
		t.Fatal(err)
	}

	// Re-ingest the same note (fresh domain model, no tags on it).
	writeTagNote(t, a)

	pd, err := a.ReadPage("F_TAG", "Pa")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"keep"}; !reflect.DeepEqual(pd.Tags, want) {
		t.Errorf("tags after re-ingest = %v, want %v (carry-forward)", pd.Tags, want)
	}
}
