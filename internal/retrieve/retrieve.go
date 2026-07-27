// Package retrieve is the platform-agnostic read contract over an archive: it
// assembles the per-note storage files (note.json + each <PAGEID>.json) into
// denormalized NoteViews that any external tool can consume to build a
// human-readable form (e.g. an org-mode generator). Pages are addressed by
// PAGEID (typically piped from query) and come back grouped per owning note.
// It is deliberately ignorant of any consumer; the view types are the stable
// JSON contract, decoupled from the on-disk Doc types.
package retrieve

import (
	"fmt"
	"sort"
	"strings"

	"github.com/jdlugosz963/snorg/internal/archive"
	"github.com/jdlugosz963/snorg/internal/snote"
)

// NoteView is the assembled, consumer-facing representation of one archived note.
type NoteView struct {
	FileID    string     `json:"file_id"`
	Signature string     `json:"signature"`
	Device    string     `json:"device"`
	Source    string     `json:"source"`
	Pages     []PageView `json:"pages"`
}

// PageView is one page in placement order, with its SVG path relative to the
// archive root (join it with the archive location to resolve the file).
type PageView struct {
	Number   int               `json:"number"`
	PageID   string            `json:"page_id"`
	Starred  bool              `json:"starred"`
	SVG      string            `json:"svg"`
	Titles   []TitleView       `json:"titles"`
	Keywords []KeywordView     `json:"keywords"`
	Links    []LinkView        `json:"links"`
	Analysis *PageAnalysisView `json:"analysis,omitempty"`
}

// PageAnalysisView is the page's transcription — AI-produced, user-edited or
// user-written via analyze-edit (assembled from the <PAGEID>.md sidecar) —
// plus custom named fields once the page was AI-analyzed. Internal bookkeeping
// like the source hash is deliberately not exposed. Content may carry conflict
// markers until a merge conflict from analyze is resolved.
type PageAnalysisView struct {
	Content string            `json:"content"`
	Fields  map[string]string `json:"fields,omitempty"`
}

// TitleView is a title region, with its transcription (if analyzed) nested
// under analysis. NameAnalysisView deliberately exposes only the name, not the
// on-disk Edited override flag — that is edit-preservation bookkeeping.
type TitleView struct {
	Rect     snote.Rect        `json:"rect"`
	Level    int               `json:"level"`
	Analysis *NameAnalysisView `json:"analysis,omitempty"`
}

// NameAnalysisView is the consumer-facing transcription of a title or link
// region: just its name.
type NameAnalysisView struct {
	Name string `json:"name"`
}

type KeywordView struct {
	Text string `json:"text"`
}

// LinkView is a tap-target. Internal is derived: the link stays within this note
// iff TargetFileID equals the note's FileID. TargetPageID is the destination page's
// stable id (resolve a heading by page_id rather than a shifting page number).
type LinkView struct {
	Rect         snote.Rect        `json:"rect"`
	TargetPageID string            `json:"target_page_id"`
	TargetFileID string            `json:"target_file_id"`
	Name         string            `json:"name"`
	Internal     bool              `json:"internal"`
	Analysis     *NameAnalysisView `json:"analysis,omitempty"`
}

// List returns the FILE_IDs available in the archive.
func List(a *archive.Archive) ([]string, error) {
	return a.List()
}

// Get assembles NoteViews for the given PAGEIDs (deduplicated), grouped per
// owning note in archive List order; each view carries full note metadata but
// only the requested pages, in note.json placement order. A PAGEID owned by no
// note is an error.
func Get(a *archive.Archive, pageIDs []string) ([]*NoteView, error) {
	pending := make(map[string]bool, len(pageIDs))
	for _, id := range pageIDs {
		pending[id] = true
	}
	ids, err := a.List()
	if err != nil {
		return nil, err
	}
	var views []*NoteView
	for _, fileID := range ids {
		nd, err := a.ReadNote(fileID)
		if err != nil {
			return nil, fmt.Errorf("note %s: %w", fileID, err)
		}
		var view *NoteView
		for _, ref := range nd.Pages {
			if !pending[ref.ID] {
				continue
			}
			delete(pending, ref.ID)
			pv, err := getPage(a, fileID, ref)
			if err != nil {
				return nil, err
			}
			if view == nil {
				view = &NoteView{
					FileID:    nd.FileID,
					Signature: nd.Signature,
					Device:    nd.Device,
					Source:    nd.Source,
				}
				views = append(views, view)
			}
			view.Pages = append(view.Pages, pv)
		}
	}
	if len(pending) > 0 {
		missing := make([]string, 0, len(pending))
		for id := range pending {
			missing = append(missing, id)
		}
		sort.Strings(missing)
		return nil, fmt.Errorf("page(s) not found in archive: %s", strings.Join(missing, ", "))
	}
	return views, nil
}

// getPage assembles one PageView, joining <PAGEID>.json with the .md sidecar.
func getPage(a *archive.Archive, fileID string, ref archive.NotePageRef) (PageView, error) {
	pd, err := a.ReadPage(fileID, ref.ID)
	if err != nil {
		return PageView{}, fmt.Errorf("page %s: %w", ref.ID, err)
	}
	content, err := a.ReadAnalysisMD(fileID, ref.ID)
	if err != nil {
		return PageView{}, fmt.Errorf("page %s: %w", ref.ID, err)
	}
	// The md sidecar is the page's transcription whether AI-produced,
	// user-edited or user-written (analyze-edit), so it is exposed whenever
	// present; fields exist only once the page was AI-analyzed.
	var analysis *PageAnalysisView
	if pd.Analysis != nil || content != "" {
		// The sidecar ends in a newline (file hygiene); the view carries the
		// content itself so templates control the surrounding whitespace.
		analysis = &PageAnalysisView{Content: strings.TrimRight(content, "\n")}
		if pd.Analysis != nil {
			analysis.Fields = pd.Analysis.Fields
		}
	}
	return pageView(fileID, ref, pd, a.SVGRel(fileID, ref.ID), analysis), nil
}

func pageView(fileID string, ref archive.NotePageRef, pd archive.PageDoc, svg string, analysis *PageAnalysisView) PageView {
	titles := make([]TitleView, 0, len(pd.Titles))
	for _, t := range pd.Titles {
		var an *NameAnalysisView
		if t.Analysis != nil {
			an = &NameAnalysisView{Name: t.Analysis.Name}
		}
		titles = append(titles, TitleView{Rect: t.Rect, Level: t.Level, Analysis: an})
	}
	keywords := make([]KeywordView, 0, len(pd.Keywords))
	for _, k := range pd.Keywords {
		keywords = append(keywords, KeywordView{Text: k.Text})
	}
	links := make([]LinkView, 0, len(pd.Links))
	for _, l := range pd.Links {
		var an *NameAnalysisView
		if l.Analysis != nil {
			an = &NameAnalysisView{Name: l.Analysis.Name}
		}
		links = append(links, LinkView{
			Rect:         l.Rect,
			TargetPageID: l.TargetPageID,
			TargetFileID: l.TargetFileID,
			Name:         l.Name,
			Internal:     l.TargetFileID == fileID,
			Analysis:     an,
		})
	}
	return PageView{
		Number:   ref.Number,
		PageID:   ref.ID,
		Starred:  pd.Starred,
		SVG:      svg,
		Titles:   titles,
		Keywords: keywords,
		Links:    links,
		Analysis: analysis,
	}
}
