package main

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/jdlugosz963/snorg/pkg/snorg"
)

func text(was, now string) snorg.TextChange { return snorg.TextChangeOf(was, now) }

func TestDescribeText(t *testing.T) {
	cases := []struct {
		name string
		c    snorg.TextChange
		want string
	}{
		{"zero is silent", snorg.TextChange{}, ""},
		{"unchanged has no counts", text("a\n", "a\n"), "unchanged"},
		{"new", text("", "one\ntwo\n"), "new +2/-0"},
		{"cleared", text("one\ntwo\n", ""), "cleared +0/-2"},
		{"updated", text("a\n", "b\n"), "updated +1/-1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := describeText(c.c); got != c.want {
				t.Errorf("describeText = %q, want %q", got, c.want)
			}
		})
	}
}

func TestDescribeTextConflict(t *testing.T) {
	c := text("a\n", "<<<<<<< edited\na\n=======\nb\n>>>>>>> reanalyzed\n")
	if got := describeText(c); !strings.HasPrefix(got, "updated conflict ") {
		t.Errorf("describeText = %q, want an updated conflict", got)
	}
}

// The cost axis and the text axis are counted separately, so a skipped box is never
// reported as if it had been transcribed.
func TestDescribeRegions(t *testing.T) {
	rs := []snorg.RegionResult{
		{RegionChange: snorg.RegionChange{ID: "a", Text: text("", "x\n")}},
		{RegionChange: snorg.RegionChange{ID: "b", Text: text("x\n", "x\n")}, Skipped: true},
		{RegionChange: snorg.RegionChange{ID: "c"}, Blank: true},
		{RegionChange: snorg.RegionChange{ID: "d"}, Blank: true},
	}
	if got, want := describeRegions(rs), "regions 1 new, 1 skipped, 2 blank"; got != want {
		t.Errorf("describeRegions = %q, want %q", got, want)
	}
	if got := describeRegions(nil); got != "" {
		t.Errorf("describeRegions(nil) = %q, want empty", got)
	}
}

func TestDescribeAnalyze(t *testing.T) {
	content := text("", "one\n")
	cases := []struct {
		name string
		r    snorg.AnalyzeResult
		want string
	}{
		{"skipped says only that",
			snorg.AnalyzeResult{PageResult: snorg.PageResult{Skipped: true, Calls: 0}},
			"skipped"},
		{"clauses omitted at zero",
			snorg.AnalyzeResult{PageResult: snorg.PageResult{Content: &content}},
			"new +1/-0"},
		{"full page",
			snorg.AnalyzeResult{PageResult: snorg.PageResult{
				Content: &content,
				Names:   []snorg.NameChange{{Kind: "title", Index: 1}},
				Fields:  []string{"summary"},
				Calls:   4,
			}},
			"new +1/-0, 1 name, 1 field, 4 calls"},
		{"templated page reports its boxes, not content",
			snorg.AnalyzeResult{PageResult: snorg.PageResult{
				Regions: []snorg.RegionResult{{RegionChange: snorg.RegionChange{ID: "a", Text: text("", "x\n")}}},
				Calls:   1,
			}},
			"regions 1 new, 1 call"},
		{"an untouched page reads as unchanged",
			snorg.AnalyzeResult{}, "unchanged"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := describeAnalyze(c.r); got != c.want {
				t.Errorf("describeAnalyze = %q, want %q", got, c.want)
			}
		})
	}
}

func TestAnalyzeDetail(t *testing.T) {
	content := text("old\n", "new\n")
	r := snorg.AnalyzeResult{PageResult: snorg.PageResult{
		Content: &content,
		Names:   []snorg.NameChange{{Kind: "title", Index: 1, Was: "Esay", Now: "Essay"}},
		Fields:  []string{"summary"},
	}}
	got := analyzeDetail(r)
	if len(got) == 0 || got[0] != "content: updated +1/-1" {
		t.Fatalf("detail[0] = %q, want the content clause", got)
	}
	joined := strings.Join(got, "\n")
	if !strings.Contains(joined, `title 1: "Esay" -> "Essay"`) {
		t.Errorf("detail missing the rename: %q", joined)
	}
	if !strings.Contains(joined, "field: summary") {
		t.Errorf("detail missing the field: %q", joined)
	}
	// A skipped page has nothing to account for.
	if d := analyzeDetail(snorg.AnalyzeResult{PageResult: snorg.PageResult{Skipped: true}}); d != nil {
		t.Errorf("skipped detail = %v, want nil", d)
	}
}

// A box that cost nothing is named by that fact, not by a text verdict it never
// earned.
func TestAnalyzeDetailRegionState(t *testing.T) {
	r := snorg.AnalyzeResult{PageResult: snorg.PageResult{Regions: []snorg.RegionResult{
		{RegionChange: snorg.RegionChange{ID: "grade", Label: "Grade", Text: text("B\n", "B+\n")}},
		{RegionChange: snorg.RegionChange{ID: "notes", Label: "Notes"}, Skipped: true},
		{RegionChange: snorg.RegionChange{ID: "date"}, Blank: true},
	}}}
	got := strings.Join(analyzeDetail(r), "\n")
	for _, want := range []string{
		"region grade (Grade): updated +1/-1",
		"region notes (Notes): skipped",
		"region date: blank",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("detail missing %q:\n%s", want, got)
		}
	}
}

