package archive

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ErrSchemaVersion is the sentinel wrapped by ReadNote/ReadPage when an on-disk
// file's schema_version differs from CurrentSchemaVersion. Callers distinguish it
// from a missing file (os.ErrNotExist) to fail loudly instead of clobbering a
// stale file; the future `migrate` command is the only reader that bypasses it.
var ErrSchemaVersion = errors.New("incompatible schema version")

// ErrNotFound is wrapped by every lookup of a PAGEID or FILE_ID the archive does
// not hold (FindPage, ReadNote, retrieve.Get). A missing note.json still also
// wraps os.ErrNotExist, which the ingest preflight relies on.
var ErrNotFound = errors.New("not found in archive")

// verifySchema gates a parsed doc: a version other than CurrentSchemaVersion is a
// stale (or future) grammar this binary must not touch, so it errors with a
// recovery hint rather than misinterpreting the bytes.
func verifySchema(kind, id string, got int) error {
	if got != CurrentSchemaVersion {
		return fmt.Errorf("%s %s: %w %d, expected %d — run `snorg migrate`",
			kind, id, ErrSchemaVersion, got, CurrentSchemaVersion)
	}
	return nil
}

// The read side of the archive: layout-aware accessors that turn the on-disk
// files back into Doc values. Retrieval/export commands build on these instead
// of knowing the directory structure themselves.

// List returns the FILE_IDs present in the archive (sub-directories that hold a
// note.json), sorted for deterministic output. Requiring a note.json is also what
// keeps snorg's own sub-directories out of every read surface: the orphan store
// (orphans/, see orphan.go) and a user's templates/ folder are not notes and must
// never be listed as one.
func (a *Archive) List() ([]string, error) {
	entries, err := os.ReadDir(a.Root)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", a.Root, err)
	}
	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(a.Root, e.Name(), "note.json")); err != nil {
			continue
		}
		ids = append(ids, e.Name())
	}
	sort.Strings(ids)
	return ids, nil
}

// ReadNote loads <fileID>/note.json.
func (a *Archive) ReadNote(fileID string) (NoteDoc, error) {
	var nd NoteDoc
	if err := readJSON(filepath.Join(a.Root, fileID, "note.json"), &nd); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return NoteDoc{}, fmt.Errorf("note %s: %w (%w)", fileID, ErrNotFound, err)
		}
		return NoteDoc{}, err
	}
	if err := verifySchema("note", fileID, nd.SchemaVersion); err != nil {
		return NoteDoc{}, err
	}
	return nd, nil
}

// ReadPage loads <fileID>/<pageID>.json.
func (a *Archive) ReadPage(fileID, pageID string) (PageDoc, error) {
	var pd PageDoc
	if err := readJSON(filepath.Join(a.Root, fileID, pageID+".json"), &pd); err != nil {
		return PageDoc{}, err
	}
	if err := verifySchema("page", pageID, pd.SchemaVersion); err != nil {
		return PageDoc{}, err
	}
	return pd, nil
}

// SVGRel is the page SVG path relative to the archive root, using forward slashes
// so it is stable across platforms and ready to join with the archive location.
func (a *Archive) SVGRel(fileID, pageID string) string {
	return filepath.ToSlash(filepath.Join(fileID, pageID+".svg"))
}

// ReadSVG returns the raw bytes of <fileID>/<pageID>.svg.
func (a *Archive) ReadSVG(fileID, pageID string) ([]byte, error) {
	b, err := os.ReadFile(filepath.Join(a.Root, fileID, pageID+".svg"))
	if err != nil {
		return nil, fmt.Errorf("read svg %s: %w", pageID, err)
	}
	return b, nil
}

// FindPage returns the FILE_ID of the note that owns pageID, scanning every note
// directory. A PAGEID names exactly one page in exactly one note — the invariant
// Write enforces by adopting a page that moved between notes on the device (adopt.go)
// — so more than one hit means a copy snorg did not put there, which re-ingesting
// the note that owns the page purges. It errors when no note, or more than one, holds
// the page.
func (a *Archive) FindPage(pageID string) (string, error) {
	ids, err := a.List()
	if err != nil {
		return "", err
	}
	var found []string
	for _, fileID := range ids {
		if _, err := os.Stat(filepath.Join(a.Root, fileID, pageID+".json")); err == nil {
			found = append(found, fileID)
		}
	}
	switch len(found) {
	case 0:
		return "", fmt.Errorf("page %s: %w", pageID, ErrNotFound)
	case 1:
		return found[0], nil
	default:
		return "", fmt.Errorf("page %s is in more than one note: %v — re-ingest them to repair the archive", pageID, found)
	}
}

