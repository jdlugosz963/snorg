package archive

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
