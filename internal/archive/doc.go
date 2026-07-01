package archive

import "github.com/jdlugosz963/snorg/internal/snote"

// The Doc types are the serialization boundary: they define the stable plaintext
// JSON contract written to disk, decoupled from the in-memory domain model.

// NoteDoc is note.json — file metadata plus ordered page placement.
type NoteDoc struct {
	FileID    string        `json:"file_id"`
	Signature string        `json:"signature"`
	Device    string        `json:"device"`
	Source    string        `json:"source"`
	Pages     []NotePageRef `json:"pages"`
}

// NotePageRef is one entry in note.json's page placement.
type NotePageRef struct {
	ID     string `json:"id"`
	Number int    `json:"number"`
}

// PageDoc is <PAGEID>.json — the per-page deterministic metadata, optionally
// enriched with a derived AI Analysis (written by the analyze command, not ingest).
type PageDoc struct {
	PageID   string        `json:"page_id"`
	Starred  bool          `json:"starred"`
	Titles   []TitleDoc    `json:"titles"`
	Keywords []KeywordDoc  `json:"keywords"`
	Links    []LinkDoc     `json:"links"`
	Analysis *PageAnalysis `json:"analysis,omitempty"`
}

// PageAnalysis is the vision-LLM analysis of a page. Content is the page's text
// as plaintext; Titles/Links carry per-region transcriptions aligned by index
// with PageDoc.Titles/PageDoc.Links. Fields holds configurable custom outputs
// (e.g. "summary") derived from Content by name.
type PageAnalysis struct {
	Content string            `json:"content"`
	Titles  []TitleAnalysis   `json:"titles,omitempty"`
	Links   []LinkAnalysis    `json:"links,omitempty"`
	Fields  map[string]string `json:"fields,omitempty"`
}

type TitleAnalysis struct {
	Name string `json:"name"`
}

type LinkAnalysis struct {
	Name string `json:"name"`
}

type TitleDoc struct {
	Rect  snote.Rect `json:"rect"`
	Level int        `json:"level"`
}

type KeywordDoc struct {
	Text string `json:"text"`
}

type LinkDoc struct {
	Rect         snote.Rect `json:"rect"`
	TargetPageID string     `json:"target_page_id"`
	TargetFileID string     `json:"target_file_id"`
	Name         string     `json:"name"`
}

func noteDoc(n *snote.Note) NoteDoc {
	pages := make([]NotePageRef, 0, len(n.Pages))
	for _, p := range n.Pages {
		pages = append(pages, NotePageRef{ID: p.ID, Number: p.Number})
	}
	return NoteDoc{
		FileID:    n.FileID,
		Signature: n.Signature,
		Device:    n.Device,
		Source:    n.Source,
		Pages:     pages,
	}
}

func pageDoc(p snote.Page) PageDoc {
	titles := make([]TitleDoc, 0, len(p.Titles))
	for _, t := range p.Titles {
		titles = append(titles, TitleDoc{Rect: t.Rect, Level: t.Level})
	}
	keywords := make([]KeywordDoc, 0, len(p.Keywords))
	for _, k := range p.Keywords {
		keywords = append(keywords, KeywordDoc{Text: k.Text})
	}
	links := make([]LinkDoc, 0, len(p.Links))
	for _, l := range p.Links {
		links = append(links, LinkDoc{
			Rect:         l.Rect,
			TargetPageID: l.TargetPageID,
			TargetFileID: l.TargetFileID,
			Name:         l.Name,
		})
	}
	return PageDoc{PageID: p.ID, Starred: p.Starred, Titles: titles, Keywords: keywords, Links: links}
}
