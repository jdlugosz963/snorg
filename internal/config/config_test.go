package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func writeCfg(t *testing.T, name, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadMergeAndDefaults(t *testing.T) {
	base := writeCfg(t, "base.yaml", `
archive: /base/archive
provider:
  endpoint: https://example.test/v1
  api_key: base-key
  model: base-model
analysis:
  content:
    prompt: base content prompt
  fields:
    summary:
      prompt: summarize this
`)
	override := writeCfg(t, "override.yaml", `
archive: /override/archive
provider:
  model: override-model
analysis:
  fields:
    todos:
      prompt: list todos
`)

	cfg, err := Load([]string{base, override})
	if err != nil {
		t.Fatal(err)
	}

	// Scalar override: later file wins; non-overridden scalar preserved.
	if cfg.Archive != "/override/archive" {
		t.Errorf("archive = %q, want /override/archive", cfg.Archive)
	}
	if cfg.Provider.Model != "override-model" {
		t.Errorf("model = %q, want override-model", cfg.Provider.Model)
	}
	if cfg.Provider.APIKey != "base-key" {
		t.Errorf("api_key = %q, want base-key", cfg.Provider.APIKey)
	}
	// Maps merge per key: both fields survive.
	if cfg.Analysis.Fields["summary"].Prompt != "summarize this" {
		t.Errorf("fields[summary] = %+v", cfg.Analysis.Fields["summary"])
	}
	if cfg.Analysis.Fields["todos"].Prompt != "list todos" {
		t.Errorf("fields[todos] = %+v", cfg.Analysis.Fields["todos"])
	}
	// Explicit prompt kept; unset prompts get built-in defaults.
	if cfg.Analysis.Content.Prompt != "base content prompt" {
		t.Errorf("content prompt = %q", cfg.Analysis.Content.Prompt)
	}
	if cfg.Analysis.Content.UpdatePrompt != defaultUpdatePrompt {
		t.Errorf("update prompt not defaulted")
	}
	if cfg.Analysis.Titles.Prompt != defaultTitlePrompt {
		t.Errorf("title prompt not defaulted")
	}
	if cfg.Analysis.Links.Prompt != defaultLinkPrompt {
		t.Errorf("link prompt not defaulted")
	}
}

func TestLoadArchivePathResolution(t *testing.T) {
	// A relative archive: resolves against the declaring file's directory.
	p := writeCfg(t, "config.yaml", "archive: .\n")
	dir := filepath.Dir(p)
	cfg, err := Load([]string{p})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Archive != dir {
		t.Errorf("archive '.' = %q, want %q", cfg.Archive, dir)
	}

	p2 := writeCfg(t, "config.yaml", "archive: sub/x\n")
	dir2 := filepath.Dir(p2)
	cfg2, err := Load([]string{p2})
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir2, "sub/x"); cfg2.Archive != want {
		t.Errorf("archive 'sub/x' = %q, want %q", cfg2.Archive, want)
	}

	// Absolute stays as-is; a leading ~ expands to $HOME.
	pAbs := writeCfg(t, "config.yaml", "archive: /abs/archive\n")
	cfgAbs, err := Load([]string{pAbs})
	if err != nil {
		t.Fatal(err)
	}
	if cfgAbs.Archive != "/abs/archive" {
		t.Errorf("absolute archive = %q, want /abs/archive", cfgAbs.Archive)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	pTilde := writeCfg(t, "config.yaml", "archive: ~/notes/sn\n")
	cfgTilde, err := Load([]string{pTilde})
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, "notes/sn"); cfgTilde.Archive != want {
		t.Errorf("~ archive = %q, want %q", cfgTilde.Archive, want)
	}
}

func TestLoadUpdatePromptOverride(t *testing.T) {
	p := writeCfg(t, "c.yaml", `
analysis:
  content:
    update_prompt: custom update
`)
	cfg, err := Load([]string{p})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Analysis.Content.UpdatePrompt != "custom update" {
		t.Errorf("update_prompt = %q, want custom update", cfg.Analysis.Content.UpdatePrompt)
	}
	if cfg.Analysis.Content.Prompt != defaultContentPrompt {
		t.Errorf("content prompt not defaulted alongside custom update_prompt")
	}
}

// TestLoadSVGToggles: unset toggles default to true; an explicit false in a
// later file survives the merge.
func TestLoadSVGToggles(t *testing.T) {
	base := writeCfg(t, "base.yaml", `
ingest:
  svg:
    links: true
`)
	override := writeCfg(t, "override.yaml", `
ingest:
  svg:
    navigation: false
`)
	cfg, err := Load([]string{base, override})
	if err != nil {
		t.Fatal(err)
	}
	s := cfg.Ingest.SVG
	for name, b := range map[string]*bool{
		"links": s.Links, "navigation": s.Navigation, "format": s.Format,
	} {
		if b == nil {
			t.Fatalf("ingest.svg.%s is nil after defaults", name)
		}
	}
	if !*s.Links || !*s.Format {
		t.Errorf("unset/true toggles = links:%v format:%v, want both true", *s.Links, *s.Format)
	}
	if *s.Navigation {
		t.Error("explicit navigation: false was lost")
	}
	if s.Background != "extract" {
		t.Errorf("unset background mode = %q, want extract", s.Background)
	}
}

// TestLoadBackgroundModeAndColors: background mode and color remap parse, and an
// explicit background mode survives defaults.
func TestLoadBackgroundModeAndColors(t *testing.T) {
	p := writeCfg(t, "c.yaml", `
ingest:
  svg:
    background: blank
    colors:
      black: "#1a1aff"
      white: red
`)
	cfg, err := Load([]string{p})
	if err != nil {
		t.Fatal(err)
	}
	s := cfg.Ingest.SVG
	if s.Background != "blank" {
		t.Errorf("background = %q, want blank", s.Background)
	}
	if s.Colors["black"] != "#1a1aff" || s.Colors["white"] != "red" {
		t.Errorf("colors = %v, want black=#1a1aff white=red", s.Colors)
	}
	if err := cfg.ValidateIngest(); err != nil {
		t.Errorf("ValidateIngest on valid config: %v", err)
	}
}

