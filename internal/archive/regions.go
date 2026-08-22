package archive

import (
	"fmt"
	"strings"
)

// A templated page's <PAGEID>.md sidecar holds its per-box transcriptions as
// id-keyed sections (in place of a normal page's free-form content), reusing the
// analyze-edit marker convention:
//
//	<!-- region <id> (<label>) -->
//	<prose for that box, verbatim, multi-line>
//	<!-- region <id2> (<label2>) -->
//	...
//
// snorg keys a section by its id only; the label is informational (for the human
// reading the file/diff) — the authoritative label/rect/prompt live in the
// config's templates: section, and retrieve resolves them from there, never from
// the sidecar. Text between two markers is taken verbatim (leading/trailing blank
// lines trimmed), so it may contain any markdown except a line that itself looks
// like a region marker.
//
// Canonical order (what analyze regenerates, so the 3-way-merge base stays
// stable): sections in the template's box order, then any tombstoned sections —
// ids present in the sidecar but no longer in the config — appended in their
// existing order, never deleted (text is never silently lost) and ignored by
// export.

// RegionSection is one id-keyed section of a region-section document. Label is the
// cosmetic marker label; Text is the verbatim transcription (blank-line trimmed).
type RegionSection struct {
	ID    string
	Label string
	Text  string
}

// ParseRegions splits a region-section document into its sections in document order.
// A line is a section boundary only when it parses as a region marker; everything
// up to the next boundary is that section's verbatim text. Leading content before
// the first marker (there should be none in canonical form) is dropped.
func ParseRegions(md string) []RegionSection {
	if strings.TrimSpace(md) == "" {
		return nil
	}
	lines := strings.Split(md, "\n")
	var sections []RegionSection
	var cur *RegionSection
	var text []string
	flush := func() {
		if cur != nil {
			cur.Text = strings.Trim(strings.Join(text, "\n"), "\n")
			sections = append(sections, *cur)
		}
		text = nil
	}
	for _, line := range lines {
		if id, label, ok := parseRegionMarker(line); ok {
			flush()
			cur = &RegionSection{ID: id, Label: label}
			continue
		}
		if cur != nil {
			text = append(text, line)
		}
	}
	flush()
	return sections
}

// AssembleRegions renders sections back to the canonical sidecar form. An empty
// slice yields "" (no sidecar). Each section is a marker line plus its text; a
// blank line separates sections for readability.
func AssembleRegions(sections []RegionSection) string {
	if len(sections) == 0 {
		return ""
	}
	var b strings.Builder
	for i, s := range sections {
		if i > 0 {
			b.WriteString("\n")
		}
		if s.Label != "" {
			fmt.Fprintf(&b, "<!-- region %s (%s) -->\n", s.ID, s.Label)
		} else {
			fmt.Fprintf(&b, "<!-- region %s -->\n", s.ID)
		}
		if s.Text != "" {
			b.WriteString(s.Text)
			b.WriteString("\n")
		}
	}
	return b.String()
}

// RegionText returns the text of the section with the given id, or "".
func RegionText(sections []RegionSection, id string) string {
	for _, s := range sections {
		if s.ID == id {
			return s.Text
		}
	}
	return ""
}

// parseRegionMarker recognizes a `<!-- region <id> [(label)] -->` line. The id is
// the first field after "region"; the remainder (a parenthesized label) is
// informational and returned stripped of its surrounding parens.
func parseRegionMarker(line string) (id, label string, ok bool) {
	s := strings.TrimSpace(line)
	if !strings.HasPrefix(s, "<!--") || !strings.HasSuffix(s, "-->") {
		return "", "", false
	}
	inner := strings.TrimSpace(s[len("<!--") : len(s)-len("-->")])
	fields := strings.Fields(inner)
	if len(fields) < 2 || fields[0] != "region" {
		return "", "", false
	}
	id = fields[1]
	rest := strings.TrimSpace(strings.TrimPrefix(inner, "region"))
	rest = strings.TrimSpace(strings.TrimPrefix(rest, id))
	rest = strings.TrimPrefix(rest, "(")
	rest = strings.TrimSuffix(rest, ")")
	return id, strings.TrimSpace(rest), true
}
