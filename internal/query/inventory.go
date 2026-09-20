package query

import (
	"sort"

	"github.com/jdlugosz963/snorg/internal/archive"
)

// ValueCount is one distinct label (a keyword or a tag) and how many pages carry
// it across the archive.
type ValueCount struct {
	Value string
	Count int
}

// Keywords enumerates the distinct device keywords across the archive with their
// page counts, sorted by value. It is the read side of `list keywords`.
func Keywords(a *archive.Archive) ([]ValueCount, error) {
	return countValues(a, func(_ archive.NoteDoc, pd archive.PageDoc) []string {
		vals := make([]string, len(pd.Keywords))
		for i, kw := range pd.Keywords {
			vals[i] = kw.Text
		}
		return vals
	})
}

// Tags enumerates the distinct snorg-managed tags across the archive with their
// page counts, sorted by value. It is the read side of `list tags`. Counts are per
// page over the effective tags, so a note-level tag counts once for each page of
// that note (and only once for a page that also carries it itself).
func Tags(a *archive.Archive) ([]ValueCount, error) {
	return countValues(a, archive.EffectiveTags)
}

// countValues walks every page and tallies the strings extract returns, counting
// each value at most once per page, then returns them sorted by value.
func countValues(a *archive.Archive, extract func(archive.NoteDoc, archive.PageDoc) []string) ([]ValueCount, error) {
	counts := map[string]int{}
	if _, err := Pages(a, func(nd archive.NoteDoc, pd archive.PageDoc) bool {
		seen := map[string]struct{}{}
		for _, v := range extract(nd, pd) {
			if _, ok := seen[v]; ok {
				continue
			}
			seen[v] = struct{}{}
			counts[v]++
		}
		return false
	}); err != nil {
		return nil, err
	}
	out := make([]ValueCount, 0, len(counts))
	for v, n := range counts {
		out = append(out, ValueCount{Value: v, Count: n})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Value < out[j].Value })
	return out, nil
}
