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
const CurrentSchemaVersion = 4

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
//
// BackgroundHash is the sha256 of the decoded page background image (the template
// selector), stamped by ingest regardless of the SVG background mode — the one
// template-related field that is an ingest fact, so it stays top-level; the per-box
// analyze state lives under Analysis.Regions instead.
//
// Tags are snorg-managed labels attached from the tag command, independent of the
// device-set Keywords. They are not sourced from the .note (pageDoc leaves them
// empty), so re-ingest carries them forward like Analysis. Kept sorted and
// de-duplicated for deterministic, VCS-friendly output.
type PageDoc struct {
	SchemaVersion  int           `json:"schema_version"`
	PageID         string        `json:"page_id"`
	Starred        bool          `json:"starred"`
	BackgroundHash string        `json:"background_hash,omitempty"`
	Tags           []string      `json:"tags,omitempty"`
	Titles         []TitleDoc    `json:"titles"`
	Keywords       []KeywordDoc  `json:"keywords"`
	Links          []LinkDoc     `json:"links"`
	Analysis       *PageAnalysis `json:"analysis,omitempty"`
}

// RegionDoc is the per-box fingerprint state for a templated page: ID is the
// template box id (stable identity, matches the config's templates: section), SourceHash is
// the hash of the box rect cropped from the page's canonical black-on-white
// rasterization, so analyze can skip a box whose handwriting is unchanged. The
// transcription itself is the box's section in the <PAGEID>.md sidecar, keyed by
// the same id.
type RegionDoc struct {
	ID         string `json:"id"`
	SourceHash string `json:"source_hash"`
}

// PageAnalysis is the page-level derived AI output — everything the analyze
// command produces. The transcribed content itself lives in the <PAGEID>.md
// sidecar (multiline markdown, diff-friendly — a templated page's per-box sections
// share that same file); per-region title/link transcriptions live on
// TitleDoc/LinkDoc. SourceHash fingerprints the rasterized page the analysis was
// derived from, so analyze can skip unchanged pages. Fields holds configurable
// custom outputs (e.g. "summary") derived from the content by name. Regions is the
// per-box change-detection state for a templated page: it sits here (not top-level)
// because a region entry is purely analyze state — its label/rect/prompt live only
// in the config's templates: section and its text in the <PAGEID>.md sidecar.
type PageAnalysis struct {
	SourceHash string            `json:"source_hash"`
	Fields     map[string]string `json:"fields,omitempty"`
	Regions    []RegionDoc       `json:"regions,omitempty"`
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
	Rect          snote.Rect    `json:"rect"`
	Kind          string        `json:"kind"`
	TargetPageID  string        `json:"target_page_id,omitempty"`
	TargetFileID  string        `json:"target_file_id,omitempty"`
	Target        string        `json:"target,omitempty"`
	TargetDocPage int           `json:"target_doc_page,omitempty"`
	Name          string        `json:"name"`
	Analysis      *LinkAnalysis `json:"analysis,omitempty"`
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
			Rect:          l.Rect,
			Kind:          string(l.Kind),
			TargetPageID:  l.TargetPageID,
			TargetFileID:  l.TargetFileID,
			Target:        l.Target,
			TargetDocPage: l.TargetDocPage,
			Name:          l.Name,
		})
	}
	return PageDoc{SchemaVersion: CurrentSchemaVersion, PageID: p.ID, Starred: p.Starred, Titles: titles, Keywords: keywords, Links: links}
}
