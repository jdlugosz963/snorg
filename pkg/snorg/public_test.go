package snorg_test

import (
	"context"
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

	// The analyze surface is nameable from outside: the options struct (including
	// the result callback), the Provider seam, and the two entry points. Not
	// called — that would need credentials — but this fails to compile if a type
	// an external consumer must name stops being re-exported.
	spec := c.AnalyzeSpec()
	var _ snorg.AnalyzeOptions = snorg.AnalyzeOptions{
		Force: true,
		Spec:  &spec,
		OnResult: func(r snorg.AnalyzeResult) {
			// Every PageResult fact is promoted through the embedding.
			var _ string = r.PageID
			var _ bool = r.Skipped
			var _ *snorg.TextChange = r.Content
			var _ []snorg.RegionResult = r.Regions
			var _ []snorg.NameChange = r.Names
			var _ []string = r.Fields
			var _ int = r.Calls
			var _ bool = r.JSONChanged
			var _ snorg.PageResult = r.PageResult
		},
	}
	var _ func() (snorg.Provider, error) = c.NewProvider
	var _ func(context.Context, snorg.Provider, []string, snorg.AnalyzeOptions) ([]snorg.AnalyzeResult, error) = c.Analyze

	// Rendering: a rect (the aliased snote.Rect, as carried by a PageDoc title or
	// a templates: box) crops straight to PNG bytes, in either the canonical or
	// the styled form.
	var _ func(string, string, snorg.Rect, bool) ([]byte, error) = c.RenderRegion

	// The change vocabulary and its two renderers. The edit surface had no guard
	// before, which is how its result type could drift from the analyze one.
	var _ snorg.TextKind = snorg.TextUnchanged
	var _ snorg.TextKind = snorg.TextNew
	var _ snorg.TextKind = snorg.TextUpdated
	var _ snorg.TextKind = snorg.TextCleared
	var _ snorg.TextKind = snorg.TextReverted
	var tc snorg.TextChange
	var _ snorg.TextKind = tc.Kind
	var _ bool = tc.Conflicts
	var _, _ string = tc.Was, tc.Now
	var _ bool = tc.Changed()
	var _ func(string, string) snorg.TextChange = snorg.TextChangeOf
	var _ func(snorg.TextChange) (string, error) = snorg.TextDiff
	var _ func(snorg.TextChange) (int, int, error) = snorg.TextStat

	var rc snorg.RegionChange
	var _, _ string = rc.ID, rc.Label
	var _ snorg.TextChange = rc.Text
	var nc snorg.NameChange
	var _ string = nc.Kind
	var _ int = nc.Index
	var _ bool = nc.Override

	// The editor round-trip: buffer out, buffer in, one PageEdit describing what
	// the save changed.
	var _ func(string) (string, error) = c.PageBuffer
	var _ func(string, string) (snorg.PageEdit, error) = c.ApplyPage
	var _ func(string, string) (snorg.PageEdit, error) = c.EditPage
	var pe snorg.PageEdit
	var _ snorg.TextChange = pe.Content
	var _ []snorg.RegionChange = pe.Regions
	var _ []snorg.NameChange = pe.Names

	// The three batch commands all stream their per-item results the same way.
	var _ func([]string, snorg.IngestOptions) ([]snorg.IngestResult, error) = c.Ingest
	var _ func([]string, snorg.MigrateOptions) ([]snorg.MigrateResult, error) = c.Migrate
	var _ func(snorg.MigrateOptions) ([]snorg.MigrateResult, error) = c.MigrateAll
	var _ snorg.IngestOptions = snorg.IngestOptions{OnResult: func(snorg.IngestResult) {}}
	var _ snorg.MigrateOptions = snorg.MigrateOptions{OnResult: func(snorg.MigrateResult) {}}
}
