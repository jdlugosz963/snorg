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

// Client is a handle on one archive plus its merged configuration. It is safe to
// reuse; its methods hold no cross-call state.
type Client struct {
	arch *archive.Archive
	cfg  *config.Config
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
	// Apply the ingest.svg toggles defensively: a config from LoadConfig has them
	// populated, but a hand-built Config leaves the bool pointers nil — keep the
	// default pipeline stage for any unset field.
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
	arch.SVG = svg
	return &Client{arch: arch, cfg: cfg}, nil
}

// ResolveOptions configures Resolve's archive-path and config-layer resolution,
// mirroring the snorg CLI's global flags.
type ResolveOptions struct {
	ArchivePath     string   // the -a flag; when empty, falls back to the config's archive: key
	ConfigFiles     []string // -c files, later overriding earlier
	NoUserConfig    bool     // skip the XDG user config
	NoArchiveConfig bool     // skip <archive>/config.yaml
}

// Resolve builds a Client the way the CLI does: it locates the archive root (the
// ArchivePath option, else the archive: key from the XDG user config and -c files,
// with a leading ~ expanded) and loads the merged config in increasing precedence
// (XDG user config → <archive>/config.yaml → -c files, later wins).
func Resolve(opts ResolveOptions) (*Client, error) {
	userPath := userConfigPath()
	archivePath := opts.ArchivePath
	if archivePath == "" {
		// The archive path is needed to locate the archive's own config, so it is
		// resolved from only the layers that do not depend on it.
		preCfg, err := config.Load(configPaths(userPath, "", opts.ConfigFiles, opts.NoUserConfig, true))
		if err != nil {
			return nil, err
		}
		archivePath = preCfg.Archive
	}
	if archivePath == "" {
		return nil, fmt.Errorf("no archive path: set ArchivePath or the archive: key in %s", userPath)
	}
	archivePath = expandHome(archivePath)

	cfg, err := config.Load(configPaths(userPath, archivePath, opts.ConfigFiles, opts.NoUserConfig, opts.NoArchiveConfig))
	if err != nil {
		return nil, err
	}
	return Open(archivePath, cfg)
}

// ArchivePath returns the client's archive root.
func (c *Client) ArchivePath() string { return c.arch.Root }

// Config returns the client's merged configuration.
func (c *Client) Config() *Config { return c.cfg }

// List returns the archived FILE_IDs, sorted.
func (c *Client) List() ([]string, error) { return retrieve.List(c.arch) }

// Query returns the pages matching pred (see the Predicate constructors and
// ParseFilter). Order follows the archive walk.
func (c *Client) Query(pred Predicate) ([]Match, error) { return query.Pages(c.arch, pred) }

// Retrieve assembles the given pages into a Result: the absolute archive root plus
// the pages grouped per owning note in placement order. An unknown PAGEID is an
// error.
func (c *Client) Retrieve(pageIDs []string) (*Result, error) { return retrieve.Get(c.arch, pageIDs) }

// ReadNote returns the raw note.json document for fileID.
func (c *Client) ReadNote(fileID string) (NoteDoc, error) { return c.arch.ReadNote(fileID) }

// ReadPage returns the raw <PAGEID>.json document.
func (c *Client) ReadPage(fileID, pageID string) (PageDoc, error) {
	return c.arch.ReadPage(fileID, pageID)
}

// ReadSVG returns a page's rendered SVG bytes.
func (c *Client) ReadSVG(fileID, pageID string) ([]byte, error) {
	return c.arch.ReadSVG(fileID, pageID)
}

// FindPage returns the FILE_ID that owns pageID (error if none or ambiguous).
func (c *Client) FindPage(pageID string) (string, error) { return c.arch.FindPage(pageID) }

// NoteFiles returns the *.note files under root, recursively and sorted — the input
// for Ingest when registering a directory.
func NoteFiles(root string) ([]string, error) { return ingest.NoteFiles(root) }

// Ingest registers each note path into the archive, running the config's SVG
// pipeline; jobs caps concurrent notes (0 = number of CPUs). Results preserve input
// order; a note's failure is reported in its IngestResult.Err without aborting the
// batch.
func (c *Client) Ingest(paths []string, jobs int) ([]IngestResult, error) {
	if err := c.cfg.ValidateIngest(); err != nil {
		return nil, err
	}
	return ingest.RunMany(sntool.New(), c.arch, paths, jobs), nil
}

// Export retrieves the given pages and renders them through the config's export
// template (export.template must be set). Use RenderTemplate to render an
// already-retrieved Result through an arbitrary template.
func (c *Client) Export(pageIDs []string) (string, error) {
	if c.cfg.Export.Template == "" {
		return "", fmt.Errorf("export.template is required")
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

// ServeHandler assembles the given pages and returns the built-in HTTP viewer as an
// http.Handler — grouped by note, or one flat gallery when flat. An empty pageIDs
// serves the whole archive. Binding a listener is left to the caller.
func (c *Client) ServeHandler(pageIDs []string, flat bool) (http.Handler, error) {
	if len(pageIDs) == 0 {
		matches, err := query.Pages(c.arch, query.All)
		if err != nil {
			return nil, err
		}
		pageIDs = make([]string, len(matches))
		for i, m := range matches {
			pageIDs[i] = m.PageID
		}
	}
	res, err := retrieve.Get(c.arch, pageIDs)
	if err != nil {
		return nil, err
	}
	return serve.Handler(c.arch, res.Notes, flat), nil
}

// Migrate upgrades the given pages (and their owning notes) to the current schema
// version; an empty pageIDs migrates the whole archive (see MigrateAll). Per-file
// errors are reported in the results, not returned.
func (c *Client) Migrate(pageIDs []string) ([]MigrateResult, error) {
	if len(pageIDs) == 0 {
		return c.arch.MigrateAll()
	}
	return c.arch.MigratePages(pageIDs)
}

// MigrateAll upgrades every note and page in the archive to the current schema.
func (c *Client) MigrateAll() ([]MigrateResult, error) { return c.arch.MigrateAll() }
