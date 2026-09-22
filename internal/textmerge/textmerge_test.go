package textmerge

import (
	"strings"
	"testing"
)

func TestDiffEqual(t *testing.T) {
	d, err := Diff("same\n", "same\n")
	if err != nil {
		t.Fatal(err)
	}
	if d != "" {
		t.Errorf("diff of equal content = %q, want empty", d)
	}
}

func TestDiffUnapplyRoundTrip(t *testing.T) {
	cases := map[string][2]string{
		"edit":       {"a\nb\nc\n", "a\nB\nc\n"},
		"from empty": {"", "written by hand\n"},
		"to empty":   {"gone\n", ""},
		"multi-hunk": {
			"one\ntwo\nthree\nfour\nfive\nsix\nseven\neight\nnine\nten\n",
			"ONE\ntwo\nthree\nfour\nfive\nsix\nseven\neight\nnine\nTEN\n",
		},
	}
	for name, c := range cases {
		old, new := c[0], c[1]
		d, err := Diff(old, new)
		if err != nil {
			t.Fatalf("%s: Diff: %v", name, err)
		}
		if d == "" {
			t.Fatalf("%s: expected a non-empty diff", name)
		}
		got, err := Unapply(new, d)
		if err != nil {
			t.Fatalf("%s: Unapply: %v", name, err)
		}
		if got != old {
			t.Errorf("%s: round trip = %q, want %q", name, got, old)
		}
	}
}

func TestDiffIsUnifiedNotJSON(t *testing.T) {
	d, err := Diff("a\nb\nc\n", "a\nB\nc\n")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"@@", "-b\n", "+B\n"} {
		if !strings.Contains(d, want) {
			t.Errorf("diff missing unified marker %q:\n%s", want, d)
		}
	}
	for _, bad := range []string{"\"operation\"", "\"source_start\"", "{"} {
		if strings.Contains(d, bad) {
			t.Errorf("diff still looks like the JSON struct (contains %q):\n%s", bad, d)
		}
	}
}

// TestDiffContextIsOneLine pins the terse `-U1` output: a single-line change in
// the middle of a larger document keeps exactly one context (' '-prefixed) line
// on each side — not 3, and not the whole file (which Context <= 0 would emit).
func TestDiffContextIsOneLine(t *testing.T) {
	old := "1\n2\n3\n4\n5\n6\n7\n"
	new := "1\n2\n3\nFOUR\n5\n6\n7\n"
	d, err := Diff(old, new)
	if err != nil {
		t.Fatal(err)
	}
	var context int
	for _, l := range strings.Split(d, "\n") {
		if strings.HasPrefix(l, " ") {
			context++
		}
	}
	if context != 2 {
		t.Errorf("expected exactly 2 context lines (one each side), got %d:\n%s", context, d)
	}
	got, err := Unapply(new, d)
	if err != nil {
		t.Fatalf("Unapply: %v", err)
	}
	if got != old {
		t.Errorf("round trip = %q, want %q", got, old)
	}
}

func TestUnapplyEmptyDiff(t *testing.T) {
	got, err := Unapply("content\n", "")
	if err != nil {
		t.Fatal(err)
	}
	if got != "content\n" {
		t.Errorf("empty diff changed content: %q", got)
	}
}

func TestUnapplyMismatchedDiff(t *testing.T) {
	d, err := Diff("a\n", "b\n")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Unapply("something else entirely\n", d); err == nil {
		t.Fatal("expected error for a diff that does not apply")
	}
}

func TestMergeClean(t *testing.T) {
	base := "intro\nline one\nline two\noutro\n"
	mine := "intro\nline one, edited by hand\nline two\noutro\n"
	theirs := "intro\nline one\nline two\noutro, reanalyzed\n"
	merged, conflicts, err := Merge(base, mine, theirs)
	if err != nil {
		t.Fatal(err)
	}
	if conflicts {
		t.Fatalf("unexpected conflicts:\n%s", merged)
	}
	want := "intro\nline one, edited by hand\nline two\noutro, reanalyzed\n"
	if merged != want {
		t.Errorf("merged = %q, want %q", merged, want)
	}
}

func TestMergeConflict(t *testing.T) {
	merged, conflicts, err := Merge("line\n", "line edited\n", "line reanalyzed\n")
	if err != nil {
		t.Fatal(err)
	}
	if !conflicts {
		t.Fatalf("expected conflicts, got clean merge: %q", merged)
	}
	for _, marker := range []string{"<<<<<<< edited", ">>>>>>> reanalyzed"} {
		if !strings.Contains(merged, marker) {
			t.Errorf("merged output missing marker %q:\n%s", marker, merged)
		}
	}
}

func TestMergeEmptyBaseConflicts(t *testing.T) {
	// Both sides added content over nothing: human transcription vs first AI
	// analysis. This must conflict, never silently pick a side.
	merged, conflicts, err := Merge("", "written by hand\n", "transcribed by AI\n")
	if err != nil {
		t.Fatal(err)
	}
	if !conflicts {
		t.Fatalf("expected conflicts, got: %q", merged)
	}
	if !strings.Contains(merged, "written by hand") || !strings.Contains(merged, "transcribed by AI") {
		t.Errorf("merged output lost a side:\n%s", merged)
	}
}

func TestStat(t *testing.T) {
	cases := []struct {
		name           string
		old, new       string
		added, removed int
	}{
		{"equal", "a\nb\n", "a\nb\n", 0, 0},
		{"insert", "a\nb\n", "a\nx\nb\n", 1, 0},
		{"delete", "a\nx\nb\n", "a\nb\n", 0, 1},
		{"replace", "a\nb\n", "a\nB\n", 1, 1},
		{"from empty", "", "a\nb\n", 2, 0},
		{"to empty", "a\nb\n", "", 0, 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			added, removed, err := Stat(c.old, c.new)
			if err != nil {
				t.Fatal(err)
			}
			if added != c.added || removed != c.removed {
				t.Errorf("Stat = +%d/-%d, want +%d/-%d", added, removed, c.added, c.removed)
			}
		})
	}
}

func TestHasConflicts(t *testing.T) {
	merged, conflicts, err := Merge("base\n", "mine\n", "theirs\n")
	if err != nil {
		t.Fatal(err)
	}
	if !conflicts {
		t.Fatal("want a conflicting merge to build the fixture")
	}
	if !HasConflicts(merged) {
		t.Errorf("HasConflicts(%q) = false, want true", merged)
	}
	if HasConflicts("plain text\n") {
		t.Error("HasConflicts on clean text = true, want false")
	}
}
