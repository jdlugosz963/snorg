// Package snorg is the public Go API for the supernote-organizer (SNORG): a single
// facade over an archive of ingested .note files. A Client bundles an archive root
// with merged configuration and exposes snorg's capabilities — ingest, list, query,
// retrieve, analyze, export, serve, migrate and programmatic page edits. The snorg
// CLI (cmd/snorg) is itself a consumer of this package.
//
// Construct a Client with Open (an explicit archive root plus an optional config) or
// Resolve (the CLI's config-layering and archive-path fallback). The internal/*
// packages that implement each capability are not importable from outside the
// module; every type this package returns is re-exported here (see types.go).
package snorg

import (
	"context"
	"fmt"
	"net/http"

	"github.com/jdlugosz963/snorg/internal/archive"
	"github.com/jdlugosz963/snorg/internal/config"
	"github.com/jdlugosz963/snorg/internal/export"
	"github.com/jdlugosz963/snorg/internal/ingest"
	"github.com/jdlugosz963/snorg/internal/query"
	"github.com/jdlugosz963/snorg/internal/retrieve"
	"github.com/jdlugosz963/snorg/internal/serve"
	"github.com/jdlugosz963/snorg/internal/snote/sntool"
)

// Client is a handle on one archive plus its merged configuration. It is meant to be
// reused; the one piece of cross-call state is the API key NewProvider resolves, cached
// so a shell api_key_command runs once per client (which also makes a Client unsafe to
// share across goroutines).
type Client struct {
	arch *archive.Archive
	cfg  *config.Config
	tmpl *Templates
	// apiKey is the resolved provider credential, deliberately kept off cfg: Config
	// hands that out, and a secret does not belong in a struct callers can marshal.
	apiKey string
}

// LoadConfig loads and deep-merges the YAML config files (later paths override
// earlier ones) and applies snorg's built-in defaults. Pass the result to Open, or
// pass nil to Open for defaults only.
func LoadConfig(paths []string) (*Config, error) { return config.Load(paths) }

// Open returns a Client for the archive rooted at archivePath. cfg may be nil, in
// which case built-in defaults are used (equivalent to LoadConfig(nil)). The
// config's ingest.svg toggles are applied to the archive's SVG pipeline, so Ingest
// honors them; unset toggles keep the default pipeline. cfg should come from
// LoadConfig (or nil) so its defaults are populated.
func Open(archivePath string, cfg *Config) (*Client, error) {
	if archivePath == "" {
		return nil, fmt.Errorf("archive path is empty")
	}
	if cfg == nil {
		var err error
		if cfg, err = config.Load(nil); err != nil {
			return nil, err
		}
	}
	arch := archive.New(archivePath)
	arch.SVG = svgPipeline(cfg)
	// Bridge the config's template specs (image paths already resolved to absolute
	// by config.Load) into the archive, which hashes each image to match a page's
	// background. A hand-built Config with no templates leaves the feature inert.
	arch.SetTemplateSpecs(templateSpecs(cfg))
	// Resolve them right away: a templates: section that names a missing image or
	// invalid boxes is a broken config, and it should fail here rather than half a
	// command later — which is also why no read path (a query predicate included)
	// has to carry a template-resolution error.
	tmpl, err := arch.Templates()
	if err != nil {
		return nil, err
	}
	return &Client{arch: arch, cfg: cfg, tmpl: tmpl}, nil
}

// svgPipeline builds the archive's SVG pipeline from the config's ingest.svg
// toggles. A hand-built Config leaves the bool pointers nil, so any unset field
// keeps the default stage.
func svgPipeline(cfg *config.Config) archive.SVGPipeline {
	svg := archive.DefaultSVGPipeline()
	s := cfg.Ingest.SVG
	if s.Links != nil {
		svg.Links = *s.Links
	}
	if s.Navigation != nil {
		svg.Navigation = *s.Navigation
	}
	if s.Format != nil {
		svg.Format = *s.Format
	}
	if s.Background != "" {
		svg.Background = archive.BackgroundMode(s.Background)
	}
	if s.Colors != nil {
		svg.Colors = s.Colors
	}
	return svg
}

