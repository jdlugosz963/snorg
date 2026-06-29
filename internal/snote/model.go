// Package snote defines the device-agnostic domain model for a Supernote note
// and the Source contract for reading/rendering one. Adapters (see sntool) map a
// concrete .note file into this model so the rest of SNORG never touches the
// on-disk binary format directly.
package snote

// Rect is an axis-aligned rectangle in page pixel space (pages render 1920x2560).
type Rect struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

// Title is a handwritten title region on a page. Only its location/level is known
// at ingest time; extracting the written text is a later (vision-LLM) phase.
type Title struct {
	Rect  Rect
	Level int
	Seq   int
}

// Keyword is invisible page-level metadata; it has decoded text but no region.
type Keyword struct {
	Text string
}

// Link is a tap-target region pointing at another page or note.
// The link is internal iff TargetFileID == Note.FileID.
type Link struct {
	Rect         Rect
	TargetPageID string // PAGEID of the target page (stable across reorder, unlike a number)
	TargetFileID string
	Name         string // human name of the target note (decoded from LINKFILE), "" if unknown
}

// Page is one page of a note in its placement order.
type Page struct {
	ID       string // PAGEID, stable identity used for filenames
	Number   int    // 1-based placement
	Starred  bool   // FIVESTAR present
	Titles   []Title
	Keywords []Keyword
	Links    []Link
}

// Note is a whole Supernote file.
type Note struct {
	FileID    string
	Signature string
	Device    string
	Source    string // original filename, informational
	Pages     []Page
}
