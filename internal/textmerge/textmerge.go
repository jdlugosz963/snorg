// Package textmerge is the unified-diff and 3-way-merge plumbing behind
// edit-preserving analysis, shelling out to git — an external PATH tool like
// supernote-tool and pandoc, not a go.mod dependency. It is pure text-in/
// text-out and knows nothing about the archive layout; callers own content
// normalization.
package textmerge

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Available reports whether git is on PATH. Callers check it up front so a
// missing tool surfaces before any editor session or LLM call is spent.
func Available() error {
	if _, err := exec.LookPath("git"); err != nil {
		return fmt.Errorf("git not found on PATH (required for edit-preserving analysis)")
	}
	return nil
}

// Diff returns a unified diff turning old into new, or "" when they are equal.
// The diff is self-contained (fixed file names, no temp paths) and is exactly
// what Unapply reverses.
func Diff(old, new string) (string, error) {
	dir, err := os.MkdirTemp("", "snorg-textmerge-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	// Same inner name on both sides keeps the headers deterministic
	// (a/a/page.md, b/b/page.md) and reverse-applicable with -p2.
	if err := writeTo(dir, "a/page.md", old); err != nil {
		return "", err
	}
	if err := writeTo(dir, "b/page.md", new); err != nil {
		return "", err
	}
	out, code, stderr, err := runGit(dir, "diff", "--no-index", "--no-color", "--no-ext-diff", "--", "a/page.md", "b/page.md")
	switch {
	case err != nil:
		return "", err
	case code == 0: // no differences
		return "", nil
	case code == 1:
		return out, nil
	default:
		return "", fmt.Errorf("git diff: exit %d: %s", code, stderr)
	}
}

// Unapply reverse-applies a Diff-produced diff to current, recovering old.
// An empty diff returns current unchanged; a diff that no longer matches
// current is an error.
func Unapply(current, diff string) (string, error) {
	if diff == "" {
		return current, nil
	}
	dir, err := os.MkdirTemp("", "snorg-textmerge-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	if err := writeTo(dir, "page.md", current); err != nil {
		return "", err
	}
	if err := writeTo(dir, "patch.diff", diff); err != nil {
		return "", err
	}
	// -p2 strips the a/a/, b/b/ header prefixes down to page.md.
	_, code, stderr, err := runGit(dir, "apply", "-R", "-p2", "patch.diff")
	if err != nil {
		return "", err
	}
	if code != 0 {
		return "", fmt.Errorf("git apply -R: exit %d: %s", code, stderr)
	}
	b, err := os.ReadFile(filepath.Join(dir, "page.md"))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// Merge 3-way-merges mine and theirs against their common base, returning the
// merged text and whether it contains conflict markers. The marker labels name
// the sides as the user sees them: "edited" (mine) vs "reanalyzed" (theirs).
func Merge(base, mine, theirs string) (merged string, conflicts bool, err error) {
	dir, err := os.MkdirTemp("", "snorg-textmerge-")
	if err != nil {
		return "", false, err
	}
	defer os.RemoveAll(dir)
	for name, content := range map[string]string{"base.md": base, "mine.md": mine, "theirs.md": theirs} {
		if err := writeTo(dir, name, content); err != nil {
			return "", false, err
		}
	}
	out, code, stderr, err := runGit(dir, "merge-file", "-p",
		"-L", "edited", "-L", "base", "-L", "reanalyzed",
		"mine.md", "base.md", "theirs.md")
	if err != nil {
		return "", false, err
	}
	// merge-file exits with the number of conflicts (capped at 127); >127 is
	// a real failure.
	if code < 0 || code > 127 {
		return "", false, fmt.Errorf("git merge-file: exit %d: %s", code, stderr)
	}
	return out, code > 0, nil
}

// runGit executes git in dir with user/system config disabled, so personal
// diff and merge settings never leak into stored artifacts. A non-zero exit
// comes back as code (with stderr for context), not as err; err is reserved
// for failures to run git at all.
func runGit(dir string, args ...string) (stdout string, code int, stderr string, err error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		exit, ok := err.(*exec.ExitError)
		if !ok {
			return "", 0, "", fmt.Errorf("git %s: %w", args[0], err)
		}
		return out.String(), exit.ExitCode(), strings.TrimSpace(errb.String()), nil
	}
	return out.String(), 0, strings.TrimSpace(errb.String()), nil
}

func writeTo(dir, name, content string) error {
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}
