// Package archive owns the on-disk plaintext layout of organized notes:
//
//	<root>/<FILE_ID>/
//	    note.json          file metadata + ordered page placement
//	    <PAGEID>.json      per-page deterministic metadata
//	    <PAGEID>.svg       per-page rendered vector
//	    backgrounds/
//	        <sha256>.png   page backgrounds, content-addressed, deduped per note
//
// The layout is VCS-friendly (stable filenames, indented JSON) and is the stable
// contract retrieval commands and user export scripts build on. Page SVGs are not
// self-contained: each references its background by relative href into the sibling
// backgrounds/ folder, so that folder must travel with the note.
package archive

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jdlugosz963/snorg/internal/snote"
)

// Archive is a rooted note store.
type Archive struct {
	Root string
	SVG  SVGPipeline
}

// SVGPipeline configures the rewrites Write applies to each rendered page SVG.
// The bool stages default to on; Background is a mode (default extract) and Colors
// optionally remaps the renderer's pen shades. Turning stages off / setting
// background to inline yields the renderer's output byte-verbatim. Any change here
// changes SVG bytes, so the next Write rewrites every page SVG once — harmless
// churn; none of it dirties the analyze fingerprint (path geometry only).
type SVGPipeline struct {
	Links      bool              // bake note links as clickable overlays (injectLinks)
	Navigation bool              // bake prev/next half-page zones (injectNav)
	Format     bool              // reflow into diff-friendly multiline layout (formatSVG)
	Background BackgroundMode    // background treatment (see BackgroundMode)
	Colors     map[string]string // remap the four default pen shades (see recolor); nil = none
}

// DefaultSVGPipeline is the full pipeline: every overlay on, background extracted,
// colors unchanged.
func DefaultSVGPipeline() SVGPipeline {
	return SVGPipeline{Links: true, Navigation: true, Format: true, Background: BackgroundExtract}
}

// New returns an Archive rooted at root with the default SVG pipeline.
func New(root string) *Archive { return &Archive{Root: root, SVG: DefaultSVGPipeline()} }

// Write registers a note keyed by its file id, reconciling the note's directory
// in place rather than rebuilding it. Pages no longer present are pruned (all of
// their <PAGEID>.* files, including derived analyses); note.json and per-page
// files are written only when their content changed. Crucially, other <PAGEID>.*
// artifacts of pages that remain are left untouched, so expensive per-page
// analyses survive re-ingest. svgs maps page id to rendered SVG bytes; each one
// runs through the a.SVG pipeline before writing: background extraction into the
// note's backgrounds/ subfolder (extractBackground), prev/next navigation zones
// (injectNav), clickable note links (injectLinks) and the diff-friendly reflow
// (formatSVG) — each stage individually toggleable, all on by default.
func (a *Archive) Write(n *snote.Note, svgs map[string][]byte) error {
	if n.FileID == "" {
		return fmt.Errorf("note has empty file id")
	}
	dir := filepath.Join(a.Root, n.FileID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}

	current := make(map[string]bool, len(n.Pages))
	for _, p := range n.Pages {
		if p.ID == "" {
			return fmt.Errorf("page %d has empty id", p.Number)
		}
		current[p.ID] = true
	}

	// Prune pages that disappeared from the note (with all their artifacts).
	existing, err := archivedPageIDs(dir)
	if err != nil {
		return err
	}
	for id := range existing {
		if !current[id] {
			if err := removePageFiles(dir, id); err != nil {
				return err
			}
		}
	}

	if err := writeJSONIfChanged(filepath.Join(dir, "note.json"), noteDoc(n)); err != nil {
		return err
	}
	for i, p := range n.Pages {
		pd := pageDoc(p)
		// Re-ingest must not discard a page's derived AI analysis: carry it over
		// from the existing page JSON (ingest itself never produces one).
		if old, err := a.ReadPage(n.FileID, p.ID); err == nil {
			pd.Analysis = old.Analysis
			carryRegionAnalyses(&pd, old)
		}
		if err := writeJSONIfChanged(filepath.Join(dir, p.ID+".json"), pd); err != nil {
			return err
		}
		if svg, ok := svgs[p.ID]; ok {
			if a.SVG.Background == BackgroundExtract {
				rewritten, img, name, hasBG := extractBackground(svg)
				if hasBG {
					if err := writeBackground(dir, name, img); err != nil {
						return fmt.Errorf("write background for page %s: %w", p.ID, err)
					}
					svg = rewritten
				}
			} else {
				svg = applyBackground(svg, a.SVG.Background)
			}
			svg = recolor(svg, a.SVG.Colors)
			if a.SVG.Navigation {
				// Nav before links: the later link overlays win hit-testing.
				var prevID, nextID string
				if i > 0 {
					prevID = n.Pages[i-1].ID
				}
				if i < len(n.Pages)-1 {
					nextID = n.Pages[i+1].ID
				}
				svg = injectNav(svg, prevID, nextID)
			}
			if a.SVG.Links {
				svg = a.injectLinks(svg, n.FileID, current, p.Links)
			}
			if a.SVG.Format {
				svg = formatSVG(svg)
			}
			if err := writeFileIfChanged(filepath.Join(dir, p.ID+".svg"), svg); err != nil {
				return fmt.Errorf("write svg for page %s: %w", p.ID, err)
			}
		}
	}
	return nil
}

