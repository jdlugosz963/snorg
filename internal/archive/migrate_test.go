package archive

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jdlugosz963/snorg/internal/snote"
	diffpatch "github.com/njchilds90/go-diffpatch"
)

// stripVersion rewrites the JSON file at path with schema_version removed,
// simulating a pre-versioning (v0) file.
func stripVersion(t *testing.T, path string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	delete(m, "schema_version")
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestMigrationsChainLength guards the core invariant: one step per version.
func TestMigrationsChainLength(t *testing.T) {
	if len(schemaMigrations) != CurrentSchemaVersion {
		t.Fatalf("schemaMigrations has %d steps, want CurrentSchemaVersion=%d",
			len(schemaMigrations), CurrentSchemaVersion)
	}
}

// TestMigrateV0ToCurrent: stale (v0) files migrate to the current version, become
// readable through the gated accessors, and match the canonical ingest bytes.
func TestMigrateV0ToCurrent(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "F_TEST")
	a := New(root)
	if _, err := a.Write(note("Pa", "Pb"), svgMap(map[string]string{"Pa": "<svg/>", "Pb": "<svg/>"})); err != nil {
		t.Fatal(err)
	}

	// Capture the canonical v1 bytes, then knock the files back to v0.
	wantNote, _ := os.ReadFile(filepath.Join(dir, "note.json"))
	wantPage, _ := os.ReadFile(filepath.Join(dir, "Pa.json"))
	stripVersion(t, filepath.Join(dir, "note.json"))
	stripVersion(t, filepath.Join(dir, "Pa.json"))
	stripVersion(t, filepath.Join(dir, "Pb.json"))

	// The gate must reject the stale files now.
	if _, err := a.ReadNote("F_TEST"); err == nil {
		t.Fatal("ReadNote should reject a stale note.json")
	}

	results, err := a.MigrateAll(context.Background(), MigrateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 { // note + 2 pages
		t.Fatalf("got %d results, want 3: %+v", len(results), results)
	}
	for _, r := range results {
		if r.Err != nil {
			t.Errorf("%s %s: unexpected error %v", r.Kind, r.ID, r.Err)
		}
		if r.Outcome != MigrateUpgraded {
			t.Errorf("%s %s: outcome %q, want %q", r.Kind, r.ID, r.Outcome, MigrateUpgraded)
		}
	}

	// Gated reads work again.
	if _, err := a.ReadNote("F_TEST"); err != nil {
		t.Errorf("ReadNote after migrate: %v", err)
	}
	if _, err := a.ReadPage("F_TEST", "Pa"); err != nil {
		t.Errorf("ReadPage after migrate: %v", err)
	}

	// Bytes are byte-identical to a fresh v1 write (canonical form).
	if got, _ := os.ReadFile(filepath.Join(dir, "note.json")); string(got) != string(wantNote) {
		t.Errorf("migrated note.json != canonical:\ngot  %s\nwant %s", got, wantNote)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "Pa.json")); string(got) != string(wantPage) {
		t.Errorf("migrated Pa.json != canonical:\ngot  %s\nwant %s", got, wantPage)
	}
}

// TestMigrateIdempotent: a second run reports everything current and rewrites
// nothing (mtimes unchanged).
func TestMigrateIdempotent(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "F_TEST")
	a := New(root)
	if _, err := a.Write(note("Pa"), svgMap(map[string]string{"Pa": "<svg/>"})); err != nil {
		t.Fatal(err)
	}
	notePath := filepath.Join(dir, "note.json")
	before, err := os.Stat(notePath)
	if err != nil {
		t.Fatal(err)
	}

	results, err := a.MigrateAll(context.Background(), MigrateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if r.Outcome != MigrateCurrent || r.Err != nil {
			t.Errorf("%s %s: outcome %q err %v, want current/nil", r.Kind, r.ID, r.Outcome, r.Err)
		}
	}
	if after, _ := os.Stat(notePath); !after.ModTime().Equal(before.ModTime()) {
		t.Error("already-current note.json was rewritten (mtime changed)")
	}
}

