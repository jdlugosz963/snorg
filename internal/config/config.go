// Package config loads snorg's runtime configuration: provider credentials,
// analysis prompts, ingest SVG toggles, the export template and template regions.
// Configuration is split across one or more YAML files that are deep-merged (later
// files win, sequences concatenate), so secrets (api_key) can live in a separate,
// gitignored file from the committed configuration. A file may also pull in other
// files via the include: key, whose contents override the including file's own keys
// (see Load).
//
// Its allowed external dependencies are yaml.v3 and internal/snote (snote.Rect, a
// pure domain type used by template boxes).
package config

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jdlugosz963/snorg/internal/snote"
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
	// absent (the flag wins), so a single archive need not be passed on every
	// invocation. Load resolves it by context like include:/image: paths: a
	// relative value against the directory of the file that declared it, a leading
	// ~ expanded, an absolute value as-is.
	Archive  string   `yaml:"archive"`
	Provider Provider `yaml:"provider"`
	Analysis Analysis `yaml:"analysis"`
	Export   Export   `yaml:"export"`
	Ingest   Ingest   `yaml:"ingest"`
	// Templates lists the archive's template regions (see docs/templates.md),
	// folded into the merged config so they layer like every other section: the
	// lists from every file add up (lower layer first), so one file can declare a
	// template and an included one add more. Each entry's Image is resolved to an
	// absolute path by Load, relative to the config file that declared it;
	// pkg/snorg bridges these specs to the archive, which hashes each image to
	// match it against a page's background.
	Templates []TemplateSpec `yaml:"templates"`
}

// Clone returns a deep copy: every map, slice and pointer field is duplicated, so
// mutating the copy can never reach the original. Any reference-typed field added to
// Config must be duplicated here (TestCloneIsDeep fails otherwise).
func (c *Config) Clone() *Config {
	cp := *c
	if c.Analysis.Fields != nil {
		cp.Analysis.Fields = make(map[string]Task, len(c.Analysis.Fields))
		for k, v := range c.Analysis.Fields {
			cp.Analysis.Fields[k] = v
		}
	}
	if c.Ingest.SVG.Colors != nil {
		cp.Ingest.SVG.Colors = make(map[string]string, len(c.Ingest.SVG.Colors))
		for k, v := range c.Ingest.SVG.Colors {
			cp.Ingest.SVG.Colors[k] = v
		}
	}
	cp.Ingest.SVG.Links = cloneBool(c.Ingest.SVG.Links)
	cp.Ingest.SVG.Navigation = cloneBool(c.Ingest.SVG.Navigation)
	cp.Ingest.SVG.Format = cloneBool(c.Ingest.SVG.Format)
	if c.Templates != nil {
		cp.Templates = make([]TemplateSpec, len(c.Templates))
		for i, t := range c.Templates {
			cp.Templates[i] = t
			if t.Boxes != nil {
				cp.Templates[i].Boxes = append([]Box(nil), t.Boxes...)
			}
		}
	}
	return &cp
}

