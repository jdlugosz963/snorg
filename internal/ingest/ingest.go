// Package ingest orchestrates registering .note files into an archive: read the
// domain model from a snote.Source, render all pages to SVG in one pass, and write
// everything through the archive store. Run handles one note; RunMany ingests a
// batch concurrently (one note per worker), and NoteFiles discovers notes under a tree.
package ingest

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

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

// RunMany ingests paths into store concurrently, one note per worker. jobs caps
// the worker count; jobs <= 0 falls back to runtime.NumCPU() (the work is
// CPU-bound native SVG rendering (potrace tracing), so more workers than cores only thrashes).
// A failed note never aborts the batch — every path yields a Result, in input
// order. Concurrency is safe because src is stateless and each note writes to its
// own <FILE_ID>/ directory under the shared store.
func RunMany(src snote.Source, store *archive.Archive, paths []string, jobs int) []Result {
	results := make([]Result, len(paths))
	if len(paths) == 0 {
		return results
	}
	if jobs <= 0 {
		jobs = runtime.NumCPU()
	}
	if jobs > len(paths) {
		jobs = len(paths)
	}

	indices := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < jobs; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range indices {
				note, report, err := Run(src, store, paths[i])
				results[i] = Result{Path: paths[i], Note: note, Report: report, Err: err}
			}
		}()
	}
	for i := range paths {
		indices <- i
	}
	close(indices)
	wg.Wait()
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
