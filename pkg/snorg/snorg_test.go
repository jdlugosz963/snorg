package snorg

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/jdlugosz963/snorg/internal/archive"
	"github.com/jdlugosz963/snorg/internal/snote"
)

// minimal SVG with a path so the archive's ingest fingerprint has geometry.
const testSVG = `<svg xmlns="http://www.w3.org/2000/svg" width="1920" height="2560"><path d="M10 10 L100 100"/></svg>`

// seedArchive writes a two-page note directly through the archive layer (no
// supernote-tool needed) and returns an opened Client rooted there.
func seedArchive(t *testing.T) *Client {
	t.Helper()
	root := t.TempDir()
	a := archive.New(root)
	n := &snote.Note{
		FileID: "F_A",
		Source: "alpha.note",
		Pages: []snote.Page{
			{ID: "P1", Number: 1, Starred: true, Keywords: []snote.Keyword{{Text: "work"}}},
			{ID: "P2", Number: 2},
		},
	}
	if err := a.Write(n, map[string][]byte{"P1": []byte(testSVG), "P2": []byte(testSVG)}); err != nil {
		t.Fatal(err)
	}
	c, err := Open(root, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return c
}

func TestOpenListQueryRetrieve(t *testing.T) {
	c := seedArchive(t)

	ids, err := c.List()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ids, []string{"F_A"}) {
		t.Errorf("List = %v, want [F_A]", ids)
	}

	all, err := c.Query(All)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Errorf("Query(All) = %d matches, want 2", len(all))
	}

	starred, err := c.Query(Starred)
	if err != nil {
		t.Fatal(err)
	}
	if len(starred) != 1 || starred[0].PageID != "P1" {
		t.Errorf("Query(Starred) = %v, want [P1]", starred)
	}

	res, err := c.Retrieve([]string{"P1", "P2"})
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(res.Archive) {
		t.Errorf("Result.Archive = %q, want absolute", res.Archive)
	}
	if len(res.Notes) != 1 || len(res.Notes[0].Pages) != 2 {
		t.Fatalf("Retrieve = %d notes, want 1 note with 2 pages", len(res.Notes))
	}
	if _, err := c.Retrieve([]string{"Pnope"}); err == nil {
		t.Error("Retrieve unknown PAGEID: want error")
	}
}

func TestParseFilter(t *testing.T) {
	c := seedArchive(t)

	run := func(filter string, args []string) []Match {
		t.Helper()
		pred, err := c.ParseFilter(filter, args)
		if err != nil {
			t.Fatalf("ParseFilter(%q, %v): %v", filter, args, err)
		}
		m, err := c.Query(pred)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}

	if got := run("starred", nil); len(got) != 1 || got[0].PageID != "P1" {
		t.Errorf("starred = %v, want [P1]", got)
	}
	// "not" inverts: the non-starred page.
	if got := run("not", []string{"starred"}); len(got) != 1 || got[0].PageID != "P2" {
		t.Errorf("not starred = %v, want [P2]", got)
	}
	if got := run("keyword", []string{"work"}); len(got) != 1 || got[0].PageID != "P1" {
		t.Errorf("keyword work = %v, want [P1]", got)
	}

	// Error cases: unknown filter, wrong arity, bad regexp.
	for _, tc := range []struct {
		filter string
		args   []string
	}{
		{"bogus", nil},
		{"note", nil},              // arity: wants 1
		{"starred", []string{"x"}}, // arity: wants 0
		{"keyword", []string{"("}}, // invalid regexp
	} {
		if _, err := c.ParseFilter(tc.filter, tc.args); err == nil {
			t.Errorf("ParseFilter(%q, %v): want error", tc.filter, tc.args)
		}
	}
}

func TestPageBufferApplyRoundTrip(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	c := seedArchive(t)

	// A never-analyzed page with no regions serializes to empty content.
	buf, err := c.PageBuffer("P2")
	if err != nil {
		t.Fatal(err)
	}
	if buf != "" {
		t.Errorf("PageBuffer(P2) = %q, want empty (unanalyzed, no regions)", buf)
	}

	// Applying a hand transcription stores it as the effective content.
	outcome, names, err := c.ApplyPage("P2", "# hand-written\n\nbody\n")
	if err != nil {
		t.Fatal(err)
	}
	if outcome != EditEdited || names != 0 {
		t.Errorf("ApplyPage = (%s, %d), want (edited, 0)", outcome, names)
	}
	if got, _ := c.PageBuffer("P2"); got != "# hand-written\n\nbody\n" {
		t.Errorf("PageBuffer after ApplyPage = %q, want the applied content", got)
	}
}

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
		from, to, err := ParseDateSpec(tc.spec)
		if (err != nil) != tc.wantErr {
			t.Errorf("ParseDateSpec(%q) err = %v, wantErr %v", tc.spec, err, tc.wantErr)
			continue
		}
		if err == nil && (from != tc.from || to != tc.to) {
			t.Errorf("ParseDateSpec(%q) = (%q,%q), want (%q,%q)", tc.spec, from, to, tc.from, tc.to)
		}
	}
}