// TestValidateIngest rejects an unknown background mode and unknown color shade.
func TestValidateIngest(t *testing.T) {
	badMode := writeCfg(t, "mode.yaml", "ingest:\n  svg:\n    background: nope\n")
	cfg, err := Load([]string{badMode})
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.ValidateIngest(); err == nil {
		t.Error("expected error for unknown background mode")
	}

	badShade := writeCfg(t, "shade.yaml", "ingest:\n  svg:\n    colors:\n      purple: blue\n")
	cfg, err = Load([]string{badShade})
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.ValidateIngest(); err == nil {
		t.Error("expected error for unknown color shade")
	}
}

func TestResolveAPIKey(t *testing.T) {
	// Literal api_key wins over both command and env.
	t.Run("literal wins", func(t *testing.T) {
		t.Setenv(apiKeyEnv, "env-key")
		cfg := &Config{Provider: Provider{APIKey: "lit-key", APIKeyCommand: "printf cmd-key"}}
		if err := cfg.ResolveAPIKey(); err != nil {
			t.Fatal(err)
		}
		if cfg.Provider.APIKey != "lit-key" {
			t.Errorf("api_key = %q, want lit-key", cfg.Provider.APIKey)
		}
	})

	// Command stdout (trimmed) beats env when the literal is empty.
	t.Run("command over env", func(t *testing.T) {
		t.Setenv(apiKeyEnv, "env-key")
		cfg := &Config{Provider: Provider{APIKeyCommand: "printf 'secret-key\\n'"}}
		if err := cfg.ResolveAPIKey(); err != nil {
			t.Fatal(err)
		}
		if cfg.Provider.APIKey != "secret-key" {
			t.Errorf("api_key = %q, want trimmed secret-key", cfg.Provider.APIKey)
		}
	})

	// Env fallback when neither literal nor command is set.
	t.Run("env fallback", func(t *testing.T) {
		t.Setenv(apiKeyEnv, "env-key")
		cfg := &Config{}
		if err := cfg.ResolveAPIKey(); err != nil {
			t.Fatal(err)
		}
		if cfg.Provider.APIKey != "env-key" {
			t.Errorf("api_key = %q, want env-key", cfg.Provider.APIKey)
		}
	})

	// A failing command is an error, not a silent empty key.
	t.Run("command failure", func(t *testing.T) {
		cfg := &Config{Provider: Provider{APIKeyCommand: "exit 3"}}
		if err := cfg.ResolveAPIKey(); err == nil {
			t.Error("expected error from failing api_key_command")
		}
	})
}

func TestValidateProvider(t *testing.T) {
	t.Setenv(apiKeyEnv, "")
	cases := map[string]string{
		"missing endpoint": "provider:\n  model: m\n  api_key: k\n",
		"missing model":    "provider:\n  endpoint: e\n  api_key: k\n",
		"missing key":      "provider:\n  endpoint: e\n  model: m\n",
		"field no prompt":  "provider:\n  endpoint: e\n  model: m\n  api_key: k\nanalysis:\n  fields:\n    bad: {}\n",
	}
	for name, body := range cases {
		p := writeCfg(t, "c.yaml", body)
		cfg, err := Load([]string{p})
		if err != nil {
			t.Fatalf("%s: Load: %v", name, err)
		}
		if err := cfg.ValidateProvider(); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home dir: %v", err)
	}
	cases := map[string]string{
		"~":          home,
		"~/notes/sn": filepath.Join(home, "notes/sn"),
		"/abs/notes": "/abs/notes",
		"rel/notes":  "rel/notes",
		"~notuser/x": "~notuser/x", // ~ not followed by / is left alone
	}
	for in, want := range cases {
		if got := ExpandHome(in); got != want {
			t.Errorf("ExpandHome(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestCloneIsDeep writes through every reference-typed field of a clone and requires
// the original to be untouched — the check that keeps Clone complete as Config grows.
func TestCloneIsDeep(t *testing.T) {
	p := writeCfg(t, "c.yaml", `
provider:
  api_key: k
analysis:
  fields:
    summary:
      prompt: summarize
ingest:
  svg:
    links: false
    colors:
      black: "#111111"
templates:
  - image: t.png
    boxes:
      - id: title
        rect: {x: 0, y: 0, w: 10, h: 10}
`)
	cfg, err := Load([]string{p})
	if err != nil {
		t.Fatal(err)
	}
	before, err := Load([]string{p})
	if err != nil {
		t.Fatal(err)
	}

	cp := cfg.Clone()
	cp.Provider.APIKey = "leaked"
	cp.Analysis.Fields["summary"] = Task{Prompt: "other"}
	cp.Analysis.Fields["added"] = Task{Prompt: "new"}
	cp.Ingest.SVG.Colors["black"] = "#222222"
	*cp.Ingest.SVG.Links = true
	cp.Templates[0].Image = "other.png"
	cp.Templates[0].Boxes[0].ID = "renamed"

	if !reflect.DeepEqual(cfg, before) {
		t.Errorf("mutating the clone changed the original:\n got %+v\nwant %+v", cfg, before)
	}
	// The pointers are distinct, not merely equal in value.
	if cp.Ingest.SVG.Links == cfg.Ingest.SVG.Links {
		t.Error("Clone shares the SVG toggle pointer")
	}
}
