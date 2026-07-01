package config

import (
	"os"
	"path/filepath"
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
	if cfg.Analysis.Titles.Prompt != defaultTitlePrompt {
		t.Errorf("title prompt not defaulted")
	}
	if cfg.Analysis.Links.Prompt != defaultLinkPrompt {
		t.Errorf("link prompt not defaulted")
	}
}

func TestLoadAPIKeyFromEnv(t *testing.T) {
	t.Setenv(apiKeyEnv, "env-key")
	p := writeCfg(t, "c.yaml", `
provider:
  endpoint: https://example.test/v1
  model: m
`)
	cfg, err := Load([]string{p})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Provider.APIKey != "env-key" {
		t.Errorf("api_key = %q, want env fallback env-key", cfg.Provider.APIKey)
	}
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
