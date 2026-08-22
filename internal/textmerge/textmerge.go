// Package textmerge is the unified-diff and 3-way-merge plumbing behind
// edit-preserving analysis. It is pure Go — no PATH tool — over three
// zero-dependency libraries: line diff/patch computation and reverse-apply
// (Diff/Unapply) via github.com/njchilds90/go-diffpatch, unified-diff
// serialization via github.com/sourcegraph/go-diff (so the stored patch is a
// normal `--- /+++ /@@ /+/-` unified diff, not a struct dump), and 3-way merge
// (Merge) via github.com/epiclabs-io/diff3. It is pure text-in/text-out and
// knows nothing about the archive layout; callers own content normalization.
package textmerge

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/epiclabs-io/diff3"
	diffpatch "github.com/njchilds90/go-diffpatch"
	"github.com/sourcegraph/go-diff/diff"
)

// Diff returns a unified diff turning old into new, or "" when they are equal.
// go-diffpatch computes the line diff (Myers, 1 line of context — `-U1`, the
// tersest go-diffpatch supports: Context <= 0 means unlimited context, a
// full-file diff) and sourcegraph/go-diff renders it as a standard unified
// diff — plaintext, self-contained and human-readable — that Unapply reverses.
func Diff(old, new string) (string, error) {
	if old == new {
		return "", nil
	}
	patch, err := diffpatch.DiffWithOptions(old, new, diffpatch.Options{Context: 1})
	if err != nil {
		return "", err
	}
	b, err := diff.PrintFileDiff(patchToFileDiff(patch))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// Unapply reverse-applies a Diff-produced unified diff to current, recovering
// old. An empty diff returns current unchanged; a diff that no longer matches
// current is an error.
func Unapply(current, unified string) (string, error) {
	if unified == "" {
		return current, nil
	}
	fd, err := diff.ParseFileDiff([]byte(unified))
	if err != nil {
		return "", err
	}
	patch, err := fileDiffToPatch(fd)
	if err != nil {
		return "", err
	}
	return diffpatch.Revert(current, patch)
}

// patchToFileDiff maps a go-diffpatch patch onto sourcegraph/go-diff's
// serializable shape: one hunk per patch hunk, each Body a concatenation of the
// change lines prefixed ' '/'+'/'-'. The names label the sides the diff records
// (AI base → effective, edited content).
func patchToFileDiff(patch diffpatch.Patch) *diff.FileDiff {
	fd := &diff.FileDiff{OrigName: "base", NewName: "edited"}
	for _, h := range patch.Hunks {
		var body strings.Builder
		var origLines, newLines int32
		for _, c := range h.Changes {
			switch c.Operation {
			case diffpatch.OperationEqual:
				body.WriteByte(' ')
				origLines++
				newLines++
			case diffpatch.OperationInsert:
				body.WriteByte('+')
				newLines++
			case diffpatch.OperationDelete:
				body.WriteByte('-')
				origLines++
			}
			body.WriteString(c.Text) // Text already carries its trailing newline.
		}
		fd.Hunks = append(fd.Hunks, &diff.Hunk{
			OrigStartLine: int32(h.SourceStart) + 1,
			OrigLines:     origLines,
			NewStartLine:  int32(h.TargetStart) + 1,
			NewLines:      newLines,
			Body:          []byte(body.String()),
		})
	}
	return fd
}

// fileDiffToPatch is the inverse: it reconstructs a go-diffpatch patch from a
// parsed unified diff so Revert can reverse-apply it. It reads only the fields
// Revert needs (hunk starts + the prefixed change lines); line counts are left
// zero.
func fileDiffToPatch(fd *diff.FileDiff) (diffpatch.Patch, error) {
	var patch diffpatch.Patch
	for _, h := range fd.Hunks {
		hunk := diffpatch.Hunk{
			SourceStart: int(h.OrigStartLine) - 1,
			TargetStart: int(h.NewStartLine) - 1,
		}
		lines := strings.SplitAfter(string(h.Body), "\n")
		if n := len(lines); n > 0 && lines[n-1] == "" {
			lines = lines[:n-1] // SplitAfter leaves a trailing "" after the final newline.
		}
		for _, l := range lines {
			var op diffpatch.Operation
			switch l[0] {
			case ' ':
				op = diffpatch.OperationEqual
			case '+':
				op = diffpatch.OperationInsert
			case '-':
				op = diffpatch.OperationDelete
			default:
				return diffpatch.Patch{}, fmt.Errorf("textmerge: bad unified-diff line prefix %q", l)
			}
			hunk.Changes = append(hunk.Changes, diffpatch.Change{Operation: op, Text: l[1:]})
		}
		patch.Hunks = append(patch.Hunks, hunk)
	}
	return patch, nil
}

// ConvertLegacyDiff upgrades a pre-unified `.md.diff` — a JSON-encoded
// diffpatch.Patch, the old on-disk form — to the current unified-diff form.
// wasLegacy is false (and s is returned unchanged) when s is not that JSON form
// (already a unified diff, or empty), so callers can sniff-and-skip. A JSON patch
// with no hunks (degenerate — no divergence) yields ("", true, nil) so the caller
// can drop the sidecar.
func ConvertLegacyDiff(s string) (unified string, wasLegacy bool, err error) {
	if !strings.HasPrefix(strings.TrimSpace(s), "{") {
		return s, false, nil // already unified (or empty): nothing to convert.
	}
	var patch diffpatch.Patch
	if err := json.Unmarshal([]byte(s), &patch); err != nil {
		return "", false, err // looked like JSON but was not a patch.
	}
	if len(patch.Hunks) == 0 {
		return "", true, nil
	}
	b, err := diff.PrintFileDiff(patchToFileDiff(patch))
	if err != nil {
		return "", false, err
	}
	return string(b), true, nil
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
