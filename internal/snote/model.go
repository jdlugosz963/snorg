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

// LinkKind classifies a tap-target's destination, mirroring the device LINKTYPE.
// Only note/file/web are known device values; anything else is LinkUnknown.
type LinkKind string

const (
	LinkUnknown LinkKind = "unknown"
	LinkNote    LinkKind = "note" // another .note (internal jump iff TargetFileID == Note.FileID)
	LinkFile    LinkKind = "file" // non-note document on device (pdf, epub, …)
	LinkWeb     LinkKind = "web"  // web URL (http/https)
)

// Link is a tap-target region pointing at another page, note, document or URL.
// Kind classifies the destination; a note link is internal iff TargetFileID ==
// Note.FileID. TargetPageID/TargetFileID are meaningful only for note links;
// file/web links carry the destination in Target instead.
type Link struct {
	Rect          Rect
	Kind          LinkKind
	TargetPageID  string // PAGEID of the target page (note links; stable across reorder)
	TargetFileID  string // FILE_ID of the target note (note links)
	Target        string // decoded LINKFILE: device path (file) or URL (web), "" for note/absent
	TargetDocPage int    // page within the linked external document (file links; OBJPAGE), 0 otherwise
	Name          string // human label per kind (URL, file basename with ext, or note name), "" if unknown
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
