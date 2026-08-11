// Package sntool implements snote.Source with the pure-Go
// github.com/jdlugosz963/sntool library: it parses the .note metadata and renders
// each page to SVG in-process, needing no external tool. It is the concrete adapter
// behind the snote.Source seam.
package sntool

import (
	"fmt"

	sn "github.com/jdlugosz963/sntool"
	"github.com/jdlugosz963/sntool/notebook"
	"github.com/jdlugosz963/sntool/render"

	"github.com/jdlugosz963/snorg/internal/snote"
)

// Source is the sntool-library backed implementation of snote.Source.
type Source struct{}

// New returns an sntool-library backed Source.
func New() *Source { return &Source{} }

// Read parses the note at path with sntool and maps it into the domain model.
// sntool already resolves each title/keyword/link to its owning page (0-based
// Annotation.PageNumber), so association is a straight bucket onto the pages.
func (s *Source) Read(path string) (*snote.Note, error) {
	nb, err := sn.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open note: %w", err)
	}

	note := &snote.Note{
		FileID:    nb.FileID(),
		Signature: nb.Signature,
		Device:    nb.Device(),
	}
	for i, p := range nb.Pages {
		note.Pages = append(note.Pages, snote.Page{
			ID:      p.PageID(),
			Number:  i + 1,
			Starred: p.Starred(),
		})
	}
	pageAt := func(n int) *snote.Page { // n is 0-based
		if n < 0 || n >= len(note.Pages) {
			return nil
		}
		return &note.Pages[n]
	}

	for _, t := range nb.Titles {
		r, ok := t.Rect()
		if !ok {
			continue
		}
		p := pageAt(t.PageNumber)
		if p == nil {
			continue
		}
		p.Titles = append(p.Titles, snote.Title{
			Rect:  snote.Rect{X: r.X, Y: r.Y, W: r.W, H: r.H},
			Level: t.Level(),
			Seq:   t.Seq(),
		})
	}

	for _, k := range nb.Keywords {
		p := pageAt(k.PageNumber)
		if p == nil {
			continue
		}
		p.Keywords = append(p.Keywords, snote.Keyword{Text: k.Text()})
	}

	for _, l := range nb.Links {
		r, ok := l.Rect()
		if !ok {
			continue
		}
		p := pageAt(l.PageNumber)
		if p == nil {
			continue
		}
		p.Links = append(p.Links, snote.Link{
			Rect:         snote.Rect{X: r.X, Y: r.Y, W: r.W, H: r.H},
			TargetPageID: l.TargetPageID(),
			TargetFileID: l.TargetFileID(),
			Name:         notebook.LinkName(l.FilePathB64()),
		})
	}

	return note, nil
}

// RenderSVGs renders every page of the note at path to SVG in page order (index 0 =
// first page). The file is parsed once and each page is traced from that in-memory
// notebook.
func (s *Source) RenderSVGs(path string) ([][]byte, error) {
	nb, err := sn.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open note: %w", err)
	}
	svgs := make([][]byte, len(nb.Pages))
	for i := range nb.Pages {
		svg, err := render.SVG(nb, i, render.Options{})
		if err != nil {
			return nil, fmt.Errorf("render page %d: %w", i+1, err)
		}
		svgs[i] = []byte(svg)
	}
	return svgs, nil
}
