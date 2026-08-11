// Package textmerge is the unified-diff and 3-way-merge plumbing behind
// edit-preserving analysis. It is pure Go — no PATH tool — over two
// zero-dependency libraries: line diff/patch (Diff/Unapply) via
// github.com/njchilds90/go-diffpatch and 3-way merge (Merge) via
// github.com/epiclabs-io/diff3. It is pure text-in/text-out and knows nothing
// about the archive layout; callers own content normalization.
package textmerge

import (
	"encoding/json"
	"strings"

	"github.com/epiclabs-io/diff3"
	diffpatch "github.com/njchilds90/go-diffpatch"
)

// Diff returns a serialized patch turning old into new, or "" when they are
// equal. The patch is a JSON-encoded diffpatch.Patch — plaintext and
// self-contained — that Unapply reverses.
func Diff(old, new string) (string, error) {
	if old == new {
		return "", nil
	}
	patch, err := diffpatch.Diff(old, new)
	if err != nil {
		return "", err
	}
	b, err := json.MarshalIndent(patch, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// Unapply reverse-applies a Diff-produced patch to current, recovering old.
// An empty diff returns current unchanged; a patch that no longer matches
// current is an error.
func Unapply(current, diff string) (string, error) {
	if diff == "" {
		return current, nil
	}
	var patch diffpatch.Patch
	if err := json.Unmarshal([]byte(diff), &patch); err != nil {
		return "", err
	}
	return diffpatch.Revert(current, patch)
}

// Merge 3-way-merges mine and theirs against their common base, returning the
// merged text and whether it contains conflict markers. The marker labels name
// the sides as the user sees them: "edited" (mine) vs "reanalyzed" (theirs).
// Splitting on "\n" and rejoining is a byte-faithful round-trip (trailing
// newline included), so a clean merge preserves the inputs' shape exactly.
func Merge(base, mine, theirs string) (merged string, conflicts bool, err error) {
	res := diff3.Diff3MergeWithOptions(
		strings.Split(mine, "\n"),   // a = mine
		strings.Split(base, "\n"),   // o = base
		strings.Split(theirs, "\n"), // b = theirs
		diff3.MergeOptions{Algorithm: diff3.DiffAlgorithmMyers, ExcludeFalseConflicts: true},
	)
	var out []string
	for _, r := range res {
		if r.Conflict != nil {
			conflicts = true
			out = append(out, "<<<<<<< edited")
			out = append(out, r.Conflict.A...)
			out = append(out, "=======")
			out = append(out, r.Conflict.B...)
			out = append(out, ">>>>>>> reanalyzed")
			continue
		}
		out = append(out, r.Ok...)
	}
	return strings.Join(out, "\n"), conflicts, nil
}
