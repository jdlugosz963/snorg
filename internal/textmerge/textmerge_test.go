package textmerge

import (
	"os/exec"
	"strings"
	"testing"
)

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not on PATH: %v", err)
	}
}

func TestDiffEqual(t *testing.T) {
	requireGit(t)
	d, err := Diff("same\n", "same\n")
	if err != nil {
		t.Fatal(err)
	}
	if d != "" {
		t.Errorf("diff of equal content = %q, want empty", d)
	}
}

func TestDiffUnapplyRoundTrip(t *testing.T) {
	requireGit(t)
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
	requireGit(t)
	d, err := Diff("a\n", "b\n")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Unapply("something else entirely\n", d); err == nil {
		t.Fatal("expected error for a diff that does not apply")
	}
}

func TestMergeClean(t *testing.T) {
	requireGit(t)
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
	requireGit(t)
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
	requireGit(t)
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
