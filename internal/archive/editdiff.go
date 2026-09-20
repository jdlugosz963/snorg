package archive

// User edits on a page's transcription. <PAGEID>.md always holds the effective
// content — what retrieve and export show. When the user edits it (the
// analyze-edit command), the divergence from the last AI-produced transcription
// (the base) is kept in the <PAGEID>.md.diff sidecar as a unified diff base→md,
// so the base can be reconstructed (ReadAnalysisBase) and a re-analysis can
// 3-way merge the fresh AI output with the user's edits (MergeAnalysis) instead
// of overwriting them. The diff exists iff md diverges from base. Ingest needs no
// special handling: both sidecars travel with their page — parked in the orphan store
// when the page leaves a note, moved along when it is adopted by another one (see
// orphan.go, adopt.go) — and reconcile never touches unknown page artifacts.

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/jdlugosz963/snorg/internal/textmerge"
)

func (a *Archive) diffPath(fileID, pageID string) string {
	return filepath.Join(a.Root, fileID, mdName(pageID)+".diff")
}

// readEditDiff returns a page's edit diff; a missing file means the md carries no
// user edits and reads as ("", nil), mirroring readMD.
func (a *Archive) readEditDiff(fileID, pageID string) (string, error) {
	b, err := os.ReadFile(a.diffPath(fileID, pageID))
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read %s.diff: %w", mdName(pageID), err)
	}
	return string(b), nil
}

func (a *Archive) writeEditDiff(fileID, pageID, diff string) error {
	_, err := writeFileIfChanged(a.diffPath(fileID, pageID), []byte(diff))
	return err
}

func (a *Archive) removeEditDiff(fileID, pageID string) error {
	if err := os.Remove(a.diffPath(fileID, pageID)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// ReadAnalysisBase returns the page's last AI-produced transcription: the md with
// the user's edit diff reverse-applied. No diff means the md is the base; no md
// reads as "".
func (a *Archive) ReadAnalysisBase(fileID, pageID string) (string, error) {
	md, err := a.readMD(fileID, pageID)
	if err != nil {
		return "", err
	}
	diff, err := a.readEditDiff(fileID, pageID)
	if err != nil {
		return "", err
	}
	if diff == "" {
		return md, nil
	}
	base, err := textmerge.Unapply(md, diff)
	if err != nil {
		name := mdName(pageID)
		return "", fmt.Errorf("page %s: %s.diff does not apply to %s (md edited outside snorg?) — remove %s.diff to accept the current md as the AI base: %w",
			pageID, name, name, name, err)
	}
	return base, nil
}

// writeEffective stores content as the page's effective transcription while
// keeping base as the AI side: the md gets the content, the diff records
// base→content, and content returning to base removes the diff. It returns the
// md's previous contents, so the caller can classify the transition. The md is
// written before the diff so a crash in between fails loudly in ReadAnalysisBase
// rather than silently losing edits.
//
// It is the shared body of WriteAnalysisEdit and MergeAnalysis, which differ only
// in how they classify what it did.
func (a *Archive) writeEffective(fileID, pageID, base, content string) (was string, err error) {
	was, err = a.readMD(fileID, pageID)
	if err != nil {
		return "", err
	}
	b, c := NormMD(base), NormMD(content)
	// No base and no content means no transcription at all: leave no empty
	// sidecars behind.
	if b == "" && c == "" {
		if err := os.Remove(a.mdPath(fileID, pageID)); err != nil && !os.IsNotExist(err) {
			return "", err
		}
		return was, a.removeEditDiff(fileID, pageID)
	}
	if err := a.writeMD(fileID, pageID, content); err != nil {
		return "", err
	}
	if b == c {
		return was, a.removeEditDiff(fileID, pageID)
	}
	diff, err := textmerge.Diff(b, c)
	if err != nil {
		return "", err
	}
	return was, a.writeEditDiff(fileID, pageID, diff)
}

// WriteAnalysisEdit stores content as the page's effective transcription while
// keeping base as the AI side (see writeEffective for the sidecar rules). It
// returns the TextChange from the md's previous contents to content.
//
// This is the one writer holding the AI base, so it is the one place that can
// report TextReverted: content returning to the base is a property of the write,
// not of the two texts.
func (a *Archive) WriteAnalysisEdit(fileID, pageID, base, content string) (TextChange, error) {
	was, err := a.writeEffective(fileID, pageID, base, content)
	if err != nil {
		return TextChange{}, err
	}
	// An empty base is no AI transcription at all, so there is nothing to return
	// to: emptying such a page is a clear, not a revert. Both leave the same
	// (absent) sidecars; only the verdict differs, and "cleared" says what
	// happened to the text rather than merely where it landed.
	c := TextChangeOf(was, content)
	if c.Changed() && NormMD(base) != "" && NormMD(content) == NormMD(base) {
		c.Kind = TextReverted
	}
	return c, nil
}

// MergeAnalysis reconciles a fresh AI transcription (theirs) with any user edits:
// without an edit diff the md simply becomes theirs; with one, the previous base,
// the current md (the user's version) and theirs go through a 3-way merge, the md
// becomes the merge result and the diff is rebased onto theirs (removed when the
// result equals it). It returns the TextChange from the md's previous contents to
// the merge result — Now is the new effective content, Was the old one, and
// Conflicts says whether markers were written. A merge can never revert, so no
// base is consulted for the verdict. A conflicted md still reverse-applies to
// theirs, so re-analyzing before the user resolves simply re-merges; resolution is
// another analyze-edit.
func (a *Archive) MergeAnalysis(fileID, pageID, theirs string) (TextChange, error) {
	diff, err := a.readEditDiff(fileID, pageID)
	if err != nil {
		return TextChange{}, err
	}
	if diff == "" {
		// No user edit: the md simply becomes theirs. It still goes through
		// writeEffective (base == content, so no diff is produced) so the
		// no-sidecar-for-no-transcription rule holds here too — a blank page
		// leaves no empty <PAGEID>.md behind.
		was, err := a.writeEffective(fileID, pageID, theirs, theirs)
		if err != nil {
			return TextChange{}, err
		}
		return TextChangeOf(was, theirs), nil
	}
	base, err := a.ReadAnalysisBase(fileID, pageID)
	if err != nil {
		return TextChange{}, err
	}
	mine, err := a.readMD(fileID, pageID)
	if err != nil {
		return TextChange{}, err
	}
	merged, conflicts, err := textmerge.Merge(NormMD(base), NormMD(mine), NormMD(theirs))
	if err != nil {
		return TextChange{}, err
	}
	was, err := a.writeEffective(fileID, pageID, theirs, merged)
	if err != nil {
		return TextChange{}, err
	}
	c := TextChangeOf(was, merged)
	// The structural answer from the merge beats the marker sniff.
	c.Conflicts = c.Conflicts || conflicts
	return c, nil
}
