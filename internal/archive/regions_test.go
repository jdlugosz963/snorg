package archive

import (
	"testing"
)

func TestParseAssembleRoundTrip(t *testing.T) {
	sections := []RegionSection{
		{ID: "title", Label: "Title", Text: "The Heading"},
		{ID: "notes", Label: "Notes", Text: "line one\n\nline two"},
		{ID: "empty", Label: "Empty", Text: ""},
	}
	md := AssembleRegions(sections)
	got := ParseRegions(md)
	if len(got) != len(sections) {
		t.Fatalf("parsed %d sections, want %d:\n%s", len(got), len(sections), md)
	}
	for i, s := range sections {
		if got[i].ID != s.ID || got[i].Text != s.Text {
			t.Errorf("section %d = %+v, want id=%s text=%q", i, got[i], s.ID, s.Text)
		}
	}
	// Assembling the parsed form again is byte-identical (stable merge base).
	if AssembleRegions(got) != md {
		t.Error("assemble∘parse is not idempotent")
	}
}

func TestRegionTextLookup(t *testing.T) {
	sections := []RegionSection{{ID: "a", Text: "alpha"}, {ID: "b", Text: "beta"}}
	if RegionText(sections, "b") != "beta" {
		t.Error("RegionText(b)")
	}
	if RegionText(sections, "missing") != "" {
		t.Error("missing id must be empty")
	}
}

func TestParseRegionsEmpty(t *testing.T) {
	if s := ParseRegions(""); s != nil {
		t.Errorf("empty md must parse to nil, got %+v", s)
	}
	if s := ParseRegions("   \n\n"); s != nil {
		t.Errorf("blank md must parse to nil, got %+v", s)
	}
}

func TestParseRegionsNoLabel(t *testing.T) {
	// A marker without a label still identifies the section by id.
	got := ParseRegions("<!-- region tombstone -->\nrescued text\n")
	if len(got) != 1 || got[0].ID != "tombstone" || got[0].Text != "rescued text" {
		t.Errorf("got %+v", got)
	}
}
