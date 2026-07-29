package archive

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
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
	if err := a.Write(note("Pa", "Pb"), svgMap(map[string]string{"Pa": "<svg/>", "Pb": "<svg/>"})); err != nil {
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

	results, err := a.MigrateAll()
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
	if err := a.Write(note("Pa"), svgMap(map[string]string{"Pa": "<svg/>"})); err != nil {
		t.Fatal(err)
	}
	notePath := filepath.Join(dir, "note.json")
	before, err := os.Stat(notePath)
	if err != nil {
		t.Fatal(err)
	}

	results, err := a.MigrateAll()
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
	if err := a.Write(note("Pa"), svgMap(map[string]string{"Pa": "<svg/>"})); err != nil {
		t.Fatal(err)
	}
	bumpVersion(t, filepath.Join(dir, "Pa.json")) // sets schema_version = 999

	results, err := a.MigrateAll()
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
	if err := a.Write(note("Pa", "Pb"), svgMap(map[string]string{"Pa": "<svg/>", "Pb": "<svg/>"})); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"note.json", "Pa.json", "Pb.json"} {
		stripVersion(t, filepath.Join(dir, p))
	}

	results, err := a.MigratePages([]string{"Pa", "Pnope"})
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
