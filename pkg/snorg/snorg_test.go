package snorg

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/jdlugosz963/snorg/internal/archive"
	"github.com/jdlugosz963/snorg/internal/snote"
)

// minimal SVG with a path so the archive's ingest fingerprint has geometry.
// testSVG is a page with handwriting on it: a closed, filled contour, the form a
// real archive SVG takes. A zero-area hairline would rasterize to no ink, making
// the page blank — which analyze deliberately transcribes without a model call.
const testSVG = `<svg xmlns="http://www.w3.org/2000/svg" width="1920" height="2560" viewBox="0 0 1920 2560"><path fill="#000000" d="M 100 100 L 500 100 L 500 500 L 100 500 Z"/></svg>`

// seedArchive writes a two-page note directly through the archive layer (no
// .note parsing/rendering needed) and returns an opened Client rooted there.
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
	if _, err := a.Write(n, map[string][]byte{"P1": []byte(testSVG), "P2": []byte(testSVG)}); err != nil {
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

	all, err := c.Query(MatchAll)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Errorf("Query(MatchAll) = %d matches, want 2", len(all))
	}

	starred, err := c.Query(MatchStarred)
	if err != nil {
		t.Fatal(err)
	}
	if len(starred) != 1 || starred[0].PageID != "P1" {
		t.Errorf("Query(MatchStarred) = %v, want [P1]", starred)
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

func TestParseQuery(t *testing.T) {
	c := seedArchive(t)

	run := func(expr string) []Match {
		t.Helper()
		pred, err := ParseQuery(expr)
		if err != nil {
			t.Fatalf("ParseQuery(%q): %v", expr, err)
		}
		m, err := c.Query(pred)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}

	if got := run("starred"); len(got) != 1 || got[0].PageID != "P1" {
		t.Errorf("starred = %v, want [P1]", got)
	}
	// NOT inverts: the non-starred page.
	if got := run("NOT starred"); len(got) != 1 || got[0].PageID != "P2" {
		t.Errorf("NOT starred = %v, want [P2]", got)
	}
	if got := run("keyword:work"); len(got) != 1 || got[0].PageID != "P1" {
		t.Errorf("keyword:work = %v, want [P1]", got)
	}

	// Error cases: syntax, unknown term, a value on a term that takes none, a
	// missing value, a bad regexp, a bad date.
	for _, expr := range []string{
		"",
		"starred keyword:work",
		"bogus",
		"starred:x",
		"keyword",
		"keyword~(",
		"date:not-a-date",
		"date~today",
		"tag[x]:y",
	} {
		if _, err := ParseQuery(expr); err == nil {
			t.Errorf("ParseQuery(%q): want error", expr)
		}
	}
}

func TestPageBufferApplyRoundTrip(t *testing.T) {
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
	res, err := c.ApplyPage("P2", "# hand-written\n\nbody\n")
	if err != nil {
		t.Fatal(err)
	}
	// The page had no transcription at all, so this is new content, not an update
	// — and no name moved.
	if res.Content.Kind != TextNew || res.Content.Now != "# hand-written\n\nbody\n" {
		t.Errorf("ApplyPage content = %+v, want %q text", res.Content, TextNew)
	}
	if len(res.Names) != 0 || len(res.Regions) != 0 {
		t.Errorf("ApplyPage = %+v, want no name or region changes", res)
	}
	if got, _ := c.PageBuffer("P2"); got != "# hand-written\n\nbody\n" {
		t.Errorf("PageBuffer after ApplyPage = %q, want the applied content", got)
	}
}

func TestConfigPaths(t *testing.T) {
	userCfg := filepath.Join(t.TempDir(), userConfigName)
	if err := os.WriteFile(userCfg, []byte("archive: /somewhere\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cli := []string{"/tmp/a.yaml", "/tmp/b.yaml"}

	// Both layers present: the XDG user config, then the -c files (each later
	// layer wins the merge).
	got := configPaths(userCfg, cli, false)
	want := append([]string{userCfg}, cli...)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("all layers: got %v, want %v", got, want)
	}

	// --no-user-config drops the auto-loaded user config, leaving only -c files.
	if got := configPaths(userCfg, cli, true); !reflect.DeepEqual(got, cli) {
		t.Errorf("no-user-config: got %v", got)
	}

	// Empty user path: only the -c files, no error.
	if got := configPaths("", cli, false); !reflect.DeepEqual(got, cli) {
		t.Errorf("missing user config: got %v, want %v", got, cli)
	}

	// A directory named config.yaml is not treated as a config file.
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, userConfigName), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := configPaths(filepath.Join(dir, userConfigName), cli, false); !reflect.DeepEqual(got, cli) {
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

// TestOpenBridgesTemplateSpecs verifies Open wires the merged config's templates:
// section into the archive: an image path resolved by LoadConfig (relative to the
// config file) is hashed and matchable via the archive's template set.
func TestOpenBridgesTemplateSpecs(t *testing.T) {
	dir := t.TempDir()
	imgBytes := []byte("device-form-template")
	sum := sha256.Sum256(imgBytes)
	hash := hex.EncodeToString(sum[:])
	if err := os.WriteFile(filepath.Join(dir, "bg.png"), imgBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(
		"templates:\n  - image: bg.png\n    boxes:\n      - {id: title, label: \"Title\", rect: {x: 0, y: 0, w: 1920, h: 400}, analyze: true}\n"),
		0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig([]string{cfgPath})
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	c, err := Open(t.TempDir(), cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	ts, err := c.arch.Templates()
	if err != nil {
		t.Fatalf("Templates: %v", err)
	}
	tmpl := ts.MatchBackground(hash)
	if tmpl == nil {
		t.Fatalf("template not matched by image hash %s (bridge broken)", hash)
	}
	if len(tmpl.Boxes) != 1 || tmpl.Boxes[0].ID != "title" {
		t.Errorf("boxes = %+v", tmpl.Boxes)
	}
}

// TestConfigIsSnapshot: Config hands out a copy, so a caller cannot reach into the
// client's own configuration — the reason NewProvider can resolve a secret without it
// showing up there.
func TestConfigIsSnapshot(t *testing.T) {
	c := seedArchive(t)
	c.cfg.Analysis.Fields = map[string]Task{"summary": {Prompt: "summarize"}}

	got := c.Config()
	if got == c.cfg {
		t.Fatal("Config returned the client's own config pointer")
	}
	got.Export.Template = "{{ archive }}"
	got.Analysis.Fields["summary"] = Task{Prompt: "hijacked"}

	if c.cfg.Export.Template != "" {
		t.Errorf("export template = %q, want the client's config untouched", c.cfg.Export.Template)
	}
	if p := c.cfg.Analysis.Fields["summary"].Prompt; p != "summarize" {
		t.Errorf("field prompt = %q, want %q", p, "summarize")
	}
	if again := c.Config(); again.Export.Template != "" {
		t.Errorf("second Config().Export.Template = %q, want empty", again.Export.Template)
	}
}

// TestBatchContextCancel: Ingest and Migrate stop on a cancelled context the way
// Analyze does — nothing processed, the context's error returned.
func TestBatchContextCancel(t *testing.T) {
	c := seedArchive(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	ing, err := c.Ingest(ctx, []string{"never-read.note"}, IngestOptions{})
	if !errors.Is(err, context.Canceled) || len(ing) != 0 {
		t.Errorf("Ingest = %d results, %v; want none, context.Canceled", len(ing), err)
	}
	mig, err := c.Migrate(ctx, []string{"P1"}, MigrateOptions{})
	if !errors.Is(err, context.Canceled) || len(mig) != 0 {
		t.Errorf("Migrate = %d results, %v; want none, context.Canceled", len(mig), err)
	}
	mig, err = c.MigrateAll(ctx, MigrateOptions{})
	if !errors.Is(err, context.Canceled) || len(mig) != 0 {
		t.Errorf("MigrateAll = %d results, %v; want none, context.Canceled", len(mig), err)
	}
}
