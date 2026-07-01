package snote

// Source reads a Supernote .note file into the domain model and renders its pages.
// It is the seam that isolates the concrete format/tooling from the rest of SNORG,
// so a native-Go parser can later replace the shell-based adapter without changes
// upstream.
type Source interface {
	// Read parses metadata for the note at path into the domain model.
	Read(path string) (*Note, error)
	// RenderSVGs renders every page of the note at path to SVG, in page order
	// (index 0 = first page), in a single pass.
	RenderSVGs(path string) ([][]byte, error)
}
