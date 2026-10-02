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

// PageEdit is what an editor round-trip changed, in the shared change vocabulary
// (internal/archive change.go). Content is the page's effective document — the
// free-form content, or, on a templated page, the assembled region document, which
// Regions then breaks down section by section. Names are the title/link overrides
// the save established. Sparse: a zero PageEdit means the buffer came back
// identical and nothing was written.
type PageEdit struct {
	PageID      string
	Content     archive.TextChange
	Regions     []archive.RegionChange
	Names       []archive.NameChange
	JSONChanged bool
}

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
// JSON. It returns what the round-trip changed. The editor runs on a temp copy,
// so aborting it (non-zero exit) leaves the page untouched. Page is
// Serialize → run editor → Apply; a non-interactive caller uses those two
// directly (see the public snorg package).
func Page(a *archive.Archive, pageID, editor string) (PageEdit, error) {
	buf, err := Serialize(a, pageID)
	if err != nil {
		return PageEdit{}, err
	}

	dir, err := os.MkdirTemp("", "snorg-edit-")
	if err != nil {
		return PageEdit{}, err
	}
	defer os.RemoveAll(dir)
	// The real PAGEID plus .md gives the editor a meaningful buffer name and
	// its Markdown mode.
	tmp := filepath.Join(dir, pageID+".md")
	if err := os.WriteFile(tmp, []byte(buf), 0o600); err != nil {
		return PageEdit{}, err
	}
	if err := runEditor(editor, tmp); err != nil {
		return PageEdit{}, err
	}
	b, err := os.ReadFile(tmp)
	if err != nil {
		return PageEdit{}, err
	}
	return Apply(a, pageID, string(b))
}

// Serialize renders pageID's editable buffer: the per-region title/link name header
// plus, for a templated page, the template-box sections, followed by the current
// transcription content. It is the input side of the buffer round-trip; feed an
// edited buffer to Apply.
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
	sections, _, err := regionLayout(a, fileID, pageID, pd.BackgroundHash)
	if err != nil {
		return "", err
	}
	return serialize(pd, cur, sections), nil
}

// regionLayout returns a templated page's region sections in canonical order
// (template box order with current text, then tombstoned sections), plus their id
// list. It returns nil,nil for a page matching no template. Serialize renders the
// sections; Apply uses the ids to validate the edited buffer and rebuild the
// region-section md in the same order.
func regionLayout(a *archive.Archive, fileID, pageID, bgHash string) ([]archive.RegionSection, []string, error) {
	templates, err := a.Templates()
	if err != nil {
		return nil, nil, err
	}
	tmpl := templates.MatchBackground(bgHash)
	if tmpl == nil {
		return nil, nil, nil
	}
	md, err := a.ReadAnalysisMD(fileID, pageID)
	if err != nil {
		return nil, nil, err
	}
	existing := archive.ParseRegions(md)
	known := map[string]bool{}
	var sections []archive.RegionSection
	for _, box := range tmpl.Boxes {
		known[box.ID] = true
		sections = append(sections, archive.RegionSection{
			ID: box.ID, Label: box.Label, Text: archive.RegionText(existing, box.ID),
		})
	}
	for _, s := range existing { // tombstones: keep them editable, never drop
		if !known[s.ID] {
			sections = append(sections, s)
		}
	}
	ids := make([]string, len(sections))
	for i, s := range sections {
		ids[i] = s.ID
	}
	return sections, ids, nil
}

// Apply stores an edited buffer (as produced by Serialize) for pageID: the content
// section becomes the edited md (its divergence from the AI base lands in the
// edit-diff sidecar) and each changed title/link name becomes a user override
// (Edited) in the page JSON. It returns what the round-trip changed. A malformed
// header writes nothing.
func Apply(a *archive.Archive, pageID, buffer string) (PageEdit, error) {
	res := PageEdit{PageID: pageID}
	fileID, err := a.FindPage(pageID)
	if err != nil {
		return PageEdit{}, err
	}
	pd, err := a.ReadPage(fileID, pageID)
	if err != nil {
		return PageEdit{}, err
	}
	base, err := a.ReadAnalysisBase(fileID, pageID)
	if err != nil {
		return PageEdit{}, err
	}
	sections, regionIDs, err := regionLayout(a, fileID, pageID, pd.BackgroundHash)
	if err != nil {
		return PageEdit{}, err
	}

	titleNames, linkNames, regionTexts, content, err := parse(buffer, len(pd.Titles), len(pd.Links), regionIDs)
	if err != nil {
		return PageEdit{}, fmt.Errorf("page %s: %w (no changes saved)", pageID, err)
	}

	res.Names = applyNames(&pd, titleNames, linkNames)

	// The page has exactly one effective md document: for a templated page the
	// region sections rebuilt from the edited texts (canonical order, matching
	// what analyze regenerates), otherwise the free-form content. Either flows
	// through the same edit-diff sidecar against the same AI base.
	doc := content
	if len(regionIDs) > 0 {
		for i := range sections {
			was := sections[i].Text
			sections[i].Text = regionTexts[sections[i].ID]
			// One entry per section written — including analyze:false boxes and
			// tombstones, which the editor does let the user edit, so an edit
			// reports a broader set than analyze's per-analyze-box list.
			res.Regions = append(res.Regions, archive.RegionChange{
				ID:    sections[i].ID,
				Label: sections[i].Label,
				Text:  archive.TextChangeOf(was, sections[i].Text),
			})
		}
		doc = archive.AssembleRegions(sections)
	}

	// WriteAnalysisEdit classifies the transition and is a byte-level no-op when
	// the document did not move (writeMD/writeEditDiff both go through
	// writeFileIfChanged, removeEditDiff ignores a missing file), so an unchanged
	// buffer needs no guard here — and the verdict comes from the writer that
	// holds the AI base rather than from a second comparison.
	if res.Content, err = a.WriteAnalysisEdit(fileID, pageID, base, doc); err != nil {
		return PageEdit{}, err
	}
	// The doc goes after the md so its modified stamp covers a text-only edit; with
	// no rename and no text change it is a byte-level no-op like the md writer.
	if res.JSONChanged, err = a.WritePage(fileID, pd, res.Content.Changed()); err != nil {
		return PageEdit{}, err
	}
	return res, nil
}

// applyNames overwrites each region name that the user changed and marks it as
// an override (Edited), returning one NameChange per name that moved — keyed by
// kind and 1-based index, the same way the buffer's markers are, so a caller can
// name the thing the user edited. A name equal to the current one is left
// untouched, so an untouched AI name keeps Edited=false and stays overwritable by
// future analysis.
func applyNames(pd *archive.PageDoc, titleNames, linkNames []string) []archive.NameChange {
	var changed []archive.NameChange
	for i, name := range titleNames {
		cur := ""
		if pd.Titles[i].Analysis != nil {
			cur = pd.Titles[i].Analysis.Name
		}
		if name != cur {
			pd.Titles[i].Analysis = &archive.TitleAnalysis{Name: name, Edited: true}
			changed = append(changed, archive.NameChange{
				Kind: "title", Index: i + 1, Was: cur, Now: name, Override: true,
			})
		}
	}
	for i, name := range linkNames {
		cur := ""
		if pd.Links[i].Analysis != nil {
			cur = pd.Links[i].Analysis.Name
		}
		if name != cur {
			pd.Links[i].Analysis = &archive.LinkAnalysis{Name: name, Edited: true}
			changed = append(changed, archive.NameChange{
				Kind: "link", Index: i + 1, Was: cur, Now: name, Override: true,
			})
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
