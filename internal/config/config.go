// Package config loads snorg's runtime configuration: provider credentials,
// analysis prompts, ingest SVG toggles and the export template. Configuration is
// split across one or more YAML files that are deep-merged (later files win), so
// secrets (api_key) can live in a separate, gitignored file from the committed
// configuration.
//
// Its allowed external dependency is yaml.v3.
package config

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"gopkg.in/yaml.v3"
)

// apiKeyEnv is the fallback source for the provider key when api_key is left empty
// in every config file, so a secret need not be written to disk.
const apiKeyEnv = "OPENAI_API_KEY"

// defaultBackgroundMode is the ingest.svg.background value applied when unset: keep
// the page background, extracted into the note's backgrounds/ subfolder.
const defaultBackgroundMode = "extract"

// backgroundModes is the set of accepted ingest.svg.background values.
var backgroundModes = map[string]bool{"extract": true, "inline": true, "blank": true, "remove": true}

// Built-in default prompts, used for any analysis task a config file leaves unset.
const (
	defaultContentPrompt = "Transcribe this handwritten note page as clean Markdown. " +
		"Mirror the visual hierarchy: render headings as #..###### matching their visual " +
		"prominence, use - for bullet lists and 1. for numbered lists, and *emphasis*/**strong** " +
		"where the writing is clearly emphasized. Preserve reading order and transcribe only " +
		"what is written. Output only the Markdown transcription — no commentary, no code fences."
	defaultUpdatePrompt = "This handwritten note page was transcribed to Markdown before, " +
		"and the page has changed since. Produce the complete updated Markdown transcription " +
		"of the page as it is now, following the same rules: #..###### headings mirroring the " +
		"visual hierarchy, lists, emphasis; no commentary, no code fences. Wherever the page is " +
		"unchanged, reuse the previous transcription's wording, punctuation and line breaks " +
		"verbatim, so your output differs from it only where the page actually changed. " +
		"The previous transcription follows."
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
	// Archive is the default archive root, used when the -a/--archive flag is
	// absent (the flag wins). Meant for the XDG user config so a single archive
	// need not be passed on every invocation; a leading ~ is expanded by the
	// caller (cmd/snorg), not here.
	Archive  string   `yaml:"archive"`
	Provider Provider `yaml:"provider"`
	Analysis Analysis `yaml:"analysis"`
	Export   Export   `yaml:"export"`
	Ingest   Ingest   `yaml:"ingest"`
}

// Ingest configures the ingest command.
type Ingest struct {
	SVG SVGToggles `yaml:"svg"`
}

// SVGToggles switches the per-page SVG rewrites applied on ingest. The bool
// pointers distinguish "unset" (defaults to true) from an explicit false, which
// survives the config merge; all three off (with background: inline) writes the
// renderer's SVG byte-verbatim. Background is a mode enum (defaults to "extract"),
// and Colors optionally remaps the renderer's four pen shades.
type SVGToggles struct {
	Links      *bool  `yaml:"links"`      // bake note links as clickable overlays
	Navigation *bool  `yaml:"navigation"` // bake prev/next half-page zones
	Format     *bool  `yaml:"format"`     // reflow into diff-friendly multiline layout
	Background string `yaml:"background"` // extract | inline | blank | remove (default extract)

	// Colors remaps the renderer's four default pen shades to configured colors
	// (CSS name or hex, substituted verbatim into fill=). Keys: black, darkgray,
	// gray, white; any unset key keeps the default shade. Empty = no recolor.
	Colors map[string]string `yaml:"colors"`
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
	// APIKeyCommand is a shell command (run via `sh -c`) whose stdout is the API
	// key; consulted by ResolveAPIKey only when APIKey is empty.
	APIKeyCommand string `yaml:"api_key_command"`
	Model         string `yaml:"model"`
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

// Task is one prompt-driven step. UpdatePrompt is only meaningful on the content
// task: it replaces Prompt when a previous transcription exists (the previous
// content is appended to it, steering the model toward a minimal diff). The
// struct form (not a bare string) leaves room for future per-task overrides.
type Task struct {
	Prompt       string `yaml:"prompt"`
	UpdatePrompt string `yaml:"update_prompt"`
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
	if c.Analysis.Content.UpdatePrompt == "" {
		c.Analysis.Content.UpdatePrompt = defaultUpdatePrompt
	}
	if c.Analysis.Titles.Prompt == "" {
		c.Analysis.Titles.Prompt = defaultTitlePrompt
	}
	if c.Analysis.Links.Prompt == "" {
		c.Analysis.Links.Prompt = defaultLinkPrompt
	}
	for _, b := range []**bool{
		&c.Ingest.SVG.Links, &c.Ingest.SVG.Navigation, &c.Ingest.SVG.Format,
	} {
		if *b == nil {
			t := true
			*b = &t
		}
	}
	if c.Ingest.SVG.Background == "" {
		c.Ingest.SVG.Background = defaultBackgroundMode
	}
}

// ResolveAPIKey fills Provider.APIKey when it is empty, in precedence order:
// literal api_key > api_key_command stdout (trimmed) > $OPENAI_API_KEY. Running the
// command is deferred to here (not Load) so key-less commands like export never invoke it.
func (c *Config) ResolveAPIKey() error {
	if c.Provider.APIKey != "" {
		return nil
	}
	if cmd := strings.TrimSpace(c.Provider.APIKeyCommand); cmd != "" {
		out, err := exec.Command("sh", "-c", cmd).Output()
		if err != nil {
			if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
				return fmt.Errorf("provider.api_key_command: %w: %s", err, strings.TrimSpace(string(ee.Stderr)))
			}
			return fmt.Errorf("provider.api_key_command: %w", err)
		}
		c.Provider.APIKey = strings.TrimSpace(string(out))
		return nil
	}
	c.Provider.APIKey = os.Getenv(apiKeyEnv)
	return nil
}

// ValidateIngest checks the fields the ingest command needs: a recognized
// background mode (defaults applied by Load) and only known color keys.
func (c *Config) ValidateIngest() error {
	if !backgroundModes[c.Ingest.SVG.Background] {
		return fmt.Errorf("ingest.svg.background: unknown mode %q (want extract, inline, blank or remove)", c.Ingest.SVG.Background)
	}
	for k := range c.Ingest.SVG.Colors {
		switch k {
		case "black", "darkgray", "gray", "white":
		default:
			return fmt.Errorf("ingest.svg.colors: unknown shade %q (want black, darkgray, gray or white)", k)
		}
	}
	return nil
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