// TestMigrateNewerThanBinary: a file from a future grammar is a per-file error, not
// a silent downgrade.
func TestMigrateNewerThanBinary(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "F_TEST")
	a := New(root)
	if _, err := a.Write(note("Pa"), svgMap(map[string]string{"Pa": "<svg/>"})); err != nil {
		t.Fatal(err)
	}
	bumpVersion(t, filepath.Join(dir, "Pa.json")) // sets schema_version = 999

	results, err := a.MigrateAll(context.Background(), MigrateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var pageErr error
	for _, r := range results {
		if r.Kind == "page" && r.ID == "Pa" {
			pageErr = r.Err
		}
	}
	if pageErr == nil {
		t.Fatal("migrating a newer-than-binary page should report an error")
	}
}

// TestMigratePagesSelection: MigratePages touches only the named page and its owning
// note, and reports unknown PAGEIDs as errors.
func TestMigratePagesSelection(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "F_TEST")
	a := New(root)
	if _, err := a.Write(note("Pa", "Pb"), svgMap(map[string]string{"Pa": "<svg/>", "Pb": "<svg/>"})); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"note.json", "Pa.json", "Pb.json"} {
		stripVersion(t, filepath.Join(dir, p))
	}

	results, err := a.MigratePages(context.Background(), []string{"Pa", "Pnope"}, MigrateOptions{})
	if err != nil {
		t.Fatal(err)
	}

	outcomes := map[string]MigrateResult{}
	for _, r := range results {
		outcomes[r.Kind+"/"+r.ID] = r
	}
	if r := outcomes["note/F_TEST"]; r.Outcome != MigrateUpgraded {
		t.Errorf("owning note not migrated: %+v", r)
	}
	if r := outcomes["page/Pa"]; r.Outcome != MigrateUpgraded {
		t.Errorf("selected page not migrated: %+v", r)
	}
	if _, ok := outcomes["page/Pb"]; ok {
		t.Error("unselected page Pb should not have been touched")
	}
	if r := outcomes["page/Pnope"]; r.Err == nil {
		t.Error("unknown PAGEID should report an error")
	}

	// Pb was left stale (still v0), proving the selection was respected.
	if _, err := a.ReadPage("F_TEST", "Pb"); err == nil {
		t.Error("unselected page Pb should still be stale after selective migrate")
	}
}

// downgradeLinksToV1 rewrites a page JSON as a genuine v1 file: schema_version=1
// and every link stripped of its v2-only kind/target fields.
func downgradeLinksToV1(t *testing.T, path string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	m["schema_version"] = 1
	if links, ok := m["links"].([]any); ok {
		for _, raw := range links {
			if l, ok := raw.(map[string]any); ok {
				delete(l, "kind")
				delete(l, "target")
			}
		}
	}
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestMigrateV1LinkKinds: the v1→v2 step stamps `note` on a link with real target
// ids and `unknown` on one whose ids are the device sentinel "none".
func TestMigrateV1LinkKinds(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "F_TEST")
	a := New(root)
	n := &snote.Note{
		FileID: "F_TEST",
		Pages: []snote.Page{{ID: "Pa", Number: 1, Links: []snote.Link{
			{Rect: snote.Rect{W: 1, H: 1}, Kind: snote.LinkNote, TargetPageID: "Pb", TargetFileID: "F_TEST"},
			{Rect: snote.Rect{W: 1, H: 1}, Kind: snote.LinkFile, TargetPageID: "none", TargetFileID: "none", Target: "/x/y.pdf"},
		}}, {ID: "Pb", Number: 2}},
	}
	if _, err := a.Write(n, svgMap(map[string]string{"Pa": "<svg/>", "Pb": "<svg/>"})); err != nil {
		t.Fatal(err)
	}
	downgradeLinksToV1(t, filepath.Join(dir, "Pa.json"))

	if _, err := a.MigratePages(context.Background(), []string{"Pa"}, MigrateOptions{}); err != nil {
		t.Fatal(err)
	}

	pd, err := a.ReadPage("F_TEST", "Pa")
	if err != nil {
		t.Fatal(err)
	}
	if len(pd.Links) != 2 {
		t.Fatalf("got %d links, want 2", len(pd.Links))
	}
	if got := pd.Links[0].Kind; got != "note" {
		t.Errorf("link with real target ids: kind %q, want note", got)
	}
	if pd.Links[0].TargetFileID != "F_TEST" || pd.Links[0].TargetPageID != "Pb" {
		t.Errorf("note link should keep its real ids, got %+v", pd.Links[0])
	}
	if got := pd.Links[1].Kind; got != "unknown" {
		t.Errorf("link with none ids: kind %q, want unknown", got)
	}
	// The "none" sentinels are blanked so they drop out of the JSON.
	if pd.Links[1].TargetFileID != "" || pd.Links[1].TargetPageID != "" {
		t.Errorf("unknown link should drop its \"none\" ids, got %+v", pd.Links[1])
	}
}

