package edit

// The analyze-edit buffer is a single editable document: an optional header of
// per-region name markers followed by the page content. Each title/link gets a
// marker line whose trailing context ((h{level}), the link target) is purely
// informational — snorg identifies the region only by the marker's kind and
// 1-based index (their order in the page JSON). The content follows the first
// <!-- content --> marker and is taken verbatim, so it may itself contain lines
// that look like markers. A page with no titles and no links has no header at
// all: the buffer is just the content, exactly as before.

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/jdlugosz963/snorg/internal/archive"
)

// serialize renders the editable buffer for pd's regions and content. With no
// regions it returns content unchanged (the header would be empty noise).
func serialize(pd archive.PageDoc, content string) string {
	if len(pd.Titles) == 0 && len(pd.Links) == 0 {
		return content
	}
	var b strings.Builder
	for i, t := range pd.Titles {
		name := ""
		if t.Analysis != nil {
			name = t.Analysis.Name
		}
		fmt.Fprintf(&b, "<!-- title %d (h%d) -->\n%s\n", i+1, t.Level, name)
	}
	for i, l := range pd.Links {
		ctx := l.Name
		if ctx == "" {
			ctx = l.TargetPageID
		}
		name := ""
		if l.Analysis != nil {
			name = l.Analysis.Name
		}
		fmt.Fprintf(&b, "<!-- link %d → %s -->\n%s\n", i+1, ctx, name)
	}
	b.WriteString("\n<!-- content -->\n")
	b.WriteString(content)
	return b.String()
}

// parse splits an edited buffer back into per-region names and the content. With
// no regions (nTitles == nLinks == 0) the whole buffer is content. Otherwise a
// <!-- content --> marker is required and the title/link markers must cover
// exactly indices 1..nTitles and 1..nLinks with no gaps or duplicates; anything
// else is a user format error and nothing is written by the caller.
func parse(buf string, nTitles, nLinks int) (titleNames, linkNames []string, content string, err error) {
	if nTitles == 0 && nLinks == 0 {
		return nil, nil, buf, nil
	}
	lines := strings.Split(buf, "\n")

	type entry struct {
		kind string
		idx  int
		name []string
	}
	var entries []entry
	contentStart := -1
	for i, line := range lines {
		if kind, idx, ok := parseMarker(line); ok {
			if kind == "content" {
				contentStart = i + 1
				break
			}
			entries = append(entries, entry{kind: kind, idx: idx})
			continue
		}
		if len(entries) > 0 {
			e := &entries[len(entries)-1]
			e.name = append(e.name, line)
		}
	}
	if contentStart < 0 {
		return nil, nil, "", fmt.Errorf("missing <!-- content --> marker")
	}

	titleNames = make([]string, nTitles)
	linkNames = make([]string, nLinks)
	seenT := make([]bool, nTitles)
	seenL := make([]bool, nLinks)
	for _, e := range entries {
		name := strings.TrimSpace(strings.Join(e.name, "\n"))
		switch e.kind {
		case "title":
			if e.idx < 1 || e.idx > nTitles {
				return nil, nil, "", fmt.Errorf("title marker index %d out of range 1..%d", e.idx, nTitles)
			}
			if seenT[e.idx-1] {
				return nil, nil, "", fmt.Errorf("duplicate title marker %d", e.idx)
			}
			seenT[e.idx-1] = true
			titleNames[e.idx-1] = name
		case "link":
			if e.idx < 1 || e.idx > nLinks {
				return nil, nil, "", fmt.Errorf("link marker index %d out of range 1..%d", e.idx, nLinks)
			}
			if seenL[e.idx-1] {
				return nil, nil, "", fmt.Errorf("duplicate link marker %d", e.idx)
			}
			seenL[e.idx-1] = true
			linkNames[e.idx-1] = name
		}
	}
	for i, s := range seenT {
		if !s {
			return nil, nil, "", fmt.Errorf("missing title marker %d", i+1)
		}
	}
	for i, s := range seenL {
		if !s {
			return nil, nil, "", fmt.Errorf("missing link marker %d", i+1)
		}
	}

	content = strings.Join(lines[contentStart:], "\n")
	return titleNames, linkNames, content, nil
}

// parseMarker recognizes a marker line and its kind/index, ignoring everything
// after the index (the informational context). "content" carries no index.
func parseMarker(line string) (kind string, idx int, ok bool) {
	s := strings.TrimSpace(line)
	if !strings.HasPrefix(s, "<!--") || !strings.HasSuffix(s, "-->") {
		return "", 0, false
	}
	inner := strings.TrimSpace(s[len("<!--") : len(s)-len("-->")])
	fields := strings.Fields(inner)
	if len(fields) == 0 {
		return "", 0, false
	}
	switch fields[0] {
	case "content":
		return "content", 0, true
	case "title", "link":
		if len(fields) < 2 {
			return "", 0, false
		}
		n, err := strconv.Atoi(fields[1])
		if err != nil {
			return "", 0, false
		}
		return fields[0], n, true
	}
	return "", 0, false
}
