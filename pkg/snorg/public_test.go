package snorg_test

import (
	"regexp"
	"testing"

	snorg "github.com/jdlugosz963/snorg/pkg/snorg"
)

// TestPublicSurface uses only the public snorg package — importing no internal/*
// path — to prove an external consumer can open an archive and drive it entirely
// through the facade (the re-exported aliases make every returned type nameable).
func TestPublicSurface(t *testing.T) {
	dir := t.TempDir()

	c, err := snorg.Open(dir, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	ids, err := c.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(ids) != 0 {
		t.Errorf("empty archive List = %v, want none", ids)
	}

	// Compose predicates and parse an expression through the public API.
	pred, err := snorg.ParseQuery("NOT starred AND (tag:work OR templated)")
	if err != nil {
		t.Fatalf("ParseQuery: %v", err)
	}
	matches, err := c.Query(snorg.MatchAnd(pred, snorg.MatchAll))
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	// Name the returned alias types to prove they are usable from outside.
	var _ []snorg.Match = matches

	if from, to, err := snorg.ParseDateSpec("2026-07-01.."); err != nil || from != "20260701" || to != "" {
		t.Errorf("ParseDateSpec = (%q, %q, %v)", from, to, err)
	}

	// The matcher constructors and the Match* family they feed are nameable from
	// outside, hand-written matchers included.
	var _ snorg.TextMatcher = snorg.Substring("work")
	var _ snorg.TextMatcher = snorg.Exact("work")
	var _ snorg.TextMatcher = snorg.Regexp(regexp.MustCompile("^work$"))
	var _ snorg.Predicate = snorg.MatchOr(
		snorg.MatchTag(func(s string) bool { return s == "work" }),
		snorg.MatchRegion("grade", snorg.Substring("A")),
		snorg.MatchTemplated,
	)
	// A bad regexp in an expression is reported rather than panicking.
	if _, err := snorg.ParseQuery("region[grade]~("); err == nil {
		t.Error("ParseQuery with an invalid regexp should error")
	}
	tags, err := c.Tags()
	if err != nil {
		t.Fatalf("Tags: %v", err)
	}
	if len(tags) != 0 {
		t.Errorf("empty archive Tags = %v, want none", tags)
	}
	// Name the alias, and prove Tag is callable (an unknown page is an error).
	var _ []snorg.ValueCount = tags
	if _, err := c.Tag("work", []string{"Pmissing"}, false); err == nil {
		t.Error("Tag on an unknown PAGEID should error")
	}
	// Same for the note-level tag, plus the inheritance rule it feeds.
	if _, err := c.TagNote("work", []string{"Fmissing"}, false); err == nil {
		t.Error("TagNote on an unknown FILE_ID should error")
	}
	if got := snorg.EffectiveTags(snorg.NoteDoc{Tags: []string{"note"}}, snorg.PageDoc{Tags: []string{"own"}}); len(got) != 2 {
		t.Errorf("EffectiveTags = %v, want the 2-element union", got)
	}

	// Config is nameable and its sections reachable via fields.
	cfg, err := snorg.LoadConfig(nil)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	var _ *snorg.Config = cfg
	_ = cfg.Provider.Model
}
