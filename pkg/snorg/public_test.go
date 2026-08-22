package snorg_test

import (
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

	// Compose predicates and parse a filter through the public API.
	pred, err := c.ParseFilter("not", []string{"starred"})
	if err != nil {
		t.Fatalf("ParseFilter: %v", err)
	}
	matches, err := c.Query(snorg.And(pred, snorg.All))
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	// Name the returned alias types to prove they are usable from outside.
	var _ []snorg.Match = matches

	if from, to, err := snorg.ParseDateSpec("2026-07-01.."); err != nil || from != "20260701" || to != "" {
		t.Errorf("ParseDateSpec = (%q, %q, %v)", from, to, err)
	}

	// The tag filter and the tag/inventory methods are reachable through the facade.
	if _, err := c.ParseFilter("tag", []string{"work"}); err != nil {
		t.Errorf("ParseFilter tag: %v", err)
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

	// Config is nameable and its sections reachable via fields.
	cfg, err := snorg.LoadConfig(nil)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	var _ *snorg.Config = cfg
	_ = cfg.Provider.Model
}