// legacyDiff returns the pre-unified on-disk sidecar form: a JSON-encoded
// go-diffpatch patch base→content, exactly what the old textmerge.Diff wrote.
func legacyDiff(t *testing.T, base, content string) string {
	t.Helper()
	p, err := diffpatch.Diff(base, content)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestMigrateLegacyEditDiff: a page whose <PAGEID>.md.diff still holds the legacy
// JSON patch is converted to a unified diff by migrate, keeps reconstructing the
// AI base, and a second run is a no-op.
func TestMigrateLegacyEditDiff(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "F_TEST")
	a := New(root)
	if _, err := a.Write(note("Pa"), svgMap(map[string]string{"Pa": "<svg/>"})); err != nil {
		t.Fatal(err)
	}
	base := "old line one\nold line two\n"
	content := "old line one\nNEW line two\n"
	if err := a.WriteAnalysisMD("F_TEST", "Pa", content); err != nil {
		t.Fatal(err)
	}
	diffPath := a.diffPath("F_TEST", "Pa")
	if err := os.WriteFile(diffPath, []byte(legacyDiff(t, base, content)), 0o644); err != nil {
		t.Fatal(err)
	}
	stripVersion(t, filepath.Join(dir, "Pa.json")) // simulate a stale archive.

	results, err := a.MigrateAll(context.Background(), MigrateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var gotDiffResult bool
	for _, r := range results {
		if r.Err != nil {
			t.Errorf("%s %s: unexpected error %v", r.Kind, r.ID, r.Err)
		}
		if r.Kind == "diff" && r.ID == "Pa" && r.Outcome == MigrateUpgraded {
			gotDiffResult = true
		}
	}
	if !gotDiffResult {
		t.Fatalf("no diff-migration result for Pa: %+v", results)
	}

	// The sidecar is now a unified diff, not the JSON struct.
	got, _ := os.ReadFile(diffPath)
	if !strings.Contains(string(got), "@@") || strings.Contains(string(got), "\"hunks\"") {
		t.Fatalf("sidecar was not converted to a unified diff:\n%s", got)
	}
	// The AI base still reconstructs, and the page reads through the gate.
	if _, err := a.ReadPage("F_TEST", "Pa"); err != nil {
		t.Errorf("ReadPage after migrate: %v", err)
	}
	base2, err := a.ReadAnalysisBase("F_TEST", "Pa")
	if err != nil {
		t.Fatal(err)
	}
	if base2 != base {
		t.Errorf("reconstructed base = %q, want %q", base2, base)
	}

	// Idempotent: a second run neither reports a diff conversion nor rewrites it.
	before, err := os.Stat(diffPath)
	if err != nil {
		t.Fatal(err)
	}
	results2, err := a.MigrateAll(context.Background(), MigrateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results2 {
		if r.Kind == "diff" {
			t.Errorf("second migrate re-reported a diff conversion: %+v", r)
		}
	}
	if after, _ := os.Stat(diffPath); !after.ModTime().Equal(before.ModTime()) {
		t.Error("already-unified .md.diff was rewritten (mtime changed)")
	}
}

// TestMigrateStreamsResults: the callback stream is the returned slice, element for
// element — including the extra `diff` result a page can yield, which is why the
// two must come from one emit path rather than two append sites.
func TestMigrateStreamsResults(t *testing.T) {
	root := t.TempDir()
	a := New(root)
	if _, err := a.Write(note("Pa", "Pb"), svgMap(map[string]string{"Pa": "<svg/>", "Pb": "<svg/>"})); err != nil {
		t.Fatal(err)
	}
	stripVersion(t, filepath.Join(root, "F_TEST", "note.json"))
	stripVersion(t, filepath.Join(root, "F_TEST", "Pa.json"))

	var streamed []MigrateResult
	results, err := a.MigrateAll(context.Background(), MigrateOptions{
		OnResult: func(r MigrateResult) { streamed = append(streamed, r) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("precondition: the fixture archive must yield results")
	}
	if !reflect.DeepEqual(streamed, results) {
		t.Errorf("stream = %+v, want the returned slice %+v", streamed, results)
	}
}
