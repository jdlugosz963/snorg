package archive

// User edits on a page's transcription. <PAGEID>.md always holds the effective
// content — what retrieve and export show. When the user edits it (the
// analyze-edit command), the divergence from the last AI-produced transcription
// (the base) is kept in the <PAGEID>.md.diff sidecar as a unified diff base→md,
// so the base can be reconstructed (ReadAnalysisBase) and a re-analysis can
// 3-way merge the fresh AI output with the user's edits (MergeAnalysis) instead
// of overwriting them. The diff exists iff md diverges from base. Ingest needs
// no special handling: the <PAGEID>.* prune glob removes the diff with its
// page, and reconcile never touches unknown page artifacts.

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

// WriteAnalysisEdit stores content as the page's effective transcription while
// keeping base as the AI side: the md gets the content, the diff records
// base→content, and content returning to base removes the diff. The md is written
// before the diff so a crash in between fails loudly in ReadAnalysisBase rather
// than silently losing edits.
func (a *Archive) WriteAnalysisEdit(fileID, pageID, base, content string) error {
	b, c := NormMD(base), NormMD(content)
	// No base and no content means no transcription at all: leave no empty
	// sidecars behind.
	if b == "" && c == "" {
		if err := os.Remove(a.mdPath(fileID, pageID)); err != nil && !os.IsNotExist(err) {
			return err
		}
		return a.removeEditDiff(fileID, pageID)
	}
	if err := a.writeMD(fileID, pageID, content); err != nil {
		return err
	}
	if b == c {
		return a.removeEditDiff(fileID, pageID)
	}
	diff, err := textmerge.Diff(b, c)
	if err != nil {
		return err
	}
	return a.writeEditDiff(fileID, pageID, diff)
}

// MergeAnalysis reconciles a fresh AI transcription (theirs) with any user edits:
// without an edit diff the md simply becomes theirs; with one, the previous base,
// the current md (the user's version) and theirs go through a 3-way merge, the md
// becomes the merge result and the diff is rebased onto theirs (removed when the
// result equals it). Returns the effective content and whether conflict markers
// were written. A conflicted md still reverse-applies to theirs, so re-analyzing
// before the user resolves simply re-merges; resolution is another analyze-edit.
func (a *Archive) MergeAnalysis(fileID, pageID, theirs string) (string, bool, error) {
	diff, err := a.readEditDiff(fileID, pageID)
	if err != nil {
		return "", false, err
	}
	if diff == "" {
		if err := a.writeMD(fileID, pageID, theirs); err != nil {
			return "", false, err
		}
		return theirs, false, nil
	}
	base, err := a.ReadAnalysisBase(fileID, pageID)
	if err != nil {
		return "", false, err
	}
	mine, err := a.readMD(fileID, pageID)
	if err != nil {
		return "", false, err
	}
	merged, conflicts, err := textmerge.Merge(NormMD(base), NormMD(mine), NormMD(theirs))
	if err != nil {
		return "", false, err
	}
	if err := a.WriteAnalysisEdit(fileID, pageID, theirs, merged); err != nil {
		return "", false, err
	}
	return merged, conflicts, nil
}
