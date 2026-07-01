// Package config loads snorg's runtime configuration: provider credentials and
// analysis prompts. Configuration is split across one or more YAML files that are
// deep-merged (later files win), so secrets (api_key) can live in a separate,
// gitignored file from the committed analysis configuration.
//
// This is the only non-analyze package allowed an external dependency (YAML); the
// rest of snorg stays stdlib-only.
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// apiKeyEnv is the fallback source for the provider key when api_key is left empty
// in every config file, so a secret need not be written to disk.
const apiKeyEnv = "OPENAI_API_KEY"

// Built-in default prompts, used for any analysis task a config file leaves unset.
const (
	defaultContentPrompt = "Transcribe all text on this handwritten note page as plaintext, " +
		"preserving reading order. Output only the transcription, no commentary."
	defaultTitlePrompt = "This is a cropped title region from a handwritten note page. " +
		"Reply with a short one-line name: the transcribed title text, or, if it depicts " +
		"something, what it represents. Output only the name."
	defaultLinkPrompt = "This is a cropped link region from a handwritten note page. " +
		"Reply with a short one-line name: the transcribed text, or what it represents. " +
		"Output only the name."
)

// Config is the merged configuration. Required-field checks are not run by Load:
// each command validates only the section it uses (analyze: ValidateProvider; export:
// a non-empty Export.Template), so an export-only config needs no provider creds.
type Config struct {
	Provider Provider `yaml:"provider"`
	Analysis Analysis `yaml:"analysis"`
	Export   Export   `yaml:"export"`
}

// Export configures the generic template exporter (export command): a single pongo2
// template rendered over the retrieved note JSON.
type Export struct {
	Template string `yaml:"template"`
}

// Provider holds the OpenAI-compatible endpoint, credential and the single global
// model used for every analysis task.
type Provider struct {
	Endpoint string `yaml:"endpoint"`
	APIKey   string `yaml:"api_key"`
	Model    string `yaml:"model"`
}

// Analysis configures the per-task prompts. Content/Titles/Links are vision tasks
// over the page image; Fields are custom text tasks derived from the transcribed
// content (keyed by output name, e.g. "summary").
type Analysis struct {
	Content Task            `yaml:"content"`
	Titles  Task            `yaml:"titles"`
	Links   Task            `yaml:"links"`
	Fields  map[string]Task `yaml:"fields"`
}

// Task is one prompt-driven step. It is a struct (not a bare string) to leave room
// for a future per-task model override without a schema change.
type Task struct {
	Prompt string `yaml:"prompt"`
}

// Load reads each path, deep-merges them (later paths override earlier ones),
// decodes the result into a Config and fills in defaults. It is an error for a file
// to be unreadable or malformed. Load does not enforce required fields; callers run
// the validation for the section they need (e.g. ValidateProvider).
func Load(paths []string) (*Config, error) {
	merged := map[string]any{}
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("read config %s: %w", p, err)
		}
		var m map[string]any
		if err := yaml.Unmarshal(raw, &m); err != nil {
			return nil, fmt.Errorf("parse config %s: %w", p, err)
		}
		deepMerge(merged, m)
	}

	out, err := yaml.Marshal(merged)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(out, &cfg); err != nil {
		return nil, err
	}
	cfg.applyDefaults()
	return &cfg, nil
}

// deepMerge recursively merges src into dst: nested maps merge per key, every other
// value (scalars, sequences) is overwritten by src.
func deepMerge(dst, src map[string]any) {
	for k, sv := range src {
		if sm, ok := sv.(map[string]any); ok {
			if dm, ok := dst[k].(map[string]any); ok {
				deepMerge(dm, sm)
				continue
			}
		}
		dst[k] = sv
	}
}

func (c *Config) applyDefaults() {
	if c.Analysis.Content.Prompt == "" {
		c.Analysis.Content.Prompt = defaultContentPrompt
	}
	if c.Analysis.Titles.Prompt == "" {
		c.Analysis.Titles.Prompt = defaultTitlePrompt
	}
	if c.Analysis.Links.Prompt == "" {
		c.Analysis.Links.Prompt = defaultLinkPrompt
	}
	if c.Provider.APIKey == "" {
		c.Provider.APIKey = os.Getenv(apiKeyEnv)
	}
}

// ValidateProvider checks the fields the analyze command needs: provider credentials
// and a prompt for every custom analysis field.
func (c *Config) ValidateProvider() error {
	if c.Provider.Endpoint == "" {
		return fmt.Errorf("provider.endpoint is required")
	}
	if c.Provider.Model == "" {
		return fmt.Errorf("provider.model is required")
	}
	if c.Provider.APIKey == "" {
		return fmt.Errorf("provider.api_key is required (or set %s)", apiKeyEnv)
	}
	for name, t := range c.Analysis.Fields {
		if t.Prompt == "" {
			return fmt.Errorf("analysis.fields.%s: prompt is required", name)
		}
	}
	return nil
}