// templateSpecs converts the config's template section into the archive's raw spec
// form (identical fields; the two packages stay mutually independent, bridged here).
func templateSpecs(cfg *config.Config) []archive.TemplateSpec {
	if len(cfg.Templates) == 0 {
		return nil
	}
	out := make([]archive.TemplateSpec, len(cfg.Templates))
	for i, t := range cfg.Templates {
		boxes := make([]archive.Box, len(t.Boxes))
		for j, b := range t.Boxes {
			boxes[j] = archive.Box{ID: b.ID, Label: b.Label, Rect: b.Rect, Analyze: b.Analyze, Prompt: b.Prompt}
		}
		out[i] = archive.TemplateSpec{Image: t.Image, Boxes: boxes}
	}
	return out
}

// ResolveOptions configures Resolve's archive-path and config-layer resolution,
// mirroring the snorg CLI's global flags.
type ResolveOptions struct {
	ArchivePath  string   // the -a flag; when empty, falls back to the config's archive: key
	ConfigFiles  []string // -c files, later overriding earlier
	NoUserConfig bool     // skip the XDG user config
}

// Resolve builds a Client the way the CLI does: it loads the merged config in
// increasing precedence (XDG user config → -c files, later wins) in a single pass,
// then locates the archive root — the ArchivePath option (CWD-relative, like any
// CLI path), else the archive: key from that merged config (resolved by config.Load
// relative to the file that declared it). A leading ~ is expanded either way.
func Resolve(opts ResolveOptions) (*Client, error) {
	userPath := userConfigPath()
	cfg, err := config.Load(configPaths(userPath, opts.ConfigFiles, opts.NoUserConfig))
	if err != nil {
		return nil, err
	}
	archivePath := opts.ArchivePath
	if archivePath == "" {
		archivePath = cfg.Archive
	}
	if archivePath == "" {
		where := userPath
		if where == "" {
			where = "the user config"
		}
		return nil, fmt.Errorf("no archive path: set ArchivePath or the archive: key in %s", where)
	}
	archivePath = config.ExpandHome(archivePath)
	return Open(archivePath, cfg)
}

// ArchivePath returns the client's archive root.
func (c *Client) ArchivePath() string { return c.arch.Root }

// Config returns a snapshot of the client's merged configuration. Mutating it has no
// effect on the client — configure through LoadConfig/Open.
func (c *Client) Config() *Config { return c.cfg.Clone() }

// List returns the archived FILE_IDs, sorted.
func (c *Client) List() ([]string, error) { return retrieve.List(c.arch) }

// Query returns the pages matching pred (see the Match* constructors and
// ParseQuery), each with the note.json and <PAGEID>.json documents the walk read.
// Order follows the archive walk: List order, then note.json page order. The first
// error a predicate returns aborts the walk and is returned, naming its page, so the
// result set never mixes matched pages with pages that were never read.
func (c *Client) Query(pred Predicate) ([]Match, error) {
	return query.Pages(c.arch, func(nd archive.NoteDoc, pd archive.PageDoc) (bool, error) {
		ok, err := pred(Page{Note: nd, Doc: pd, c: c})
		if err != nil {
			return false, fmt.Errorf("page %s: %w", pd.PageID, err)
		}
		return ok, nil
	})
}

// Retrieve assembles the given pages into a Result: the absolute archive root plus
// the pages grouped per owning note in placement order. An unknown PAGEID is an
// error.
func (c *Client) Retrieve(pageIDs []string) (*Result, error) { return retrieve.Get(c.arch, pageIDs) }

// ReadNote returns the raw note.json document for fileID.
func (c *Client) ReadNote(fileID string) (NoteDoc, error) { return c.arch.ReadNote(fileID) }

// ReadPage returns the raw <PAGEID>.json document. Like every single-page reader it
// takes the PAGEID alone (it names one page in one note) and wraps ErrNotFound for
// an unknown one.
func (c *Client) ReadPage(pageID string) (PageDoc, error) {
	fileID, err := c.arch.FindPage(pageID)
	if err != nil {
		return PageDoc{}, err
	}
	return c.arch.ReadPage(fileID, pageID)
}

// ReadSVG returns a page's rendered SVG bytes.
func (c *Client) ReadSVG(pageID string) ([]byte, error) {
	fileID, err := c.arch.FindPage(pageID)
	if err != nil {
		return nil, err
	}
	return c.arch.ReadSVG(fileID, pageID)
}

