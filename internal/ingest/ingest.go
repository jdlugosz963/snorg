// Package ingest orchestrates registering a single .note into an archive:
// read the domain model from a snote.Source, render each page to SVG, and write
// everything through the archive store.
package ingest

import (
	"fmt"
	"path/filepath"

	"github.com/jdlugosz963/snorg/internal/archive"
	"github.com/jdlugosz963/snorg/internal/snote"
)

// Run ingests notePath into store via src and returns the parsed note.
func Run(src snote.Source, store *archive.Archive, notePath string) (*snote.Note, error) {
	note, err := src.Read(notePath)
	if err != nil {
		return nil, fmt.Errorf("read note: %w", err)
	}
	note.Source = filepath.Base(notePath)

	svgs := make(map[string][]byte, len(note.Pages))
	for i, p := range note.Pages {
		svg, err := src.RenderSVG(notePath, i)
		if err != nil {
			return nil, fmt.Errorf("render page %d: %w", i+1, err)
		}
		svgs[p.ID] = svg
	}

	if err := store.Write(note, svgs); err != nil {
		return nil, fmt.Errorf("write archive: %w", err)
	}
	return note, nil
}
