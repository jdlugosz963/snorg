package archive

import "testing"

func TestTextChangeOfKinds(t *testing.T) {
	cases := []struct {
		name     string
		was, now string
		want     TextKind
	}{
		{"both empty", "", "", TextUnchanged},
		{"identical", "a\n", "a\n", TextUnchanged},
		{"trailing newline only", "a", "a\n", TextUnchanged},
		{"fresh", "", "a\n", TextNew},
		{"erased", "a\n", "", TextCleared},
		{"erased to whitespace", "a\n", "\n\n", TextCleared},
		{"changed", "a\n", "b\n", TextUpdated},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := TextChangeOf(c.was, c.now)
			if got.Kind != c.want {
				t.Errorf("Kind = %q, want %q", got.Kind, c.want)
			}
			// Was/Now are always the stored form, whatever the kind.
			if got.Was != NormMD(c.was) || got.Now != NormMD(c.now) {
				t.Errorf("Was/Now = %q/%q, want %q/%q", got.Was, got.Now, NormMD(c.was), NormMD(c.now))
			}
			if got.Changed() != (c.want != TextUnchanged) {
				t.Errorf("Changed() = %v for kind %q", got.Changed(), got.Kind)
			}
		})
	}
}

// TextChangeOf never reports TextReverted — that verdict belongs to the write
// that knows the AI base (WriteAnalysisEdit), not to a comparison of two texts.
func TestTextChangeOfNeverReverts(t *testing.T) {
	if got := TextChangeOf("edited\n", "base\n"); got.Kind != TextUpdated {
		t.Errorf("Kind = %q, want %q", got.Kind, TextUpdated)
	}
}

func TestTextChangeOfSniffsConflicts(t *testing.T) {
	now := "<<<<<<< edited\nmine\n=======\ntheirs\n>>>>>>> reanalyzed\n"
	if got := TextChangeOf("a\n", now); !got.Conflicts {
		t.Error("Conflicts = false, want true")
	}
	if got := TextChangeOf("a\n", "b\n"); got.Conflicts {
		t.Error("Conflicts = true on clean text, want false")
	}
}

// A zero TextChange is one nobody computed, distinct from an unchanged one.
func TestZeroTextChangeIsNotUnchanged(t *testing.T) {
	var zero TextChange
	if zero.Changed() {
		t.Error("zero TextChange reports Changed()")
	}
	if zero.Kind == TextUnchanged {
		t.Error("zero TextChange has Kind unchanged; it must be empty")
	}
}