// ReadAnalysis returns a page's transcription — the <PAGEID>.md sidecar, AI-produced
// or hand-written. A never-analyzed page reads as empty, not as an error.
func (c *Client) ReadAnalysis(pageID string) (string, error) {
	fileID, err := c.arch.FindPage(pageID)
	if err != nil {
		return "", err
	}
	return c.arch.ReadAnalysisMD(fileID, pageID)
}

// Templates returns the template set built from the config's templates: section,
// against which a page's BackgroundHash is matched. Open resolved it, so a broken
// templates: section already failed there.
func (c *Client) Templates() *Templates { return c.tmpl }

// FindPage returns the FILE_ID that owns pageID (error if none or ambiguous).
func (c *Client) FindPage(pageID string) (string, error) { return c.arch.FindPage(pageID) }

// NoteFiles returns the *.note files under root, recursively and sorted — the input
// for Ingest when registering a directory.
func NoteFiles(root string) ([]string, error) { return ingest.NoteFiles(root) }

// IngestOptions tunes a batch ingest.
type IngestOptions struct {
	// OnResult, when set, is called with each note's result as it lands, so a
	// caller can report progress instead of waiting for the whole batch. Results
	// fire in the returned slice's order, on the calling goroutine.
	OnResult func(IngestResult)
}

// Ingest registers each note path into the archive, running the config's SVG
// pipeline. Results preserve input order; a note's failure is reported in its
// IngestResult.Err without aborting the batch.
//
// A cancelled ctx stops the batch: the results so far are returned with ctx.Err().
func (c *Client) Ingest(ctx context.Context, paths []string, opts IngestOptions) ([]IngestResult, error) {
	if err := c.cfg.ValidateIngest(); err != nil {
		return nil, err
	}
	return ingest.RunMany(ctx, sntool.New(), c.arch, paths, ingest.Options{OnResult: opts.OnResult})
}

// Export retrieves the given pages and renders them through the config's export
// template (export.template must be set). Use RenderTemplate to render an
// already-retrieved Result through an arbitrary template.
func (c *Client) Export(pageIDs []string) (string, error) {
	if c.cfg.Export.Template == "" {
		return "", ErrNoExportTemplate
	}
	res, err := retrieve.Get(c.arch, pageIDs)
	if err != nil {
		return "", err
	}
	return export.Render(res, c.cfg.Export.Template)
}

// RenderTemplate renders a Result through a pongo2/Jinja2 template (see docs/config).
func RenderTemplate(res *Result, template string) (string, error) {
	return export.Render(res, template)
}

// ServeOptions tunes the built-in viewer.
type ServeOptions struct {
	// Flat serves one gallery of all pages instead of grouping them by note.
	Flat bool
}

// ServeHandler returns the built-in HTTP viewer over an already-retrieved Result
// (see Retrieve) as an http.Handler. Binding a listener is left to the caller.
func (c *Client) ServeHandler(res *Result, opts ServeOptions) http.Handler {
	return serve.Handler(c.arch, res.Notes, opts.Flat)
}

// MigrateOptions tunes a migration batch.
type MigrateOptions struct {
	// OnResult, when set, is called with each file's result as it lands, in the
	// returned slice's order (the walk is sequential).
	OnResult func(MigrateResult)
}

// Migrate upgrades the given pages (and their owning notes) to the current schema
// version; an empty pageIDs migrates the whole archive (see MigrateAll). Per-file
// errors are reported in the results, not returned.
//
// A cancelled ctx stops the batch: the results so far are returned with ctx.Err().
func (c *Client) Migrate(ctx context.Context, pageIDs []string, opts MigrateOptions) ([]MigrateResult, error) {
	if len(pageIDs) == 0 {
		return c.MigrateAll(ctx, opts)
	}
	return c.arch.MigratePages(ctx, pageIDs, archive.MigrateOptions{OnResult: opts.OnResult})
}

// MigrateAll upgrades every note and page in the archive to the current schema; ctx
// cancels it as in Migrate.
func (c *Client) MigrateAll(ctx context.Context, opts MigrateOptions) ([]MigrateResult, error) {
	return c.arch.MigrateAll(ctx, archive.MigrateOptions{OnResult: opts.OnResult})
}
