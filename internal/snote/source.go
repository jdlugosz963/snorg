package snote

// Source reads a Supernote .note file into the domain model and renders its pages.
// It is the seam that isolates the concrete format/tooling from the rest of SNORG,
// so a native-Go parser can later replace the shell-based adapter without changes
// upstream.
type Source interface {
	// Read parses metadata for the note at path into the domain model.
	Read(path string) (*Note, error)
	// RenderSVG renders a single page (0-based index) to SVG bytes.
	RenderSVG(path string, pageIndex int) ([]byte, error)
}
