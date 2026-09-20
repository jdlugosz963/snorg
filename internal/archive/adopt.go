package archive

// Cross-note page moves. A PAGEID is minted by the device and is unique across the
// whole archive, so a page belongs to exactly one note directory — but a page can be
// moved to another note on the device, and re-ingesting only that note would
// otherwise leave the page stored twice, the second copy stripped of the analysis,
// tags and transcription still sitting in the first. Write therefore looks beyond its
// own directory for any page it is about to write: a page found elsewhere is *moved*,
// not copied, and the note it came from is repaired.
//
// Every read here happens in Write's preflight phase, before the first mutation, so a
// donor with a stale schema aborts the write with the archive untouched — adoption
// can never half-migrate a page.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/jdlugosz963/snorg/internal/snote"
)

// pageOrigin is where a page the incoming note claims currently lives.
type pageOrigin struct {
	// From is the directory whose state is carried over: the first donor holding a
	// transcription, else the first donor. The orphan store appears as orphanDir.
	From string
	// Donors is every directory that held the page, sorted. All of them are purged —
	// more than one means the archive was already duplicated and this write heals it.
	Donors []string
	// Doc is From's page doc, zero when it had none (a donor holding only a stray
	// sidecar is still worth rescuing).
	Doc PageDoc
}

// foreignPages is the archive-wide lookup's result: where each claimed page lives,
// plus the donor notes whose note.json the repair rewrites. Both are gathered through
// the schema-gated readers, so every stale-file check happens before any mutation.
type foreignPages struct {
	Origins map[string]pageOrigin
	Notes   map[string]NoteDoc
}

// locateForeign finds the pages of the incoming note that currently live somewhere
// else in the archive — under another note, or parked in the orphan store.
//
// It short-circuits when the note's own directory already holds every page, which is
// the ordinary re-ingest case, so the scan costs nothing until a note actually gains a
// page. Otherwise it reads each candidate directory once (O(notes) readdirs, not
// O(notes x pages) stats) and looks up *every* page of the note rather than only the
// missing ones — the same cost, and it also sweeps residue left by a crash mid-move.
func (a *Archive) locateForeign(selfID string, pages []snote.Page, have map[string]PageDoc) (foreignPages, error) {
	found := foreignPages{Origins: map[string]pageOrigin{}, Notes: map[string]NoteDoc{}}
	if len(have) == len(pages) {
		return found, nil
	}
	want := make(map[string]bool, len(pages))
	for _, p := range pages {
		want[p.ID] = true
	}

	fileIDs, err := a.List()
	if err != nil {
		return foreignPages{}, err
	}
	donors := map[string][]string{} // page id -> directories holding it
	for _, dir := range append(fileIDs, orphanDir) {
		if dir == selfID {
			continue
		}
		ids, err := archivedPageIDs(filepath.Join(a.Root, dir))
		if errors.Is(err, os.ErrNotExist) {
			continue // the orphan store does not exist yet
		}
		if err != nil {
			return foreignPages{}, err
		}
		for id := range ids {
			if want[id] {
				donors[id] = append(donors[id], dir)
			}
		}
	}

	for id, dirs := range donors {
		sort.Strings(dirs)
		o := pageOrigin{From: a.preferredDonor(dirs, id), Donors: dirs}
		switch pd, err := a.ReadPage(o.From, id); {
		case err == nil:
			o.Doc = pd
		case errors.Is(err, os.ErrNotExist):
			// a donor holding only a sidecar — nothing to carry but the text
		default:
			return foreignPages{}, fmt.Errorf("adopt page %s from %s: %w", id, o.From, err)
		}
		found.Origins[id] = o
		for _, dir := range dirs {
			if dir == orphanDir {
				continue
			}
			if _, ok := found.Notes[dir]; ok {
				continue
			}
			nd, err := a.ReadNote(dir)
			if err != nil {
				return foreignPages{}, fmt.Errorf("repair note %s: %w", dir, err)
			}
			found.Notes[dir] = nd
		}
	}
	return found, nil
}

