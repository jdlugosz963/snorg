package archive

// The orphan store. A page's transcription, hand-edits, tags and analysis are the
// only things in the archive that cannot be rebuilt from the .note, so when a page
// leaves a note they are set aside here instead of deleted — the page may have been
// moved to another note on the device, and that note may not be ingested until much
// later (or may have been ingested already, before this one). Parking makes the
// outcome independent of ingest order.
//
//	<root>/orphans/
//	    <PAGEID>.json      the PageDoc as it was pruned (analysis, tags, region rects)
//	    <PAGEID>.md        the transcription
//	    <PAGEID>.md.diff   the hand-edits
//
// The layout is deliberately a note directory without a note: the same accessors
// (ReadPage, readMD, movePageFiles, removePageFiles) address it by passing orphanDir
// where a FILE_ID goes, so reclaiming a parked page and adopting one from a live note
// are the same code path with a different source directory. It holds no .svg — that
// is re-derivable from the .note, and its background href only resolves next to a
// note's backgrounds/ folder, so a parked one would render broken.
//
// Nothing collects the store automatically: an entry is the last copy of text snorg
// cannot reproduce. It disappears when some note claims the page; entries for pages
// genuinely deleted on the device stay as a few KB of plaintext, removable by hand.

import (
	"errors"
	"os"
	"path/filepath"
)

// orphanDir is the store's directory name, used where a FILE_ID goes. List only
// reports directories holding a note.json, so it is invisible to every read surface.
const orphanDir = "orphans"

// prunePage removes a page that left its note. State worth keeping is parked in the
// orphan store and the rest deleted; a page carrying nothing reclaimable is deleted
// outright, as before, so the store stays empty in ordinary use. It reports whether
// it parked.
func (a *Archive) prunePage(fileID, pageID string) (bool, error) {
	dir := filepath.Join(a.Root, fileID)
	park, err := a.reclaimable(fileID, pageID)
	if err != nil {
		return false, err
	}
	if park {
		dst := filepath.Join(a.Root, orphanDir)
		if err := os.MkdirAll(dst, 0o755); err != nil {
			return false, err
		}
		if _, err := movePageFiles(dir, dst, pageID, ".svg"); err != nil {
			return false, err
		}
	}
	// Whatever was not parked (the .svg, or everything) goes.
	return park, removePageFiles(dir, pageID)
}

// reclaimable reports whether a page carries state that could not be rebuilt by
// re-ingesting the note it belongs to: a transcription, hand-edits, snorg tags or an
// AI analysis. A page doc that cannot be read counts as reclaimable — parking an
// unreadable file is recoverable, deleting it is not.
func (a *Archive) reclaimable(fileID, pageID string) (bool, error) {
	for _, path := range []string{a.mdPath(fileID, pageID), a.diffPath(fileID, pageID)} {
		switch _, err := os.Stat(path); {
		case err == nil:
			return true, nil
		case errors.Is(err, os.ErrNotExist):
		default:
			return false, err
		}
	}
	pd, err := a.ReadPage(fileID, pageID)
	if err != nil {
		return true, nil
	}
	return len(pd.Tags) > 0 || pd.Analysis != nil, nil
}

// dropOrphanDirIfEmpty removes the store once its last entry has been reclaimed, so
// "no parked state" reads as an absent directory rather than an empty one. A
// non-empty directory makes os.Remove fail, which is exactly the guard wanted.
func (a *Archive) dropOrphanDirIfEmpty() {
	_ = os.Remove(filepath.Join(a.Root, orphanDir))
}
