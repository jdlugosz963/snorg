package archive

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// The read side of the archive: layout-aware accessors that turn the on-disk
// files back into Doc values. Retrieval/export commands build on these instead
// of knowing the directory structure themselves.

// List returns the FILE_IDs present in the archive (sub-directories that hold a
// note.json), sorted for deterministic output.
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
// directory. PAGEIDs are stable per-note ids; in practice unique across a single
// archive. It errors when no note (or more than one) holds the page.
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
		return "", fmt.Errorf("page %s not found in archive", pageID)
	case 1:
		return found[0], nil
	default:
		return "", fmt.Errorf("page %s is ambiguous, found in notes: %v", pageID, found)
	}
}

// WritePage writes pd to <fileID>/<pageID>.json in the canonical format, leaving
// every sibling artifact (svg, other pages, backgrounds) untouched.
func (a *Archive) WritePage(fileID string, pd PageDoc) error {
	return writeJSONIfChanged(filepath.Join(a.Root, fileID, pd.PageID+".json"), pd)
}

func (a *Archive) analysisMDPath(fileID, pageID string) string {
	return filepath.Join(a.Root, fileID, pageID+".md")
}

// ReadAnalysisMD returns the page's transcribed content from <fileID>/<pageID>.md.
// A missing sidecar means the page was never analyzed and reads as ("", nil).
func (a *Archive) ReadAnalysisMD(fileID, pageID string) (string, error) {
	b, err := os.ReadFile(a.analysisMDPath(fileID, pageID))
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read analysis %s: %w", pageID, err)
	}
	return string(b), nil
}

// WriteAnalysisMD writes the page's transcribed content (markdown) to the
// <fileID>/<pageID>.md sidecar in NormMD form.
func (a *Archive) WriteAnalysisMD(fileID, pageID, content string) error {
	return writeFileIfChanged(a.analysisMDPath(fileID, pageID), []byte(NormMD(content)))
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
