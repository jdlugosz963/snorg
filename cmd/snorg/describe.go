package main

// Turning the change vocabulary into the lines the CLI prints. One place, so an
// analyzed page, an analyze-edit save and an ingested note all describe a
// TextChange with the same words. Pure functions over the public structs — no I/O,
// no client — so they are testable on hand-built values.

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/jdlugosz963/snorg/pkg/snorg"
)

// maxDiffLines caps a verbose diff block; the rest is summarized by a tail line.
const maxDiffLines = 20

// describeText renders a change as one clause: the kind, "conflict" when it carries
// merge markers, and the line counts. The counts come from snorg.TextStat rather
// than from a stored field — TextChange carries the text, not a summary of it — and
// are omitted if that diff fails, which two in-memory strings essentially never do.
func describeText(c snorg.TextChange) string {
	if c.Kind == "" {
		return ""
	}
	parts := []string{string(c.Kind)}
	if c.Conflicts {
		parts = append(parts, "conflict")
	}
	if added, removed, err := snorg.TextStat(c); err == nil && (added > 0 || removed > 0) {
		parts = append(parts, fmt.Sprintf("+%d/-%d", added, removed))
	}
	return strings.Join(parts, " ")
}

// describeRegions summarizes a templated page's boxes as counts per state, in a
// fixed order so the line is deterministic. The cost axis (Skipped/Blank — what the
// box did or did not cost) and the text axis (what moved) are counted separately,
// which is exactly why they are separate fields.
func describeRegions(rs []snorg.RegionResult) string {
	if len(rs) == 0 {
		return ""
	}
	counts := map[string]int{}
	conflicts := false
	for _, r := range rs {
		switch {
		case r.Skipped:
			counts["skipped"]++
		case r.Blank:
			counts["blank"]++
		default:
			counts[string(r.Text.Kind)]++
		}
		conflicts = conflicts || r.Text.Conflicts
	}
	var parts []string
	for _, k := range []string{"new", "updated", "cleared", "unchanged", "skipped", "blank"} {
		if n := counts[k]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, k))
		}
	}
	out := "regions " + strings.Join(parts, ", ")
	if conflicts {
		out += "; conflict"
	}
	return out
}

// describeAnalyze is one analyzed page's summary line body: what happened to its
// text, then the tail clauses, each omitted at zero — so an untouched page reads
// just "unchanged" and a free one never claims to have cost calls.
func describeAnalyze(r snorg.AnalyzeResult) string {
	if r.Skipped {
		return "skipped"
	}
	var head string
	switch {
	case len(r.Regions) > 0:
		head = describeRegions(r.Regions)
	case r.Content != nil:
		head = describeText(*r.Content)
	}
	parts := []string{}
	if head != "" {
		parts = append(parts, head)
	}
	parts = append(parts, countClauses(
		clause{len(r.Names), "name", "names"},
		clause{len(r.Fields), "field", "fields"},
		clause{r.Calls, "call", "calls"},
	)...)
	if len(parts) == 0 {
		return "unchanged"
	}
	return strings.Join(parts, ", ")
}

// analyzeDetail is the -v block under an analyzed page: the content (or per-region)
// change with its capped diff, each renamed title/link, and the regenerated fields.
func analyzeDetail(r snorg.AnalyzeResult) []string {
	if r.Skipped {
		return nil
	}
	var out []string
	if r.Content != nil && r.Content.Changed() {
		out = append(out, "content: "+describeText(*r.Content))
		out = append(out, indent(diffLines(*r.Content))...)
	}
	for _, rr := range r.Regions {
		out = append(out, "region "+regionLabel(rr.RegionChange)+": "+regionState(rr))
		if rr.Text.Changed() {
			out = append(out, indent(diffLines(rr.Text))...)
		}
	}
	out = append(out, nameLines(r.Names)...)
	for _, f := range sorted(r.Fields) {
		out = append(out, "field: "+f)
	}
	return out
}

// describeEdit is an analyze-edit save's summary line body.
func describeEdit(e snorg.PageEdit) string {
	parts := []string{describeText(e.Content)}
	if n := changedRegions(e.Regions); n > 0 {
		parts = append(parts, fmt.Sprintf("%d region(s)", n))
	}
	parts = append(parts, countClauses(clause{len(e.Names), "name", "names"})...)
	return strings.Join(parts, ", ")
}

// editDetail is the -v block under an analyze-edit save.
func editDetail(e snorg.PageEdit) []string {
	var out []string
	if e.Content.Changed() {
		out = append(out, "content: "+describeText(e.Content))
		out = append(out, indent(diffLines(e.Content))...)
	}
	for _, rc := range e.Regions {
		if !rc.Text.Changed() {
			continue
		}
		out = append(out, "region "+regionLabel(rc)+": "+describeText(rc.Text))
		out = append(out, indent(diffLines(rc.Text))...)
	}
	return append(out, nameLines(e.Names)...)
}

// describeIngest is one ingested note's summary line: its size, where it landed and
// what the incremental reconcile actually changed there. The reporter's id column
// already names the note, and the archive root is the same for every note in the
// batch, so neither is repeated here.
func describeIngest(r snorg.IngestResult) string {
	return fmt.Sprintf("%d pages -> %s: %s", len(r.Note.Pages), r.Note.FileID, describeReport(r.Report))
}

