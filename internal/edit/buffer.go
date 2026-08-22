package edit

// The analyze-edit buffer is a single editable document. Each title/link gets a
// marker line whose trailing context ((h{level}), the link target) is purely
// informational — snorg identifies the region only by the marker's kind and
// 1-based index (their order in the page JSON).
//
// The body after the header depends on the page kind. A normal page's body is its
// free-form content, following the first <!-- content --> marker and taken
// verbatim (so it may itself contain lines that look like markers). A templated
// page has no free-form content: its body is one <!-- region <id> (<label>) -->
// section per template box (label informational, keyed by the stable id), which
// carry the page's whole transcription — there is no <!-- content --> marker, and
// one appearing is a user error. A page with no titles, links or regions has no
// header at all: the buffer is just the content, exactly as before.

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/jdlugosz963/snorg/internal/archive"
)

// serialize renders the editable buffer for pd's regions and content. regions are
// the template-box sections (nil for a non-templated page); when present they are
// the body and content is ignored (a templated page has no free-form content).
// With no titles, links or regions it returns content unchanged (the header would
// be empty noise).
func serialize(pd archive.PageDoc, content string, regions []archive.RegionSection) string {
	if len(pd.Titles) == 0 && len(pd.Links) == 0 && len(regions) == 0 {
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
	if len(regions) > 0 {
		// Templated page: the region sections are the body, no content marker.
		for _, r := range regions {
			if r.Label != "" {
				fmt.Fprintf(&b, "<!-- region %s (%s) -->\n%s\n", r.ID, r.Label, r.Text)
			} else {
				fmt.Fprintf(&b, "<!-- region %s -->\n%s\n", r.ID, r.Text)
			}
		}
		return b.String()
	}
	b.WriteString("\n<!-- content -->\n")
	b.WriteString(content)
	return b.String()
}

// parse splits an edited buffer back into per-region names, per-box region texts
// and the content. With no regions of any kind (nTitles == nLinks == 0 and no
// regionIDs) the whole buffer is content. A templated page (len(regionIDs) > 0)
// has no free-form content: its body is the region sections, so no <!-- content -->
// marker is expected and one appearing is an error; content comes back "". Any
// other page with markers requires a <!-- content --> marker. In every case the
// title/link markers must cover exactly indices 1..nTitles and 1..nLinks, and the
// region markers must cover exactly regionIDs (as a set) — no gaps, duplicates or
// unknown ids. Anything else is a user format error and nothing is written by the
// caller.
func parse(buf string, nTitles, nLinks int, regionIDs []string) (titleNames, linkNames []string, regionTexts map[string]string, content string, err error) {
	if nTitles == 0 && nLinks == 0 && len(regionIDs) == 0 {
		return nil, nil, nil, buf, nil
	}
	templated := len(regionIDs) > 0
	lines := strings.Split(buf, "\n")

	type entry struct {
		kind string
		key  string // index token (title/link) or id (region)
		body []string
	}
	var entries []entry
	contentStart := -1
	for i, line := range lines {
		if kind, key, ok := parseMarker(line); ok {
			if kind == "content" {
				if templated {
					return nil, nil, nil, "", fmt.Errorf("a templated page has no content section; remove the <!-- content --> marker")
				}
				contentStart = i + 1
				break
			}
			entries = append(entries, entry{kind: kind, key: key})
			continue
		}
		if len(entries) > 0 {
			e := &entries[len(entries)-1]
			e.body = append(e.body, line)
		}
	}
	if !templated && contentStart < 0 {
		return nil, nil, nil, "", fmt.Errorf("missing <!-- content --> marker")
	}

	titleNames = make([]string, nTitles)
	linkNames = make([]string, nLinks)
	regionTexts = map[string]string{}
	seenT := make([]bool, nTitles)
	seenL := make([]bool, nLinks)
	wantRegion := map[string]bool{}
	seenRegion := map[string]bool{}
	for _, id := range regionIDs {
		wantRegion[id] = true
	}
	for _, e := range entries {
		text := strings.Trim(strings.Join(e.body, "\n"), "\n")
		switch e.kind {
		case "title", "link":
			idx, cerr := strconv.Atoi(e.key)
			if cerr != nil {
				return nil, nil, nil, "", fmt.Errorf("%s marker has non-numeric index %q", e.kind, e.key)
			}
			name := strings.TrimSpace(text)
			if e.kind == "title" {
				if idx < 1 || idx > nTitles {
					return nil, nil, nil, "", fmt.Errorf("title marker index %d out of range 1..%d", idx, nTitles)
				}
				if seenT[idx-1] {
					return nil, nil, nil, "", fmt.Errorf("duplicate title marker %d", idx)
				}
				seenT[idx-1] = true
				titleNames[idx-1] = name
			} else {
				if idx < 1 || idx > nLinks {
					return nil, nil, nil, "", fmt.Errorf("link marker index %d out of range 1..%d", idx, nLinks)
				}
				if seenL[idx-1] {
					return nil, nil, nil, "", fmt.Errorf("duplicate link marker %d", idx)
				}
				seenL[idx-1] = true
				linkNames[idx-1] = name
			}
		case "region":
			if !wantRegion[e.key] {
				return nil, nil, nil, "", fmt.Errorf("unknown region marker %q", e.key)
			}
			if seenRegion[e.key] {
				return nil, nil, nil, "", fmt.Errorf("duplicate region marker %q", e.key)
			}
			seenRegion[e.key] = true
			regionTexts[e.key] = text
		}
	}
	for i, s := range seenT {
		if !s {
			return nil, nil, nil, "", fmt.Errorf("missing title marker %d", i+1)
		}
	}
	for i, s := range seenL {
		if !s {
			return nil, nil, nil, "", fmt.Errorf("missing link marker %d", i+1)
		}
	}
	for _, id := range regionIDs {
		if !seenRegion[id] {
			return nil, nil, nil, "", fmt.Errorf("missing region marker %q", id)
		}
	}

	if !templated {
		content = strings.Join(lines[contentStart:], "\n")
	}
	return titleNames, linkNames, regionTexts, content, nil
}

// parseMarker recognizes a marker line and its kind plus key, ignoring everything
// after the key (the informational context). "content" carries no key; "title"/
// "link" carry a numeric index (as a string); "region" carries a string id.
func parseMarker(line string) (kind, key string, ok bool) {
	s := strings.TrimSpace(line)
	if !strings.HasPrefix(s, "<!--") || !strings.HasSuffix(s, "-->") {
		return "", "", false
	}
	inner := strings.TrimSpace(s[len("<!--") : len(s)-len("-->")])
	fields := strings.Fields(inner)
	if len(fields) == 0 {
		return "", "", false
	}
	switch fields[0] {
	case "content":
		return "content", "", true
	case "title", "link", "region":
		if len(fields) < 2 {
			return "", "", false
		}
		return fields[0], fields[1], true
	}
	return "", "", false
}
