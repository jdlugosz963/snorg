package export

import (
	"strings"

	"github.com/flosch/pongo2/v6"
)

func init() {
	// Registered process-wide; a name collision here is a programming error.
	if err := pongo2.RegisterFilter("denote", filterDenote); err != nil {
		panic(err)
	}
}

// filterDenote turns a FILE_ID into an Emacs denote identifier, e.g.
// {{ link.target_file_id|denote }} -> 20260414T171729, for [[denote:...]] links.
func filterDenote(in *pongo2.Value, _ *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
	return pongo2.AsValue(denoteID(in.String())), nil
}

// denoteID extracts the denote identifier embedded in a FILE_ID. The layout is
// "F" + 14-digit YYYYMMDDHHMMSS + tail; the denote id is YYYYMMDD "T" HHMMSS. Input
// that does not match (no leading digits run of 14) is returned unchanged so a render
// never fails on an unexpected id.
func denoteID(fileID string) string {
	s := strings.TrimPrefix(fileID, "F")
	n := 0
	for n < len(s) && s[n] >= '0' && s[n] <= '9' {
		n++
	}
	if n < 14 {
		return fileID
	}
	return s[:8] + "T" + s[8:14]
}