func TestDescribeEdit(t *testing.T) {
	e := snorg.PageEdit{
		Content: text("a\n", "b\n"),
		Names:   []snorg.NameChange{{Kind: "title", Index: 1, Now: "T", Override: true}},
	}
	if got, want := describeEdit(e), "updated +1/-1, 1 name"; got != want {
		t.Errorf("describeEdit = %q, want %q", got, want)
	}
	// Only boxes that moved are counted.
	e.Regions = []snorg.RegionChange{
		{ID: "a", Text: text("x\n", "y\n")},
		{ID: "b", Text: text("z\n", "z\n")},
	}
	if got, want := describeEdit(e), "updated +1/-1, 1 region(s), 1 name"; got != want {
		t.Errorf("describeEdit = %q, want %q", got, want)
	}
}

func TestDescribeIngestReport(t *testing.T) {
	if got, want := describeReport(nil), "current"; got != want {
		t.Errorf("describeReport(nil) = %q, want %q", got, want)
	}
	// An all-false report means the note was already current.
	if got, want := describeReport(&snorg.WriteReport{}), "current"; got != want {
		t.Errorf("empty report = %q, want %q", got, want)
	}
	rep := &snorg.WriteReport{
		NoteChanged: true,
		Pages:       []snorg.PageWriteReport{{PageID: "P1", New: true}, {PageID: "P2"}},
		Pruned:      []string{"P9"},
	}
	if got, want := describeReport(rep), "2 page(s) changed, note.json changed, 1 pruned"; got != want {
		t.Errorf("describeReport = %q, want %q", got, want)
	}
}

func TestIngestDetail(t *testing.T) {
	r := snorg.IngestResult{Report: &snorg.WriteReport{
		Pages: []snorg.PageWriteReport{
			{PageID: "P1", New: true, JSONChanged: true, SVGChanged: true, BackgroundChanged: true},
			{PageID: "P2", SVGChanged: true, DroppedRegions: []snorg.DroppedRegion{{Kind: "title"}, {Kind: "link"}}},
		},
		Pruned: []string{"P9"},
	}}
	want := []string{
		"P1: new, json, svg, background",
		"P2: svg, dropped 2 region analyses (title, link)",
		"pruned P9",
	}
	if got := ingestDetail(r); !reflect.DeepEqual(got, want) {
		t.Errorf("ingestDetail =\n%v\nwant\n%v", got, want)
	}
}

func TestDiffLinesTruncates(t *testing.T) {
	var was, now strings.Builder
	for i := 0; i < 50; i++ {
		was.WriteString("old line\n")
		now.WriteString("new line\n")
	}
	got := diffLines(text(was.String(), now.String()))
	if len(got) != maxDiffLines+1 {
		t.Fatalf("diffLines = %d lines, want %d plus a tail", len(got), maxDiffLines)
	}
	if !strings.HasPrefix(got[len(got)-1], "… ") {
		t.Errorf("last line = %q, want the truncation tail", got[len(got)-1])
	}
	// An unchanged text has no diff to show.
	if d := diffLines(text("a\n", "a\n")); d != nil {
		t.Errorf("diffLines on unchanged = %v, want nil", d)
	}
}

func TestDescribeReportAdoption(t *testing.T) {
	rep := &snorg.WriteReport{
		Pages: []snorg.PageWriteReport{
			{PageID: "P1", AdoptedFrom: []string{"F_A"}},
			{PageID: "P2", JSONChanged: true},
		},
		Pruned:   []string{"P9"},
		Parked:   []string{"P9"},
		Repaired: []string{"F_A"},
	}
	want := "2 page(s) changed, 1 pruned, 1 adopted, 1 note(s) repaired"
	if got := describeReport(rep); got != want {
		t.Errorf("describeReport = %q, want %q", got, want)
	}
}

// TestIngestDetailAdoption: a page that moved in and one that moved out read
// differently from a plain write — "pruned" alone would suggest the transcription was
// deleted, when it was set aside.
func TestIngestDetailAdoption(t *testing.T) {
	r := snorg.IngestResult{Report: &snorg.WriteReport{
		Pages: []snorg.PageWriteReport{
			{PageID: "P1", JSONChanged: true, AdoptedFrom: []string{"F_A", "orphans"}},
		},
		Pruned:   []string{"P8", "P9"},
		Parked:   []string{"P9"},
		Repaired: []string{"F_A"},
	}}
	want := []string{
		"P1: json, adopted from F_A, orphans",
		"pruned P8",
		"parked P9 (transcription kept)",
		"repaired F_A/note.json",
	}
	if got := ingestDetail(r); !reflect.DeepEqual(got, want) {
		t.Errorf("ingestDetail =\n%v\nwant\n%v", got, want)
	}
}

// TestDuplicatePages: two notes claiming the same page in one batch means the page
// was copied, not moved — snorg cannot represent that, so it must be named.
func TestDuplicatePages(t *testing.T) {
	results := []snorg.IngestResult{
		{Note: &snorg.Note{FileID: "F_A", Pages: []snorg.NotePage{{ID: "P1"}, {ID: "P2"}}}},
		{Note: &snorg.Note{FileID: "F_B", Pages: []snorg.NotePage{{ID: "P2"}, {ID: "P3"}}}},
		{Err: errTest}, // a failed note contributes no claims
	}
	got := duplicatePages(results)
	want := map[string][]string{"P2": {"F_A", "F_B"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("duplicatePages = %v, want %v", got, want)
	}
}

var errTest = errors.New("boom")
