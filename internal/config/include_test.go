package config

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// writeIn writes body to <dir>/name and returns the full path, so several config
// files can share a directory (relative include/image paths resolve against it).
func writeIn(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestIncludeAboveIncluder: a file's include: list overrides that file's own keys,
// and include paths resolve relative to the including file's directory.
func TestIncludeAboveIncluder(t *testing.T) {
	dir := t.TempDir()
	writeIn(t, dir, "over.yaml", "provider:\n  model: from-include\n")
	main := writeIn(t, dir, "main.yaml", "include:\n  - over.yaml\nprovider:\n  model: from-main\n  endpoint: https://main.test/v1\n")

	cfg, err := Load([]string{main})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Provider.Model != "from-include" {
		t.Errorf("model = %q, want from-include (include overrides includer)", cfg.Provider.Model)
	}
	// A key the include does not set is kept from the including file.
	if cfg.Provider.Endpoint != "https://main.test/v1" {
		t.Errorf("endpoint = %q, want the includer's value", cfg.Provider.Endpoint)
	}
}

// TestIncludeOrderAndNesting: later-listed includes win over earlier ones, and an
// include's own includes win over its body.
func TestIncludeOrderAndNesting(t *testing.T) {
	dir := t.TempDir()
	writeIn(t, dir, "deep.yaml", "provider:\n  model: deep\n")
	writeIn(t, dir, "a.yaml", "provider:\n  model: a\n")
	writeIn(t, dir, "b.yaml", "include:\n  - deep.yaml\nprovider:\n  model: b\n")
	main := writeIn(t, dir, "main.yaml", "include:\n  - a.yaml\n  - b.yaml\nprovider:\n  model: main\n")

	cfg, err := Load([]string{main})
	if err != nil {
		t.Fatal(err)
	}
	// Effective low→high: main body, a, b body, deep → deep wins.
	if cfg.Provider.Model != "deep" {
		t.Errorf("model = %q, want deep", cfg.Provider.Model)
	}
}

// TestTopLayerBeatsIncludes: a later top-level path still overrides an earlier
// path's includes (the whole earlier layer, includes and all, is lower).
func TestTopLayerBeatsIncludes(t *testing.T) {
	dir := t.TempDir()
	writeIn(t, dir, "inc.yaml", "provider:\n  model: inc\n")
	lower := writeIn(t, dir, "lower.yaml", "include:\n  - inc.yaml\n")
	higher := writeIn(t, dir, "higher.yaml", "provider:\n  model: higher\n")

	cfg, err := Load([]string{lower, higher})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Provider.Model != "higher" {
		t.Errorf("model = %q, want higher (later top-level path wins over an earlier one's include)", cfg.Provider.Model)
	}
}

// TestIncludeCycleErrors: a file that includes itself (directly or via a chain) is
// an error, not an infinite loop.
func TestIncludeCycleErrors(t *testing.T) {
	dir := t.TempDir()
	writeIn(t, dir, "a.yaml", "include:\n  - b.yaml\n")
	writeIn(t, dir, "b.yaml", "include:\n  - a.yaml\n")
	if _, err := Load([]string{filepath.Join(dir, "a.yaml")}); err == nil {
		t.Fatal("expected an include cycle error")
	}
}

// TestTemplateImageResolvedRelativeToFile: a template's image: path becomes
// absolute relative to the config file that declared it.
func TestTemplateImageResolvedRelativeToFile(t *testing.T) {
	dir := t.TempDir()
	main := writeIn(t, dir, "config.yaml", "templates:\n  - image: templates/bg.png\n    boxes:\n      - {id: title, rect: {x: 0, y: 0, w: 100, h: 100}, analyze: true}\n")

	cfg, err := Load([]string{main})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Templates) != 1 {
		t.Fatalf("templates = %+v, want 1", cfg.Templates)
	}
	want := filepath.Join(dir, "templates", "bg.png")
	if cfg.Templates[0].Image != want {
		t.Errorf("image = %q, want %q", cfg.Templates[0].Image, want)
	}
	if cfg.Templates[0].Boxes[0].ID != "title" || cfg.Templates[0].Boxes[0].Rect.W != 100 {
		t.Errorf("box = %+v", cfg.Templates[0].Boxes[0])
	}
}

// TestTemplateImageFromIncludeResolvesToIncludeDir: when the templates block comes
// from an included file, its image resolves relative to that included file's dir,
// not the includer's.
func TestTemplateImageFromIncludeResolvesToIncludeDir(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	writeIn(t, sub, "tmpl.yaml", "templates:\n  - image: bg.png\n    boxes:\n      - {id: a, rect: {x: 0, y: 0, w: 10, h: 10}, analyze: true}\n")
	main := writeIn(t, root, "main.yaml", "include:\n  - sub/tmpl.yaml\n")

	cfg, err := Load([]string{main})
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(sub, "bg.png")
	if len(cfg.Templates) != 1 || cfg.Templates[0].Image != want {
		t.Fatalf("image = %+v, want %q", cfg.Templates, want)
	}
}

// tmplYAML is a one-template templates: section naming image, so a test can hand
// each layer its own distinguishable entry.
func tmplYAML(image string) string {
	return "templates:\n  - image: " + image + "\n    boxes:\n      - {id: a, rect: {x: 0, y: 0, w: 10, h: 10}, analyze: true}\n"
}

// images lists the resolved image path of every template, in order.
func images(cfg *Config) []string {
	out := make([]string, 0, len(cfg.Templates))
	for _, t := range cfg.Templates {
		out = append(out, t.Image)
	}
	return out
}

// TestTemplatesConcatAcrossIncludes: a sequence is merged, not replaced — the
// including file's own templates and each include's all apply, body first, includes
// in listed order, each image still resolved against its own declaring file.
func TestTemplatesConcatAcrossIncludes(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	writeIn(t, root, "one.yaml", tmplYAML("one.png"))
	writeIn(t, sub, "two.yaml", tmplYAML("two.png"))
	main := writeIn(t, root, "main.yaml", "include:\n  - one.yaml\n  - sub/two.yaml\n"+tmplYAML("main.png"))

	cfg, err := Load([]string{main})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		filepath.Join(root, "main.png"),
		filepath.Join(root, "one.png"),
		filepath.Join(sub, "two.png"),
	}
	if got := images(cfg); !slices.Equal(got, want) {
		t.Errorf("templates = %v, want %v", got, want)
	}
}

// TestSequencesConcatAcrossTopLevelPaths: the same merge applies between top-level
// -c layers — a later file adds to the earlier file's list instead of replacing it.
func TestSequencesConcatAcrossTopLevelPaths(t *testing.T) {
	dir := t.TempDir()
	a := writeIn(t, dir, "a.yaml", tmplYAML("a.png"))
	b := writeIn(t, dir, "b.yaml", tmplYAML("b.png"))

	cfg, err := Load([]string{a, b})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(dir, "a.png"), filepath.Join(dir, "b.png")}
	if got := images(cfg); !slices.Equal(got, want) {
		t.Errorf("templates = %v, want %v", got, want)
	}
}
