// Package edit opens a page's transcription in the user's editor and stores
// the result through the archive's edit-diff sidecar, so manual edits survive
// re-analysis (see internal/archive editdiff). It also serves pages never sent
// to an LLM: the editor opens empty and the saved text becomes the page's
// transcription.
package edit

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/jdlugosz963/snorg/internal/archive"
	"github.com/jdlugosz963/snorg/internal/textmerge"
)

// Outcome says what Page did with the editor's result.
type Outcome string

const (
	Unchanged Outcome = "unchanged" // editor saved identical content; nothing written
	Edited    Outcome = "edited"    // md updated, edit diff (re)written
	Reverted  Outcome = "reverted"  // content returned to the AI base; edit diff removed
)

// EditorFromEnv returns the editor command line: $VISUAL, else $EDITOR.
func EditorFromEnv() (string, error) {
	for _, v := range []string{"VISUAL", "EDITOR"} {
		if ed := os.Getenv(v); ed != "" {
			return ed, nil
		}
	}
	return "", fmt.Errorf("no editor configured: set $VISUAL or $EDITOR")
}

// Page opens pageID's transcription in editor (a shell command line, so it may
// carry arguments) and stores the result: the md becomes the edited content and
// the divergence from the AI base lands in the edit-diff sidecar. The editor
// runs on a temp copy, so aborting it (non-zero exit) leaves the page untouched.
func Page(a *archive.Archive, pageID, editor string) (Outcome, error) {
	// A missing git must surface before the editor opens, not after the user
	// has finished an edit that then cannot be saved.
	if err := textmerge.Available(); err != nil {
		return "", err
	}
	fileID, err := a.FindPage(pageID)
	if err != nil {
		return "", err
	}
	base, err := a.ReadAnalysisBase(fileID, pageID)
	if err != nil {
		return "", err
	}
	cur, err := a.ReadAnalysisMD(fileID, pageID)
	if err != nil {
		return "", err
	}

	dir, err := os.MkdirTemp("", "snorg-edit-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	// The real PAGEID plus .md gives the editor a meaningful buffer name and
	// its Markdown mode.
	tmp := filepath.Join(dir, pageID+".md")
	if err := os.WriteFile(tmp, []byte(cur), 0o600); err != nil {
		return "", err
	}
	if err := runEditor(editor, tmp); err != nil {
		return "", err
	}
	b, err := os.ReadFile(tmp)
	if err != nil {
		return "", err
	}

	edited := string(b)
	switch archive.NormMD(edited) {
	case archive.NormMD(cur):
		return Unchanged, nil
	case archive.NormMD(base):
		return Reverted, a.WriteAnalysisEdit(fileID, pageID, base, edited)
	default:
		return Edited, a.WriteAnalysisEdit(fileID, pageID, base, edited)
	}
}

// runEditor runs the editor command line on path through sh -c, inheriting the
// terminal (stdin/stdout/stderr) as interactive editors require.
func runEditor(editor, path string) error {
	cmd := exec.Command("sh", "-c", editor+` "$1"`, "sh", path)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("editor %q: %w (no changes saved)", editor, err)
	}
	return nil
}
