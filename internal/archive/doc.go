package archive

import "github.com/jdlugosz963/snorg/internal/snote"

// The Doc types are the serialization boundary: they define the stable plaintext
// JSON contract written to disk, decoupled from the in-memory domain model.

// CurrentSchemaVersion is the JSON grammar version this binary reads and writes.
// Every note.json/<PAGEID>.json is stamped with it (SchemaVersion), and
// ReadNote/ReadPage reject any file not equal to it. Bump it (and add the matching
// migration step) whenever the JSON contract changes; a future `migrate` command
// walks stale files forward one version at a time. 0 (absent field) is
// pre-versioning, and is also stale.
const CurrentSchemaVersion = 1

// NoteDoc is note.json — file metadata plus ordered page placement.
type NoteDoc struct {
	SchemaVersion int           `json:"schema_version"`
	FileID        string        `json:"file_id"`
	Signature     string        `json:"signature"`
	Device        string        `json:"device"`
	Source        string        `json:"source"`
	Pages         []NotePageRef `json:"pages"`
}

// NotePageRef is one entry in note.json's page placement.
type NotePageRef struct {
	ID     string `json:"id"`
	Number int    `json:"number"`
}

// PageDoc is <PAGEID>.json — the per-page deterministic metadata, optionally
// enriched with a derived AI Analysis (written by the analyze command, not ingest).
type PageDoc struct {
	SchemaVersion int           `json:"schema_version"`
	PageID        string        `json:"page_id"`
	Starred       bool          `json:"starred"`
	Titles        []TitleDoc    `json:"titles"`
	Keywords      []KeywordDoc  `json:"keywords"`
	Links         []LinkDoc     `json:"links"`
	Analysis      *PageAnalysis `json:"analysis,omitempty"`
}

// PageAnalysis is the page-level derived AI output. The transcribed content
// itself lives in the <PAGEID>.md sidecar (multiline markdown, diff-friendly);
// per-region transcriptions live on TitleDoc/LinkDoc. SourceHash fingerprints
// the rasterized page the analysis was derived from, so analyze can skip
// unchanged pages. Fields holds configurable custom outputs (e.g. "summary")
// derived from the content by name.
type PageAnalysis struct {
	SourceHash string            `json:"source_hash"`
	Fields     map[string]string `json:"fields,omitempty"`
}

// TitleAnalysis is a title region's transcription. Edited marks Name as a
// user override (set via analyze-edit): analyze then leaves it untouched and
// re-transcribes only non-edited regions. It is edit-preservation bookkeeping,
// not part of the retrieve read contract.
type TitleAnalysis struct {
	Name   string `json:"name"`
	Edited bool   `json:"edited,omitempty"`
}

// LinkAnalysis is a link region's transcription. Edited behaves as in
// TitleAnalysis.
type LinkAnalysis struct {
	Name   string `json:"name"`
	Edited bool   `json:"edited,omitempty"`
}

type TitleDoc struct {
	Rect     snote.Rect     `json:"rect"`
	Level    int            `json:"level"`
	Analysis *TitleAnalysis `json:"analysis,omitempty"`
}

type KeywordDoc struct {
	Text string `json:"text"`
}

type LinkDoc struct {
	Rect         snote.Rect    `json:"rect"`
	TargetPageID string        `json:"target_page_id"`
	TargetFileID string        `json:"target_file_id"`
	Name         string        `json:"name"`
	Analysis     *LinkAnalysis `json:"analysis,omitempty"`
}

func noteDoc(n *snote.Note) NoteDoc {
	pages := make([]NotePageRef, 0, len(n.Pages))
	for _, p := range n.Pages {
		pages = append(pages, NotePageRef{ID: p.ID, Number: p.Number})
	}
	return NoteDoc{
		SchemaVersion: CurrentSchemaVersion,
		FileID:        n.FileID,
		Signature:     n.Signature,
		Device:        n.Device,
		Source:        n.Source,
		Pages:         pages,
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
	return PageDoc{SchemaVersion: CurrentSchemaVersion, PageID: p.ID, Starred: p.Starred, Titles: titles, Keywords: keywords, Links: links}
}
