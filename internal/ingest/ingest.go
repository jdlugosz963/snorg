// Package ingest orchestrates registering .note files into an archive: read the
// domain model from a snote.Source, render all pages to SVG in one pass, and write
// everything through the archive store. Run handles one note; RunMany ingests a
// batch one note at a time, and NoteFiles discovers notes under a tree.
package ingest

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jdlugosz963/snorg/internal/archive"
	"github.com/jdlugosz963/snorg/internal/snote"
)

// Run ingests notePath into store via src and returns the parsed note plus the
// store's WriteReport (what the incremental reconcile changed on disk).
func Run(src snote.Source, store *archive.Archive, notePath string) (*snote.Note, *archive.WriteReport, error) {
	note, err := src.Read(notePath)
	if err != nil {
		return nil, nil, fmt.Errorf("read note: %w", err)
	}
	note.Source = filepath.Base(notePath)

	pageSVGs, err := src.RenderSVGs(notePath)
	if err != nil {
		return nil, nil, fmt.Errorf("render pages: %w", err)
	}
	if len(pageSVGs) != len(note.Pages) {
		return nil, nil, fmt.Errorf("rendered %d pages, note has %d", len(pageSVGs), len(note.Pages))
	}
	svgs := make(map[string][]byte, len(note.Pages))
	for i, p := range note.Pages {
		svgs[p.ID] = pageSVGs[i]
	}

	report, err := store.Write(note, svgs)
	if err != nil {
		return nil, nil, fmt.Errorf("write archive: %w", err)
	}
	return note, report, nil
}

// Result is the outcome of ingesting one path in a batch: on success Note and
// Report are set (Report says what changed on disk); on failure Err is set.
// Results preserve the input order of the paths passed to RunMany.
type Result struct {
	Path   string
	Note   *snote.Note
	Report *archive.WriteReport
	Err    error
}

// Options tunes a batch ingest.
type Options struct {
	// OnResult, when set, is called with each note's Result as it lands, so a
	// caller can report progress instead of waiting for the whole batch. Calls
	// are sequential, on the calling goroutine, in the order of the paths passed
	// to RunMany. Every Result passed here is also in the returned slice.
	OnResult func(Result)
}

// RunMany ingests paths into store one note at a time. A failed note never
// aborts the batch — every path yields a Result, in input order.
func RunMany(src snote.Source, store *archive.Archive, paths []string, opts Options) []Result {
	results := make([]Result, len(paths))
	for i, path := range paths {
		note, report, err := Run(src, store, path)
		results[i] = Result{Path: path, Note: note, Report: report, Err: err}
		if opts.OnResult != nil {
			opts.OnResult(results[i])
		}
	}
	return results
}

// NoteFiles walks root recursively and returns every *.note file path, sorted for
// deterministic order.
func NoteFiles(root string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.EqualFold(filepath.Ext(path), ".note") {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan %s: %w", root, err)
	}
	sort.Strings(paths)
	return paths, nil
}
