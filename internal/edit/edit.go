// Package edit opens a page's transcription in the user's editor and stores
// the result. The content goes through the archive's edit-diff sidecar, so
// manual edits survive re-analysis (see internal/archive editdiff). The buffer
// also carries the title/link name transcriptions as an editable header (see
// buffer.go): a changed name is stored as a user override (Edited) that wins
// over re-analysis. It also serves pages never sent to an LLM: the editor opens
// empty (or with an empty-name header) and the saved text becomes the page's
// transcription.
package edit

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/jdlugosz963/snorg/internal/archive"
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
// carry arguments) and stores the result: the content section becomes the
// edited md (its divergence from the AI base lands in the edit-diff sidecar),
// and any changed title/link name becomes a user override (Edited) in the page
// JSON. It returns the content Outcome and how many names changed. The editor
// runs on a temp copy, so aborting it (non-zero exit) leaves the page untouched.
// Page is Serialize → run editor → Apply; a non-interactive caller uses those two
// directly (see the public snorg package).
func Page(a *archive.Archive, pageID, editor string) (Outcome, int, error) {
	buf, err := Serialize(a, pageID)
	if err != nil {
		return "", 0, err
	}

	dir, err := os.MkdirTemp("", "snorg-edit-")
	if err != nil {
		return "", 0, err
	}
	defer os.RemoveAll(dir)
	// The real PAGEID plus .md gives the editor a meaningful buffer name and
	// its Markdown mode.
	tmp := filepath.Join(dir, pageID+".md")
	if err := os.WriteFile(tmp, []byte(buf), 0o600); err != nil {
		return "", 0, err
	}
	if err := runEditor(editor, tmp); err != nil {
		return "", 0, err
	}
	b, err := os.ReadFile(tmp)
	if err != nil {
		return "", 0, err
	}
	return Apply(a, pageID, string(b))
}

// Serialize renders pageID's editable buffer: the per-region title/link name header
// (only when the page has regions) followed by the current transcription content.
// It is the input side of the buffer round-trip; feed an edited buffer to Apply.
func Serialize(a *archive.Archive, pageID string) (string, error) {
	fileID, err := a.FindPage(pageID)
	if err != nil {
		return "", err
	}
	pd, err := a.ReadPage(fileID, pageID)
	if err != nil {
		return "", err
	}
	cur, err := a.ReadAnalysisMD(fileID, pageID)
	if err != nil {
		return "", err
	}
	return serialize(pd, cur), nil
}

// Apply stores an edited buffer (as produced by Serialize) for pageID: the content
// section becomes the edited md (its divergence from the AI base lands in the
// edit-diff sidecar) and each changed title/link name becomes a user override
// (Edited) in the page JSON. It returns the content Outcome and how many names
// changed. A malformed header writes nothing.
func Apply(a *archive.Archive, pageID, buffer string) (Outcome, int, error) {
	fileID, err := a.FindPage(pageID)
	if err != nil {
		return "", 0, err
	}
	pd, err := a.ReadPage(fileID, pageID)
	if err != nil {
		return "", 0, err
	}
	base, err := a.ReadAnalysisBase(fileID, pageID)
	if err != nil {
		return "", 0, err
	}
	cur, err := a.ReadAnalysisMD(fileID, pageID)
	if err != nil {
		return "", 0, err
	}

	titleNames, linkNames, content, err := parse(buffer, len(pd.Titles), len(pd.Links))
	if err != nil {
		return "", 0, fmt.Errorf("page %s: %w (no changes saved)", pageID, err)
	}

	namesChanged := applyNames(&pd, titleNames, linkNames)
	if namesChanged > 0 {
		if err := a.WritePage(fileID, pd); err != nil {
			return "", 0, err
		}
	}

	switch archive.NormMD(content) {
	case archive.NormMD(cur):
		return Unchanged, namesChanged, nil
	case archive.NormMD(base):
		return Reverted, namesChanged, a.WriteAnalysisEdit(fileID, pageID, base, content)
	default:
		return Edited, namesChanged, a.WriteAnalysisEdit(fileID, pageID, base, content)
	}
}

// applyNames overwrites each region name that the user changed and marks it as
// an override (Edited), returning how many changed. A name equal to the current
// one is left untouched, so an untouched AI name keeps Edited=false and stays
// overwritable by future analysis.
func applyNames(pd *archive.PageDoc, titleNames, linkNames []string) int {
	changed := 0
	for i, name := range titleNames {
		cur := ""
		if pd.Titles[i].Analysis != nil {
			cur = pd.Titles[i].Analysis.Name
		}
		if name != cur {
			pd.Titles[i].Analysis = &archive.TitleAnalysis{Name: name, Edited: true}
			changed++
		}
	}
	for i, name := range linkNames {
		cur := ""
		if pd.Links[i].Analysis != nil {
			cur = pd.Links[i].Analysis.Name
		}
		if name != cur {
			pd.Links[i].Analysis = &archive.LinkAnalysis{Name: name, Edited: true}
			changed++
		}
	}
	return changed
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