// cloneBool duplicates an SVGToggles pointer, preserving nil (= unset).
func cloneBool(p *bool) *bool {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

// TemplateSpec is one template: a background Image (an absolute path after Load
// resolves it against the declaring file's directory) and its boxes in author
// order. The image hashing, validation and background matching live in
// internal/archive; this is the raw parsed form.
type TemplateSpec struct {
	Image string `yaml:"image"`
	Boxes []Box  `yaml:"boxes"`
}

// Box is one region of a template: a stable id, a renameable label, a pixel-space
// rect (snote.Rect, the 1920x2560 page space), whether analyze transcribes it and
// an optional per-box prompt.
type Box struct {
	ID      string     `yaml:"id"`
	Label   string     `yaml:"label"`
	Rect    snote.Rect `yaml:"rect"`
	Analyze bool       `yaml:"analyze"`
	Prompt  string     `yaml:"prompt"`
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

// Load reads each path, deep-merges them (later paths override earlier ones; a
// sequence such as templates: is concatenated, lower layer first, rather than
// replaced), decodes the result into a Config and fills in defaults. It is an error
// for a file to be unreadable or malformed. Load does not enforce required fields;
// callers run the validation for the section they need (e.g. ValidateProvider).
//
// A file may carry an include: list of other config paths (relative to that file's
// directory, ~ expanded). Includes are merged *over* the including file's own keys
// ("above includer") and expanded recursively — so the effective low-to-high order
// for a file is: its body, then each include in listed order, with a later include
// (and any file's own includes) winning. The top-level paths still layer in the
// order given, later winning. An include cycle is an error.
//
// Each template's image: path is resolved to an absolute path relative to the file
// that declared it, before the merge, so every layer's entries keep their own
// provenance once the templates: lists from all of them are concatenated.
func Load(paths []string) (*Config, error) {
	merged := map[string]any{}
	for _, p := range paths {
		if err := loadInto(merged, p, map[string]bool{}); err != nil {
			return nil, err
		}
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

// loadInto merges the config at path into dst, then merges each of its includes
// over it (recursively). stack holds the absolute paths on the current include
// chain, so a cycle is caught rather than looping forever.
func loadInto(dst map[string]any, path string, stack map[string]bool) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	if stack[abs] {
		return fmt.Errorf("config include cycle: %s", abs)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read config %s: %w", path, err)
	}
	var m map[string]any
	if err := yaml.Unmarshal(raw, &m); err != nil {
		return fmt.Errorf("parse config %s: %w", path, err)
	}

	dir := filepath.Dir(path)
	includes, err := popIncludes(m, dir)
	if err != nil {
		return fmt.Errorf("config %s: %w", path, err)
	}
	if err := resolveTemplateImages(m, dir); err != nil {
		return fmt.Errorf("config %s: %w", path, err)
	}
	if err := resolveArchivePath(m, dir); err != nil {
		return fmt.Errorf("config %s: %w", path, err)
	}

	// Body first (lower precedence), then includes over it (above-includer).
	deepMerge(dst, m)
	stack[abs] = true
	defer delete(stack, abs)
	for _, inc := range includes {
		if err := loadInto(dst, inc, stack); err != nil {
			return err
		}
	}
	return nil
}

// popIncludes removes the include: key from m and returns the listed paths, each
// resolved (~ expanded, made absolute relative to dir when not already absolute).
func popIncludes(m map[string]any, dir string) ([]string, error) {
	v, ok := m["include"]
	if !ok {
		return nil, nil
	}
	delete(m, "include")
	items, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("include must be a list of paths")
	}
	out := make([]string, 0, len(items))
	for _, it := range items {
		s, ok := it.(string)
		if !ok {
			return nil, fmt.Errorf("include entries must be strings")
		}
		out = append(out, resolvePath(s, dir))
	}
	return out, nil
}

// resolveTemplateImages rewrites each templates[].image in m to an absolute path
// relative to dir (the declaring file's directory), so it stays correct after the
// merge regardless of which file's templates list wins.
func resolveTemplateImages(m map[string]any, dir string) error {
	v, ok := m["templates"]
	if !ok {
		return nil
	}
	list, ok := v.([]any)
	if !ok {
		return fmt.Errorf("templates must be a list")
	}
	for _, it := range list {
		tm, ok := it.(map[string]any)
		if !ok {
			return fmt.Errorf("each template must be a mapping")
		}
		img, ok := tm["image"]
		if !ok {
			continue
		}
		s, ok := img.(string)
		if !ok {
			return fmt.Errorf("template image must be a string")
		}
		tm["image"] = resolvePath(s, dir)
	}
	return nil
}

// resolveArchivePath rewrites a relative top-level archive: value to a path
// relative to dir (the declaring file's directory), so the default archive root is
// resolved by context like include: and template image: paths — relative to the
// file that set it, absolute/~ values used as-is. Done before the merge so the
// resolved path survives a later file overwriting the scalar.
func resolveArchivePath(m map[string]any, dir string) error {
	v, ok := m["archive"]
	if !ok {
		return nil
	}
	s, ok := v.(string)
	if !ok {
		return fmt.Errorf("archive must be a string")
	}
	m["archive"] = resolvePath(s, dir)
	return nil
}

// resolvePath expands a leading ~ and makes a relative path absolute against dir.
func resolvePath(p, dir string) string {
	p = ExpandHome(p)
	if p == "" || filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(dir, p)
}

// ExpandHome expands a leading ~ or ~/ to the user's home directory (YAML/Go do
// not do this like the shell). Returns p unchanged when it has no ~ prefix or the
// home dir can't be resolved.
func ExpandHome(p string) string {
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	return filepath.Join(home, strings.TrimPrefix(p[1:], "/"))
}

// deepMerge recursively merges src into dst: nested maps merge per key, sequences
// concatenate (dst's items first, src's appended after), and every other value
// (scalars) is overwritten by src. So a list setting adds up across layers rather
// than the highest layer replacing it; the concatenation builds a fresh slice, so
// neither parsed document is aliased.
func deepMerge(dst, src map[string]any) {
	for k, sv := range src {
		switch s := sv.(type) {
		case map[string]any:
			if dm, ok := dst[k].(map[string]any); ok {
				deepMerge(dm, s)
				continue
			}
		case []any:
			if dl, ok := dst[k].([]any); ok {
				dst[k] = append(append(make([]any, 0, len(dl)+len(s)), dl...), s...)
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
