package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jdlugosz963/snorg/internal/archive"
	"github.com/jdlugosz963/snorg/internal/snote"
)

func TestConfigPaths(t *testing.T) {
	dir := t.TempDir()
	archiveCfg := filepath.Join(dir, archiveConfigName)
	if err := os.WriteFile(archiveCfg, []byte("provider:\n  model: archive\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	userCfg := filepath.Join(t.TempDir(), archiveConfigName)
	if err := os.WriteFile(userCfg, []byte("archive: /somewhere\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cli := []string{"/tmp/a.yaml", "/tmp/b.yaml"}

	// All layers present: XDG user config, then archive config, then -c files
	// (each later layer wins the merge).
	got := configPaths(userCfg, dir, cli, false, false)
	want := append([]string{userCfg, archiveCfg}, cli...)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("all layers: got %v, want %v", got, want)
	}

	// Opt-outs are independent.
	if got := configPaths(userCfg, dir, cli, true, false); !reflect.DeepEqual(got, append([]string{archiveCfg}, cli...)) {
		t.Errorf("no-user-config: got %v", got)
	}
	if got := configPaths(userCfg, dir, cli, false, true); !reflect.DeepEqual(got, append([]string{userCfg}, cli...)) {
		t.Errorf("no-archive-config: got %v", got)
	}
	if got := configPaths(userCfg, dir, cli, true, true); !reflect.DeepEqual(got, cli) {
		t.Errorf("both opt-outs: got %v, want %v", got, cli)
	}

	// Empty user path and missing archive config: only the -c files, no error.
	if got := configPaths("", t.TempDir(), cli, false, false); !reflect.DeepEqual(got, cli) {
		t.Errorf("missing files: got %v, want %v", got, cli)
	}

	// A directory named config.yaml is not treated as a config file.
	dir2 := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir2, archiveConfigName), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := configPaths("", dir2, cli, false, false); !reflect.DeepEqual(got, cli) {
		t.Errorf("config.yaml dir: got %v, want %v", got, cli)
	}
}

func TestExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home dir: %v", err)
	}
	cases := map[string]string{
		"~":          home,
		"~/notes/sn": filepath.Join(home, "notes/sn"),
		"/abs/notes": "/abs/notes",
		"rel/notes":  "rel/notes",
		"~notuser/x": "~notuser/x", // ~ not followed by / is left alone
	}
	for in, want := range cases {
		if got := expandHome(in); got != want {
			t.Errorf("expandHome(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseDateSpec(t *testing.T) {
	today := time.Now().Format("20060102")
	yesterday := time.Now().AddDate(0, 0, -1).Format("20060102")
	cases := []struct {
		spec     string
		from, to string
		wantErr  bool
	}{
		{"today", today, today, false},
		{"yesterday", yesterday, yesterday, false},
		{"2026-07-22", "20260722", "20260722", false},
		{"2026-07-01..2026-07-22", "20260701", "20260722", false},
		{"..2026-07-22", "", "20260722", false},
		{"2026-07-01..", "20260701", "", false},
		{"..", "", "", true},
		{"2026-13-01", "", "", true},
		{"not-a-date", "", "", true},
	}
	for _, tc := range cases {
		from, to, err := parseDateSpec(tc.spec)
		if (err != nil) != tc.wantErr {
			t.Errorf("parseDateSpec(%q) err = %v, wantErr %v", tc.spec, err, tc.wantErr)
			continue
		}
		if err == nil && (from != tc.from || to != tc.to) {
			t.Errorf("parseDateSpec(%q) = (%q,%q), want (%q,%q)", tc.spec, from, to, tc.from, tc.to)
		}
	}
}

func TestRootDispatch(t *testing.T) {
	ctx := context.Background()
	arch := t.TempDir()

	// Isolate the XDG user config: point it at an empty dir so the developer's
	// real ~/.config/snorg/config.yaml can't leak an archive: default into tests.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	// No -a and no user config: the resolver reports no archive path.
	if err := root().Run(ctx, []string{"snorg", "list"}); err == nil || !strings.Contains(err.Error(), "no archive path") {
		t.Errorf("missing -a: got %v, want no-archive-path error", err)
	}

	// Archive given but no command: root Action prints a usage error.
	if err := root().Run(ctx, []string{"snorg", "-a", arch}); err == nil || !strings.Contains(err.Error(), "usage:") {
		t.Errorf("missing command: got %v, want usage error", err)
	}

	// Unknown command name falls through to the root Action's usage error.
	if err := root().Run(ctx, []string{"snorg", "-a", arch, "bogus"}); err == nil || !strings.Contains(err.Error(), "usage:") {
		t.Errorf("unknown command: got %v, want usage error", err)
	}

	// A real command runs against the shared archive (empty archive matches nothing).
	if err := root().Run(ctx, []string{"snorg", "-a", arch, "query", "all"}); err != nil {
		t.Errorf("query all on empty archive: %v", err)
	}

	// The command's own flags must reach the command, not the root parser,
	// and -c must be honored at the root (missing file = load error).
	if err := root().Run(ctx, []string{"snorg", "-c", filepath.Join(arch, "absent.yaml"), "-a", arch, "query", "all"}); err == nil || !strings.Contains(err.Error(), "read config") {
		t.Errorf("-c with missing file: got %v, want read-config error", err)
	}
	err := root().Run(ctx, []string{"snorg", "-a", arch, "analyze", "--force", "PAGEID"})
	if err == nil || strings.Contains(err.Error(), "--force") {
		t.Errorf("analyze --force: got %v, want a non-flag-parse error (provider validation)", err)
	}
}

// TestArchiveFromUserConfig: with no -a, the archive path comes from `archive:` in
// the XDG user config; -a overrides it; --no-user-config ignores it.
func TestArchiveFromUserConfig(t *testing.T) {
	ctx := context.Background()
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	if err := os.MkdirAll(filepath.Join(xdg, "snorg"), 0o755); err != nil {
		t.Fatal(err)
	}
	arch := t.TempDir()
	if err := os.WriteFile(filepath.Join(xdg, "snorg", archiveConfigName), []byte("archive: "+arch+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// No -a: `query all` runs against the user-config archive (empty = no match).
	if err := root().Run(ctx, []string{"snorg", "query", "all"}); err != nil {
		t.Errorf("archive from user config: %v", err)
	}
	// --no-user-config drops that default, leaving no archive path.
	if err := root().Run(ctx, []string{"snorg", "--no-user-config", "query", "all"}); err == nil || !strings.Contains(err.Error(), "no archive path") {
		t.Errorf("no-user-config: got %v, want no-archive-path error", err)
	}
	// -a wins over the user-config archive: a bad -a path still surfaces (the
	// user-config archive is empty and would not error), proving -a is used.
	if err := root().Run(ctx, []string{"snorg", "-a", filepath.Join(arch, "nope"), "list"}); err == nil {
		t.Errorf("-a override: expected error listing a non-existent archive dir")
	}
}

// TestQueryLong: `query -l` annotates each PAGEID with tab-separated columns
// "<note> p<page#> <*?> <headings> #keywords" (note = source sans .note; headings =
// analyzed title names joined " / ", empty until analyzed; keywords = device
// metadata as #tags, present without analysis; * = starred), while the bare form
// stays pipe-safe (PAGEIDs only).
func TestQueryLong(t *testing.T) {
	ctx := context.Background()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	arch := t.TempDir()

	a := archive.New(arch)
	n := &snote.Note{
		FileID: "F_TEST",
		Source: "meeting-notes.note",
		Pages: []snote.Page{
			{ID: "P1", Number: 1, Starred: true,
				Keywords: []snote.Keyword{{Text: "work"}, {Text: "q1"}},
				Titles: []snote.Title{
					{Rect: snote.Rect{X: 0, Y: 0, W: 10, H: 10}, Level: 1},
					{Rect: snote.Rect{X: 0, Y: 20, W: 10, H: 10}, Level: 2},
				}},
			{ID: "P2", Number: 2, Keywords: []snote.Keyword{{Text: "notes"}}},
		},
	}
	svg := `<svg xmlns="http://www.w3.org/2000/svg" width="1920" height="2560"><path d="M10 10 L100 100"/></svg>`
	if err := a.Write(n, map[string][]byte{"P1": []byte(svg), "P2": []byte(svg)}); err != nil {
		t.Fatal(err)
	}
	// Simulate analyze having transcribed two titles on P1; P2 stays unanalyzed.
	pd, err := a.ReadPage("F_TEST", "P1")
	if err != nil {
		t.Fatal(err)
	}
	pd.Titles[0].Analysis = &archive.TitleAnalysis{Name: "Agenda"}
	pd.Titles[1].Analysis = &archive.TitleAnalysis{Name: "Action items"}
	if err := a.WritePage("F_TEST", pd); err != nil {
		t.Fatal(err)
	}

	// Bare form is unchanged: PAGEIDs only, safe to pipe downstream.
	if got := captureStdout(t, func() error {
		return root().Run(ctx, []string{"snorg", "-a", arch, "query", "all"})
	}); got != "P1\nP2\n" {
		t.Errorf("bare query all = %q, want %q", got, "P1\nP2\n")
	}

	// Annotated form: tab-separated columns; * marks the starred page (empty
	// otherwise), analyzed headings join " / " (empty on the unanalyzed page),
	// keywords render as #tags (present even without analysis).
	want := "P1\tmeeting-notes\tp1\t*\tAgenda / Action items\t#work #q1\n" +
		"P2\tmeeting-notes\tp2\t\t\t#notes\n"
	if got := captureStdout(t, func() error {
		return root().Run(ctx, []string{"snorg", "-a", arch, "query", "-l", "all"})
	}); got != want {
		t.Errorf("query -l all = %q, want %q", got, want)
	}
}

// captureStdout runs fn with os.Stdout redirected to a pipe and returns what it
// wrote. Fails the test on a pipe error or a non-nil fn error.
func captureStdout(t *testing.T, fn func() error) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	runErr := fn()
	os.Stdout = orig
	w.Close()
	out, readErr := io.ReadAll(r)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if runErr != nil {
		t.Fatalf("run: %v", runErr)
	}
	return string(out)
}
