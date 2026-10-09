package export

import (
	"regexp"

	"github.com/flosch/pongo2/v6"
)

func init() {
	// Registered process-wide; a name collision here is a programming error.
	if err := pongo2.RegisterFilter("denote", filterDenote); err != nil {
		panic(err)
	}
}

// filterDenote returns the Emacs denote identifier a value carries — a FILE_ID or
// PAGEID ({{ link.target_file_id|denote }} -> 20260414T171729) or a denote-scheme
// file name ({{ link.name|denote }} on "20260915T001940--solaris__calibre.epub"
// -> 20260915T001940) — or empty when it carries none, so a template can test
// it. The filter only extracts the id; the template builds the link around it,
// choosing its own label.
func filterDenote(in *pongo2.Value, _ *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
	s := in.String()
	if id, ok := denoteID(s); ok {
		return pongo2.AsValue(id), nil
	}
	id, _ := denoteFileID(s)
	return pongo2.AsValue(id), nil
}

// denoteID extracts the denote identifier embedded in a supernote id. FILE_IDs
// and PAGEIDs share the layout "F"/"P" + 14-digit YYYYMMDDHHMMSS + tail; the
// denote id is YYYYMMDD "T" HHMMSS. Input without a leading run of 14 digits
// after the prefix reports false.
func denoteID(id string) (string, bool) {
	s := id
	if len(s) > 0 && (s[0] == 'F' || s[0] == 'P') {
		s = s[1:]
	}
	n := 0
	for n < len(s) && s[n] >= '0' && s[n] <= '9' {
		n++
	}
	if n < 14 {
		return "", false
	}
	return s[:8] + "T" + s[8:14], true
}

// denoteFileName matches the identifier of a denote file name: leading
// (ID==sig--title__kw.ext, any component after the id optional) or, in denote's
// reordered layouts, an @@ID component anywhere.
var denoteFileName = regexp.MustCompile(`^(\d{8}T\d{6})(?:$|==|--|__|@@|\.)|@@(\d{8}T\d{6})(?:$|==|--|__|@@|\.)`)

// denoteFileID returns the denote identifier embedded in file name, if any.
func denoteFileID(name string) (string, bool) {
	m := denoteFileName.FindStringSubmatch(name)
	switch {
	case m == nil:
		return "", false
	case m[1] != "":
		return m[1], true
	default:
		return m[2], true
	}
}