// WritePage writes pd to <fileID>/<pageID>.json in the canonical format, leaving
// every sibling artifact (svg, other pages, backgrounds) untouched. It stamps the
// current schema version so a persisted doc always reports the grammar it was
// written in (and never fails its own next read). It reports whether the bytes
// actually changed, so a caller can tell a real metadata write from a no-op.
//
// It is the single page-doc writer, so it owns ModifiedAt: the stored stamp is
// kept unless the doc's content differs from the stored one (bookkeeping aside, see
// sameContent), the page is new, or touched says a sibling page file (svg, md,
// md.diff) changed in the same operation — which is why callers write those first.
func (a *Archive) WritePage(fileID string, pd PageDoc, touched bool) (bool, error) {
	path := filepath.Join(a.Root, fileID, pd.PageID+".json")
	pd.SchemaVersion = CurrentSchemaVersion
	var prev PageDoc
	switch err := readJSON(path, &prev); {
	case err == nil:
		pd.ModifiedAt = prev.ModifiedAt
		if touched || !sameContent(prev, pd) {
			pd.ModifiedAt = a.now()
		}
	case errors.Is(err, os.ErrNotExist):
		pd.ModifiedAt = a.now()
	default:
		return false, err
	}
	return writeJSONIfChanged(path, pd)
}

// WriteNote writes nd to <nd.FileID>/note.json in the canonical format, leaving
// every sibling artifact (pages, svgs, backgrounds) untouched. Like WritePage it
// stamps the current schema version so a persisted doc always reports the grammar
// it was written in, and reports whether the bytes actually changed, so a caller
// can tell a real metadata write from a no-op.
func (a *Archive) WriteNote(nd NoteDoc) (bool, error) {
	nd.SchemaVersion = CurrentSchemaVersion
	return writeJSONIfChanged(filepath.Join(a.Root, nd.FileID, "note.json"), nd)
}

// mdName is the transcription sidecar filename for a page. A page has exactly one
// <pageID>.md: plain markdown for a normal page, the id-keyed region-section
// document (see regions.go) for a templated page — the two forms are mutually
// exclusive per page, so they share the file.
func mdName(pageID string) string {
	return pageID + ".md"
}

func (a *Archive) mdPath(fileID, pageID string) string {
	return filepath.Join(a.Root, fileID, mdName(pageID))
}

// readMD returns a page's transcription sidecar content; a missing file reads as
// ("", nil) — the page was never analyzed/edited.
func (a *Archive) readMD(fileID, pageID string) (string, error) {
	b, err := os.ReadFile(a.mdPath(fileID, pageID))
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read %s: %w", mdName(pageID), err)
	}
	return string(b), nil
}

// writeMD writes a page's transcription sidecar in NormMD form.
func (a *Archive) writeMD(fileID, pageID, content string) error {
	_, err := writeFileIfChanged(a.mdPath(fileID, pageID), []byte(NormMD(content)))
	return err
}

// ReadAnalysisMD returns the page's transcription from <fileID>/<pageID>.md (plain
// content for a normal page, the region-section document for a templated page). A
// missing sidecar means the page was never analyzed and reads as ("", nil).
func (a *Archive) ReadAnalysisMD(fileID, pageID string) (string, error) {
	return a.readMD(fileID, pageID)
}

// WriteAnalysisMD writes the page's transcription (markdown) to the
// <fileID>/<pageID>.md sidecar in NormMD form.
func (a *Archive) WriteAnalysisMD(fileID, pageID, content string) error {
	return a.writeMD(fileID, pageID, content)
}

// NormMD normalizes transcription content to its stored form: exactly one
// trailing newline, with newline-only content collapsing to empty. Edit diffs
// are computed and compared in this form so they match the bytes on disk.
func NormMD(s string) string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return ""
	}
	return s + "\n"
}

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", filepath.Base(path), err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("parse %s: %w", filepath.Base(path), err)
	}
	return nil
}