// carryRegionAnalyses copies per-title/per-link analyses from old into pd,
// matched by exact rect (the region's identity: same rect = same handwriting
// underneath, even if the link's target changed). Regions that moved or resized
// drop their analysis and get re-transcribed by the next analyze run. Duplicate
// rects are consumed in order, each old analysis at most once.
func carryRegionAnalyses(pd *PageDoc, old PageDoc) {
	titles := map[snote.Rect][]*TitleAnalysis{}
	for _, t := range old.Titles {
		titles[t.Rect] = append(titles[t.Rect], t.Analysis)
	}
	for i, t := range pd.Titles {
		if q := titles[t.Rect]; len(q) > 0 {
			pd.Titles[i].Analysis = q[0]
			titles[t.Rect] = q[1:]
		}
	}
	links := map[snote.Rect][]*LinkAnalysis{}
	for _, l := range old.Links {
		links[l.Rect] = append(links[l.Rect], l.Analysis)
	}
	for i, l := range pd.Links {
		if q := links[l.Rect]; len(q) > 0 {
			pd.Links[i].Analysis = q[0]
			links[l.Rect] = q[1:]
		}
	}
}

// archivedPageIDs returns the set of PAGEIDs already present in dir, derived from
// the leading "P..." token of every <PAGEID>.* filename (page files only).
func archivedPageIDs(dir string) (map[string]bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}
	ids := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, "P") {
			continue
		}
		if i := strings.IndexByte(name, '.'); i > 0 {
			ids[name[:i]] = true
		}
	}
	return ids, nil
}

// removePageFiles deletes every <pageID>.* file in dir.
func removePageFiles(dir, pageID string) error {
	matches, err := filepath.Glob(filepath.Join(dir, pageID+".*"))
	if err != nil {
		return err
	}
	for _, m := range matches {
		if err := os.Remove(m); err != nil {
			return fmt.Errorf("remove %s: %w", m, err)
		}
	}
	return nil
}

// writeBackground stores a content-addressed page background under the note's
// <noteDir>/backgrounds/ subfolder. The name is a content hash, so identical
// backgrounds (every page of a note shares the same template) collapse to one
// file and re-ingest writes nothing new.
func writeBackground(noteDir, name string, img []byte) error {
	dir := filepath.Join(noteDir, backgroundsDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	return writeFileIfChanged(filepath.Join(dir, name), img)
}

func writeJSONIfChanged(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", filepath.Base(path), err)
	}
	b = append(b, '\n')
	return writeFileIfChanged(path, b)
}

// writeFileIfChanged writes data only when it differs from what is on disk, so
// unchanged files keep their bytes (and mtime) and produce no VCS churn.
func writeFileIfChanged(path string, data []byte) error {
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, data) {
		return nil
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
