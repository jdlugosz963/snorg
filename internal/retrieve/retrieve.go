// Package retrieve is the platform-agnostic read contract over an archive: it
// assembles the per-note storage files (note.json + each <PAGEID>.json) into a
// single denormalized NoteView that any external tool can consume to build a
// human-readable form (e.g. an org-mode generator). It is deliberately ignorant
// of any consumer; the view types are the stable JSON contract, decoupled from
// the on-disk Doc types.
package retrieve

import (
	"fmt"

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
	Number   int           `json:"number"`
	PageID   string        `json:"page_id"`
	Starred  bool          `json:"starred"`
	SVG      string        `json:"svg"`
	Titles   []TitleView   `json:"titles"`
	Keywords []KeywordView `json:"keywords"`
	Links    []LinkView    `json:"links"`
}

// TitleView is a title region. Text is empty until the future vision-LLM phase
// fills it; the field exists now so the contract is forward-stable.
type TitleView struct {
	Rect  snote.Rect `json:"rect"`
	Level int        `json:"level"`
	Text  string     `json:"text"`
}

type KeywordView struct {
	Text string `json:"text"`
}

// LinkView is a tap-target. Internal is derived: the link stays within this note
// iff TargetFileID equals the note's FileID. TargetPageID is the destination page's
// stable id (resolve a heading by page_id rather than a shifting page number).
type LinkView struct {
	Rect         snote.Rect `json:"rect"`
	TargetPageID string     `json:"target_page_id"`
	TargetFileID string     `json:"target_file_id"`
	Name         string     `json:"name"`
	Internal     bool       `json:"internal"`
}

// List returns the FILE_IDs available in the archive.
func List(a *archive.Archive) ([]string, error) {
	return a.List()
}

// Get assembles the NoteView for fileID, preserving note.json page order.
func Get(a *archive.Archive, fileID string) (*NoteView, error) {
	nd, err := a.ReadNote(fileID)
	if err != nil {
		return nil, err
	}
	view := &NoteView{
		FileID:    nd.FileID,
		Signature: nd.Signature,
		Device:    nd.Device,
		Source:    nd.Source,
		Pages:     make([]PageView, 0, len(nd.Pages)),
	}
	for _, ref := range nd.Pages {
		pd, err := a.ReadPage(fileID, ref.ID)
		if err != nil {
			return nil, fmt.Errorf("page %s: %w", ref.ID, err)
		}
		view.Pages = append(view.Pages, pageView(nd.FileID, ref, pd, a.SVGRel(fileID, ref.ID)))
	}
	return view, nil
}

func pageView(fileID string, ref archive.NotePageRef, pd archive.PageDoc, svg string) PageView {
	titles := make([]TitleView, 0, len(pd.Titles))
	for _, t := range pd.Titles {
		titles = append(titles, TitleView{Rect: t.Rect, Level: t.Level})
	}
	keywords := make([]KeywordView, 0, len(pd.Keywords))
	for _, k := range pd.Keywords {
		keywords = append(keywords, KeywordView{Text: k.Text})
	}
	links := make([]LinkView, 0, len(pd.Links))
	for _, l := range pd.Links {
		links = append(links, LinkView{
			Rect:         l.Rect,
			TargetPageID: l.TargetPageID,
			TargetFileID: l.TargetFileID,
			Name:         l.Name,
			Internal:     l.TargetFileID == fileID,
		})
	}
	return PageView{
		Number:   ref.Number,
		PageID:   ref.ID,
		Starred:  ref.Starred,
		SVG:      svg,
		Titles:   titles,
		Keywords: keywords,
		Links:    links,
	}
}
