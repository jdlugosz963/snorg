package archive

import "strings"

// formatSVG rewrites a rendered SVG into a diff-friendly layout: each <path>
// element starts on its own line, and inside a path the `d` attribute is broken
// so every drawing command (M/L/C/Z/...) sits on its own line, aligned under the
// start of the path data. supernote-tool emits the whole SVG as a single huge
// line, so editing one stroke rewrote everything; with this layout a changed
// stroke touches only its own lines, keeping VCS diffs small. The transform is
// deterministic and idempotent, so unchanged content re-renders byte-identically.
func formatSVG(b []byte) []byte {
	s := string(b)
	var out strings.Builder
	out.Grow(len(s) + len(s)/8)
	var last byte
	for {
		i := strings.Index(s, "<path")
		if i < 0 {
			out.WriteString(s)
			break
		}
		out.WriteString(s[:i])
		if i > 0 {
			last = s[i-1]
		}
		if out.Len() > 0 && last != '\n' { // each <path> starts a fresh line
			out.WriteByte('\n')
			last = '\n'
		}
		s = s[i:]
		end := strings.IndexByte(s, '>')
		if end < 0 { // malformed; emit the remainder verbatim
			out.WriteString(s)
			break
		}
		elem := formatPath(s[:end+1])
		out.WriteString(elem)
		last = elem[len(elem)-1]
		s = s[end+1:]
	}
	return []byte(out.String())
}

// formatPath reflows the `d` attribute of a single <path .../> element, leaving
// every other attribute untouched. Continuation lines are indented to align
// under the first coordinate (the column right after `d="`).
func formatPath(elem string) string {
	di := strings.Index(elem, ` d="`)
	if di < 0 {
		return elem
	}
	start := di + len(` d="`)
	q := strings.IndexByte(elem[start:], '"')
	if q < 0 {
		return elem
	}
	head := elem[:start]          // `<path ... d="`
	data := elem[start : start+q] // path command stream
	rest := elem[start+q:]        // closing `"` + remaining attributes + `/>`
	indent := strings.Repeat(" ", len(head))

	var b strings.Builder
	first := true
	for _, f := range strings.Fields(data) {
		if isPathCommand(f) {
			if first {
				first = false
			} else {
				b.WriteByte('\n')
				b.WriteString(indent)
			}
			b.WriteString(f)
		} else {
			b.WriteByte(' ')
			b.WriteString(f)
		}
	}
	return head + b.String() + rest
}

// isPathCommand reports whether token is an SVG path command (M/L/C/Z/...) rather
// than a coordinate. Commands are standalone letter tokens; coordinates start
// with a digit, sign, or dot (exponents like 1e5 start with a digit).
func isPathCommand(token string) bool {
	if token == "" {
		return false
	}
	c := token[0]
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
}