// preferredDonor picks the directory whose state is worth carrying: the first holding
// a transcription, else the first. Only an already-duplicated archive has a choice to
// make, and there the copy with the text is the one that matters.
func (a *Archive) preferredDonor(dirs []string, pageID string) string {
	for _, dir := range dirs {
		if _, err := os.Stat(a.mdPath(dir, pageID)); err == nil {
			return dir
		}
	}
	return dirs[0]
}

// adopt moves a page into dstDir (the note now claiming it) and empties every donor.
// When keepOwn is set the destination already holds the page and its own state wins,
// so the donors are purged without carrying anything over — that is the heal path for
// an archive duplicated before the invariant existed, and the one place a
// transcription is dropped rather than parked. It returns the FILE_IDs whose note.json
// it repaired.
//
// The order per donor matters: the sidecars land at the destination before anything is
// deleted, and the donor's note.json stops referencing the page before the page's own
// files go — the reverse would leave a crash window where the donor lists a page whose
// file is gone, which is exactly what makes query and retrieve hard-error.
func (a *Archive) adopt(dstDir, pageID string, o pageOrigin, notes map[string]NoteDoc, keepOwn bool) ([]string, error) {
	var repaired []string
	for _, donor := range o.Donors {
		donorDir := filepath.Join(a.Root, donor)
		if donor == o.From && !keepOwn {
			// .json and .svg are rebuilt from the .note plus the carried Doc; every
			// other sidecar is state only the donor has.
			if _, err := movePageFiles(donorDir, dstDir, pageID, ".json", ".svg"); err != nil {
				return nil, err
			}
		}
		if nd, ok := notes[donor]; ok {
			// Fold the drop back into the map: when several pages move out of the
			// same note in one write, each repair must build on the last, or the
			// second would write back the page the first removed.
			// Fold the drop back into the map: when several pages move out of the
			// same note in one write, each repair must build on the last, or the
			// second would write back the page the first removed.
			nd = dropPageRef(nd, pageID)
			notes[donor] = nd
			if err := a.WriteNote(nd); err != nil {
				return nil, err
			}
			repaired = append(repaired, donor)
		}
		if err := removePageFiles(donorDir, pageID); err != nil {
			return nil, err
		}
	}
	return repaired, nil
}

// dropPageRef removes pageID from the note's page placement and renumbers the
// survivors from 1. Number is positional, not device data (sntool assigns it from the
// page's index), so contiguous renumbering is exactly what the donor's own next ingest
// will write — the repair is a fixed point, and that later ingest reports no change
// instead of a second, spurious diff.
func dropPageRef(nd NoteDoc, pageID string) NoteDoc {
	pages := make([]NotePageRef, 0, len(nd.Pages))
	for _, ref := range nd.Pages {
		if ref.ID == pageID {
			continue
		}
		pages = append(pages, NotePageRef{ID: ref.ID, Number: len(pages) + 1})
	}
	nd.Pages = pages
	return nd
}

// movePageFiles renames every <pageID>.* file from srcDir into dstDir, except those
// whose suffix after the page id is listed in skip. It returns the suffixes it moved,
// sorted. Naming what to leave behind rather than what to take means a sidecar added
// later travels with its page by default instead of being silently dropped.
func movePageFiles(srcDir, dstDir, pageID string, skip ...string) ([]string, error) {
	matches, err := filepath.Glob(filepath.Join(srcDir, pageID+".*"))
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)
	var moved []string
	for _, src := range matches {
		suffix := strings.TrimPrefix(filepath.Base(src), pageID)
		if slices.Contains(skip, suffix) {
			continue
		}
		if err := os.Rename(src, filepath.Join(dstDir, filepath.Base(src))); err != nil {
			return nil, fmt.Errorf("move %s: %w", src, err)
		}
		moved = append(moved, suffix)
	}
	return moved, nil
}
