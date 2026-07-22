package main

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestConfigPaths(t *testing.T) {
	dir := t.TempDir()
	archiveCfg := filepath.Join(dir, archiveConfigName)
	if err := os.WriteFile(archiveCfg, []byte("provider:\n  model: archive\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cli := []string{"/tmp/a.yaml", "/tmp/b.yaml"}

	// Archive config present: it comes first, then -c files (which win the merge).
	got := configPaths(dir, cli, false)
	want := append([]string{archiveCfg}, cli...)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("with archive config: got %v, want %v", got, want)
	}

	// Opt-out: only the -c files.
	if got := configPaths(dir, cli, true); !reflect.DeepEqual(got, cli) {
		t.Errorf("no-archive-config: got %v, want %v", got, cli)
	}

	// Missing archive config: only the -c files, no error.
	if got := configPaths(t.TempDir(), cli, false); !reflect.DeepEqual(got, cli) {
		t.Errorf("missing archive config: got %v, want %v", got, cli)
	}

	// A directory named config.yaml is not treated as a config file.
	dir2 := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir2, archiveConfigName), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := configPaths(dir2, cli, false); !reflect.DeepEqual(got, cli) {
		t.Errorf("config.yaml dir: got %v, want %v", got, cli)
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

	// Missing archive flag: urfave enforces the required global flag.
	if err := root().Run(ctx, []string{"snorg", "list"}); err == nil || !strings.Contains(err.Error(), "archive") {
		t.Errorf("missing -a: got %v, want required-flag error", err)
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
