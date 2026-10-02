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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/jdlugosz963/snorg/internal/snote"
)

// Archive is a rooted note store.
type Archive struct {
	Root string
	SVG  SVGPipeline
	// Now is the clock the page change stamps read (see stamp.go); nil = time.Now.
	Now func() time.Time

	// templateSpecs are the template regions injected from the merged config (via
	// SetTemplateSpecs); templates is the resolved, image-hashed set Templates()
	// builds from them and caches. Nil specs = the template feature is inert.
	templateSpecs []TemplateSpec
	templates     *Templates
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

// WriteReport is what one Write changed on disk: whether note.json was rewritten,
// the pages that were newly written or changed (Pages), the ids of pages pruned
// because they left the note, and the other notes this write had to touch because a
// page moved out of them. It is the incremental reconcile made observable — an
// all-false/empty report means the note was already current.
type WriteReport struct {
	FileID      string
	NoteChanged bool
	Pages       []PageWriteReport
	// Pruned are the pages that left this note, sorted.
	Pruned []string
	// Parked are the pruned pages whose state went to the orphan store rather than
	// being deleted, sorted — a subset of Pruned (see orphan.go).
	Parked []string
	// Repaired are the FILE_IDs of other notes whose note.json this write rewrote
	// because a page moved out of them into this one, sorted.
	Repaired []string
}

// PageWriteReport is one page's change detail from Write. It is present only for a
// page that was new or whose bytes changed; a page written unchanged produces no
// entry. New means no page doc for it existed anywhere in the archive before this
// write — not merely in this note's directory — so it is what tells a caller the page
// still needs transcribing. DroppedRegions lists title/link analyses that were not
// carried forward because their rect moved (empty on a first ingest, where there is
// nothing to carry).
type PageWriteReport struct {
	PageID string
	New    bool
	// AdoptedFrom names where this page's state came from when the page moved into
	// this note on the device, sorted; the orphan store appears as "orphans". The
	// first entry supplied the carried analysis/tags/transcription, any further one
	// was a duplicate copy this write purged. Mutually exclusive with New.
	AdoptedFrom       []string
	JSONChanged       bool
	SVGChanged        bool
	BackgroundChanged bool
	DroppedRegions    []DroppedRegion
}

// DroppedRegion identifies a per-region (title/link) analysis that re-ingest could
// not carry forward: the old region carried an analysis but its rect no longer
// matches any region in the re-ingested page, so the analysis is dropped and the
// region will be re-transcribed by the next analyze run.
type DroppedRegion struct {
	Kind string // "title" | "link"
	Rect snote.Rect
}

// Write registers a note keyed by its file id, reconciling the note's directory
// in place rather than rebuilding it. Pages no longer present are pruned — their
// reclaimable state parked in the orphan store, the rest deleted (orphan.go) — and a
// page the note has gained that currently lives elsewhere in the archive is adopted
// from there rather than duplicated, with its old note repaired (adopt.go), so a
// PAGEID always names exactly one page in exactly one note. note.json and per-page
// files are written only when their content changed. Crucially, other <PAGEID>.*
// artifacts of pages that remain are left untouched, so expensive per-page
// analyses survive re-ingest. svgs maps page id to rendered SVG bytes; each one
// runs through the a.SVG pipeline before writing: background extraction into the
// note's backgrounds/ subfolder (extractBackground), prev/next navigation zones
// (injectNav), clickable note links (injectLinks) and the diff-friendly reflow
// (formatSVG) — each stage individually toggleable, all on by default.
func (a *Archive) Write(n *snote.Note, svgs map[string][]byte) (*WriteReport, error) {
	if n.FileID == "" {
		return nil, fmt.Errorf("note has empty file id")
	}
	dir := filepath.Join(a.Root, n.FileID)
	report := &WriteReport{FileID: n.FileID}

	current := make(map[string]bool, len(n.Pages))
	for _, p := range n.Pages {
		if p.ID == "" {
			return nil, fmt.Errorf("page %d has empty id", p.Number)
		}
		current[p.ID] = true
	}

	// Preflight the note and its kept pages through the schema-gated readers before
	// any write, so a stale-schema file aborts (ErrSchemaVersion) with the archive
	// untouched rather than after note.json is already rewritten. The docs it returns
	// carry the snorg-managed state (tags, analyses) forward.
	oldNote, oldPages, err := a.preflight(n.FileID, n.Pages)
	if err != nil {
		return nil, err
	}
	// The other half of the preflight: pages this note has gained that currently live
	// under another note or in the orphan store. Also gated, also before any write.
	foreign, err := a.locateForeign(n.FileID, n.Pages, oldPages)
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create %s: %w", dir, err)
	}

	// Move in every page that moved here on the device, before the prune below reads
	// the directory. A page this note already owns keeps its own state; the copy
	// elsewhere is purged either way — this note is the source of truth for the pages
	// it claims.
	adopted := make(map[string][]string, len(foreign.Origins))
	for _, id := range sortedKeys(foreign.Origins) {
		o := foreign.Origins[id]
		_, keepOwn := oldPages[id]
		repaired, err := a.adopt(dir, id, o, foreign.Notes, keepOwn)
		if err != nil {
			return nil, err
		}
		report.Repaired = append(report.Repaired, repaired...)
		if !keepOwn {
			oldPages[id] = o.Doc
			adopted[id] = o.Donors
		}
	}
	sort.Strings(report.Repaired)
	report.Repaired = slices.Compact(report.Repaired)
	a.dropOrphanDirIfEmpty()

	// Prune pages that disappeared from the note, parking what cannot be rebuilt.
	existing, err := archivedPageIDs(dir)
	if err != nil {
		return nil, err
	}
	for id := range existing {
		if !current[id] {
			parked, err := a.prunePage(n.FileID, id)
			if err != nil {
				return nil, err
			}
			report.Pruned = append(report.Pruned, id)
			if parked {
				report.Parked = append(report.Parked, id)
			}
		}
	}
	sort.Strings(report.Pruned)
	sort.Strings(report.Parked)

	// noteDoc rebuilds note.json from the .note, so the note's snorg-managed tags —
	// which the .note knows nothing about — must be carried over from the preflight
	// doc, exactly like a page's tags/analysis below.
	nd := noteDoc(n)
	nd.Tags = oldNote.Tags
	noteChanged, err := writeJSONIfChanged(filepath.Join(dir, "note.json"), nd)
	if err != nil {
		return nil, err
	}
	report.NoteChanged = noteChanged
	for i, p := range n.Pages {
		pd := pageDoc(p)
		devHash := deviceHash(pd, svgs[p.ID])
		pc := PageWriteReport{PageID: p.ID, AdoptedFrom: adopted[p.ID]}
		// Stamp the template selector from the source SVG (before the background
		// pipeline runs), so it is captured under every background mode. Absent
		// inline background (blank page) leaves it empty.
		if svg, ok := svgs[p.ID]; ok {
			if h, ok := backgroundHash(svg); ok {
				pd.BackgroundHash = h
			}
		}
		// Re-ingest must not discard a page's derived AI analysis: carry it over from
		// the existing page doc gathered in the preflight (ingest itself never
		// produces one). Absent = the normal first-ingest case, nothing to carry.
		// Region fingerprints ride along inside Analysis, so this carries them too.
		old, hadOld := oldPages[p.ID]
		if hadOld {
			pd.Analysis = old.Analysis
			pd.Tags = old.Tags
			pc.DroppedRegions = carryRegionAnalyses(&pd, old)
		} else {
			pc.New = true
		}
		// The device stamp moves only when the device's view of the page did. A page
		// new to this note (first ingest or adopted from elsewhere) is a device
		// change; an empty stored hash is a just-migrated page, whose hash is
		// recorded as the baseline without claiming a change nobody observed.
		pd.DeviceHash = devHash
		switch {
		case !hadOld || len(adopted[p.ID]) > 0:
			pd.DeviceModifiedAt = a.now()
		case old.DeviceHash != "" && old.DeviceHash != pd.DeviceHash:
			pd.DeviceModifiedAt = a.now()
		default:
			pd.DeviceModifiedAt = old.DeviceModifiedAt
		}
		if svg, ok := svgs[p.ID]; ok {
			if a.SVG.Background == BackgroundExtract {
				rewritten, img, name, hasBG := extractBackground(svg)
				if hasBG {
					bgChanged, err := writeBackground(dir, name, img)
					if err != nil {
						return nil, fmt.Errorf("write background for page %s: %w", p.ID, err)
					}
					pc.BackgroundChanged = bgChanged
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
			svgChanged, err := writeFileIfChanged(filepath.Join(dir, p.ID+".svg"), svg)
			if err != nil {
				return nil, fmt.Errorf("write svg for page %s: %w", p.ID, err)
			}
			pc.SVGChanged = svgChanged
		}
		// The doc goes last so WritePage can stamp ModifiedAt for a page whose SVG
		// alone changed.
		jsonChanged, err := a.WritePage(n.FileID, pd, pc.SVGChanged)
		if err != nil {
			return nil, err
		}
		pc.JSONChanged = jsonChanged
		if pc.New || len(pc.AdoptedFrom) > 0 || pc.JSONChanged || pc.SVGChanged || pc.BackgroundChanged || len(pc.DroppedRegions) > 0 {
			report.Pages = append(report.Pages, pc)
		}
	}
	return report, nil
}

// preflight reads the note and its kept pages through the schema-gated readers
// before Write mutates anything, so a stale-schema file aborts (ErrSchemaVersion)
// with no partial write. It covers the note's own directory; locateForeign is the
// other half, reading the donors of any page this note has gained. It returns the old note doc (for the note-tag
// carry-forward) and the old page docs keyed by id (for the analysis/tag
// carry-forward); os.ErrNotExist is the normal first-ingest case for a page or the
// whole note dir, so it is safe to call before MkdirAll.
func (a *Archive) preflight(fileID string, pages []snote.Page) (NoteDoc, map[string]PageDoc, error) {
	var oldNote NoteDoc
	switch nd, err := a.ReadNote(fileID); {
	case err == nil:
		oldNote = nd
	case errors.Is(err, os.ErrNotExist):
		// first ingest of this note — nothing to carry
	default:
		return NoteDoc{}, nil, err
	}
	old := make(map[string]PageDoc, len(pages))
	for _, p := range pages {
		pd, err := a.ReadPage(fileID, p.ID)
		switch {
		case err == nil:
			old[p.ID] = pd
		case errors.Is(err, os.ErrNotExist):
			// first ingest of this page — nothing to carry
		default:
			return NoteDoc{}, nil, err
		}
	}
	return oldNote, old, nil
}

// carryRegionAnalyses copies per-title/per-link analyses from old into pd,
// matched by exact rect (the region's identity: same rect = same handwriting
// underneath, even if the link's target changed). Regions that moved or resized
// drop their analysis and get re-transcribed by the next analyze run. Duplicate
// rects are consumed in order, each old analysis at most once. It returns the old
// regions whose analysis was non-nil but went unconsumed — the dropped analyses,
// in title-then-link, old-document order.
func carryRegionAnalyses(pd *PageDoc, old PageDoc) []DroppedRegion {
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
	// Any old analysis left in the queues had no matching rect in the new page: it
	// is dropped. Report in old-document order (titles first, then links) so the
	// result is deterministic; consume each queue front-to-back to stay in sync
	// with the matching loops above.
	var dropped []DroppedRegion
	for _, t := range old.Titles {
		q := titles[t.Rect]
		if len(q) == 0 {
			continue
		}
		titles[t.Rect] = q[1:]
		if q[0] != nil {
			dropped = append(dropped, DroppedRegion{Kind: "title", Rect: t.Rect})
		}
	}
	for _, l := range old.Links {
		q := links[l.Rect]
		if len(q) == 0 {
			continue
		}
		links[l.Rect] = q[1:]
		if q[0] != nil {
			dropped = append(dropped, DroppedRegion{Kind: "link", Rect: l.Rect})
		}
	}
	return dropped
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
// file and re-ingest writes nothing new. It reports whether it wrote.
func writeBackground(noteDir, name string, img []byte) (bool, error) {
	dir := filepath.Join(noteDir, backgroundsDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, fmt.Errorf("create %s: %w", dir, err)
	}
	return writeFileIfChanged(filepath.Join(dir, name), img)
}

// writeJSONIfChanged marshals v to indented JSON and writes it only when the bytes
// differ from disk; it reports whether it wrote.
func writeJSONIfChanged(path string, v any) (bool, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return false, fmt.Errorf("marshal %s: %w", filepath.Base(path), err)
	}
	b = append(b, '\n')
	return writeFileIfChanged(path, b)
}

// writeFileIfChanged writes data only when it differs from what is on disk, so
// unchanged files keep their bytes (and mtime) and produce no VCS churn. It reports
// whether it wrote (false = the file was already up to date).
func writeFileIfChanged(path string, data []byte) (bool, error) {
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, data) {
		return false, nil
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return false, fmt.Errorf("write %s: %w", path, err)
	}
	return true, nil
}

// sortedKeys is the deterministic iteration order for a map keyed by page or file id.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
