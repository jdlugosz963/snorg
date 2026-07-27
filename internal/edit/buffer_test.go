package edit

import (
	"testing"

	"github.com/jdlugosz963/snorg/internal/archive"
)

func samplePage() archive.PageDoc {
	return archive.PageDoc{
		Titles: []archive.TitleDoc{
			{Level: 1, Analysis: &archive.TitleAnalysis{Name: "Intro"}},
			{Level: 2}, // never analyzed: empty name
		},
		Links: []archive.LinkDoc{
			{Name: "NoteB", TargetPageID: "P2", Analysis: &archive.LinkAnalysis{Name: "Chapter"}},
		},
	}
}

func eq(t *testing.T, got, want string, label string) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %q, want %q", label, got, want)
	}
}

func TestSerializeParseRoundTrip(t *testing.T) {
	pd := samplePage()
	buf := serialize(pd, "# body\n\ntext\n")

	titleNames, linkNames, content, err := parse(buf, len(pd.Titles), len(pd.Links))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(titleNames) != 2 || titleNames[0] != "Intro" || titleNames[1] != "" {
		t.Errorf("titleNames = %q", titleNames)
	}
	if len(linkNames) != 1 || linkNames[0] != "Chapter" {
		t.Errorf("linkNames = %q", linkNames)
	}
	eq(t, content, "# body\n\ntext\n", "content")
}

func TestSerializeNoRegions(t *testing.T) {
	// No titles and no links: the buffer is just the content, and parse hands it
	// straight back — the pre-feature behavior.
	buf := serialize(archive.PageDoc{}, "just content\n")
	eq(t, buf, "just content\n", "buffer")

	titleNames, linkNames, content, err := parse(buf, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if titleNames != nil || linkNames != nil {
		t.Errorf("names = %q/%q, want nil", titleNames, linkNames)
	}
	eq(t, content, "just content\n", "content")
}

func TestParseIgnoresMarkerContext(t *testing.T) {
	// The context after the index is informational; changing/removing it must not
	// affect parsing.
	buf := "<!-- title 1 (h9) whatever -->\nA\n" +
		"<!-- link 1 -->\nB\n" +
		"<!-- content -->\nbody\n"
	titleNames, linkNames, content, err := parse(buf, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, titleNames[0], "A", "title")
	eq(t, linkNames[0], "B", "link")
	eq(t, content, "body\n", "content")
}

func TestParseContentIsVerbatim(t *testing.T) {
	// Content after the first content marker is taken as-is, even lines that look
	// like markers or a second content marker.
	buf := "<!-- title 1 -->\nT\n<!-- content -->\n<!-- title 2 -->\n<!-- content -->\nreal body\n"
	titleNames, _, content, err := parse(buf, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, titleNames[0], "T", "title")
	eq(t, content, "<!-- title 2 -->\n<!-- content -->\nreal body\n", "content")
}

func TestParseMultilineAndEmptyNames(t *testing.T) {
	buf := "<!-- title 1 -->\nline one\nline two\n<!-- title 2 -->\n<!-- content -->\n"
	titleNames, _, content, err := parse(buf, 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, titleNames[0], "line one\nline two", "multiline title")
	eq(t, titleNames[1], "", "empty title")
	eq(t, content, "", "content")
}

func TestParseErrors(t *testing.T) {
	cases := map[string]struct {
		buf             string
		nTitles, nLinks int
	}{
		"missing content marker": {"<!-- title 1 -->\nA\n", 1, 0},
		"missing title marker":   {"<!-- link 1 -->\nB\n<!-- content -->\n", 1, 1},
		"index out of range":     {"<!-- title 2 -->\nA\n<!-- content -->\n", 1, 0},
		"duplicate marker":       {"<!-- title 1 -->\nA\n<!-- title 1 -->\nB\n<!-- content -->\n", 1, 0},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if _, _, _, err := parse(c.buf, c.nTitles, c.nLinks); err == nil {
				t.Errorf("expected error for %s", name)
			}
		})
	}
}