// describeReport turns a WriteReport into the change tail. An empty report means the
// note was already current — which is worth saying, not worth hiding.
func describeReport(rep *snorg.WriteReport) string {
	if rep == nil {
		return "current"
	}
	var parts []string
	if n := len(rep.Pages); n > 0 {
		parts = append(parts, fmt.Sprintf("%d page(s) changed", n))
	}
	if rep.NoteChanged {
		parts = append(parts, "note.json changed")
	}
	if n := len(rep.Pruned); n > 0 {
		parts = append(parts, fmt.Sprintf("%d pruned", n))
	}
	if n := adoptedPages(rep); n > 0 {
		parts = append(parts, fmt.Sprintf("%d adopted", n))
	}
	if n := len(rep.Repaired); n > 0 {
		parts = append(parts, fmt.Sprintf("%d note(s) repaired", n))
	}
	if len(parts) == 0 {
		return "current"
	}
	return strings.Join(parts, ", ")
}

// ingestDetail is the -v block under an ingested note: the per-page write report the
// CLI otherwise throws away. Flags print in a fixed order, omitting the false ones.
func ingestDetail(r snorg.IngestResult) []string {
	if r.Report == nil {
		return nil
	}
	var out []string
	for _, p := range r.Report.Pages {
		var flags []string
		for _, f := range []struct {
			on   bool
			name string
		}{
			{p.New, "new"},
			{p.JSONChanged, "json"},
			{p.SVGChanged, "svg"},
			{p.BackgroundChanged, "background"},
		} {
			if f.on {
				flags = append(flags, f.name)
			}
		}
		if len(p.AdoptedFrom) > 0 {
			flags = append(flags, "adopted from "+strings.Join(p.AdoptedFrom, ", "))
		}
		if n := len(p.DroppedRegions); n > 0 {
			var kinds []string
			for _, d := range p.DroppedRegions {
				kinds = append(kinds, d.Kind)
			}
			flags = append(flags, fmt.Sprintf("dropped %d region analyses (%s)", n, strings.Join(kinds, ", ")))
		}
		out = append(out, p.PageID+": "+strings.Join(flags, ", "))
	}
	parked := map[string]bool{}
	for _, id := range r.Report.Parked {
		parked[id] = true
	}
	for _, id := range r.Report.Pruned {
		if parked[id] {
			out = append(out, "parked "+id+" (transcription kept)")
			continue
		}
		out = append(out, "pruned "+id)
	}
	for _, id := range r.Report.Repaired {
		out = append(out, "repaired "+id+"/note.json")
	}
	return out
}

// adoptedPages counts the pages this write took over from somewhere else in the
// archive because they moved between notes on the device.
func adoptedPages(rep *snorg.WriteReport) int {
	n := 0
	for _, p := range rep.Pages {
		if len(p.AdoptedFrom) > 0 {
			n++
		}
	}
	return n
}

// duplicatePages reports the PAGEIDs claimed by more than one note in this batch,
// each mapped to the claiming FILE_IDs, sorted. A page present in two .note files was
// copied, not moved: snorg keys a page by its device id, so the two notes would take
// it from each other on every ingest. Nothing can resolve that automatically — saying
// so is the only honest answer.
func duplicatePages(results []snorg.IngestResult) map[string][]string {
	claims := map[string][]string{}
	for _, r := range results {
		if r.Note == nil {
			continue
		}
		for _, p := range r.Note.Pages {
			claims[p.ID] = append(claims[p.ID], r.Note.FileID)
		}
	}
	dups := map[string][]string{}
	for id, fileIDs := range claims {
		fileIDs = sorted(fileIDs)
		fileIDs = slices.Compact(fileIDs)
		if len(fileIDs) > 1 {
			dups[id] = fileIDs
		}
	}
	return dups
}

// diffLines renders a change's before/after as unified-diff lines, capped. It
// recomputes the diff from Was/Now, which only verbose mode pays for.
func diffLines(c snorg.TextChange) []string {
	d, err := snorg.TextDiff(c)
	if err != nil || d == "" {
		return nil
	}
	lines := strings.Split(strings.TrimRight(d, "\n"), "\n")
	if len(lines) > maxDiffLines {
		rest := len(lines) - maxDiffLines
		lines = append(lines[:maxDiffLines:maxDiffLines], fmt.Sprintf("… %d more lines", rest))
	}
	return lines
}

// regionState says what a box did, cost first: a skipped or blank box is reported as
// such rather than by a text verdict it never earned.
func regionState(r snorg.RegionResult) string {
	switch {
	case r.Skipped:
		return "skipped"
	case r.Blank:
		return "blank"
	default:
		return describeText(r.Text)
	}
}

// regionLabel names a box as "id (label)", or just the id when it has no label.
func regionLabel(rc snorg.RegionChange) string {
	if rc.Label == "" {
		return rc.ID
	}
	return rc.ID + " (" + rc.Label + ")"
}

// nameLines renders each renamed title/link as "kind N: was -> now".
func nameLines(names []snorg.NameChange) []string {
	var out []string
	for _, n := range names {
		out = append(out, fmt.Sprintf("%s %d: %q -> %q", n.Kind, n.Index, n.Was, n.Now))
	}
	return out
}

// changedRegions counts the boxes whose text actually moved.
func changedRegions(rs []snorg.RegionChange) int {
	n := 0
	for _, r := range rs {
		if r.Text.Changed() {
			n++
		}
	}
	return n
}

// clause is one "N thing(s)" tail element, singular/plural.
type clause struct {
	n         int
	one, many string
}

// countClauses renders the non-zero clauses in the order given.
func countClauses(cs ...clause) []string {
	var out []string
	for _, c := range cs {
		if c.n == 0 {
			continue
		}
		word := c.many
		if c.n == 1 {
			word = c.one
		}
		out = append(out, fmt.Sprintf("%d %s", c.n, word))
	}
	return out
}

func indent(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = "  " + l
	}
	return out
}

func sorted(s []string) []string {
	out := append([]string(nil), s...)
	sort.Strings(out)
	return out
}
