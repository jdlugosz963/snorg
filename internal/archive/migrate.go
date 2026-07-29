package archive

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Schema migration: the one place allowed to read stale on-disk grammars. Every
// other reader gates on schema_version (verifySchema); migrate walks each versioned
// JSON file forward one version at a time (v→v+1→…→CurrentSchemaVersion) through the
// schemaMigrations chain, then re-serializes it through the canonical Doc struct so
// the bytes match what ingest would write. It never uses the gated accessors and
// enumerates the archive via os.Stat/glob, so it works on exactly the stale archive
// it exists to repair.

// migKind distinguishes the two versioned doc types so a step can transform them
// differently. The version axis itself is archive-wide (one CurrentSchemaVersion).
type migKind int

const (
	kindNote migKind = iota
	kindPage
)

func (k migKind) String() string {
	if k == kindNote {
		return "note"
	}
	return "page"
}

// schemaMigrations[v] transforms a decoded JSON object from version v to v+1 in
// place; the framework stamps schema_version = v+1 after each step. Indexed by
// source version, so len(schemaMigrations) == CurrentSchemaVersion (asserted in a
// test). Append one function (and bump CurrentSchemaVersion) on any contract change.
var schemaMigrations = []func(migKind, map[string]any) error{
	// v0 → v1: schema_version was introduced; there is no structural change between
	// the two grammars, and the framework stamps the field, so this step only has to
	// exist to establish the chain.
	func(migKind, map[string]any) error { return nil },
}

// MigrateOutcome reports what happened to one file.
type MigrateOutcome string

const (
	MigrateCurrent  MigrateOutcome = "current"  // already at CurrentSchemaVersion
	MigrateUpgraded MigrateOutcome = "migrated" // walked forward to CurrentSchemaVersion
)

// MigrateResult is the per-file outcome. Err is set when that file failed (a newer
// grammar than this binary, malformed JSON); the walk continues past it so one bad
// file never blocks the rest.
type MigrateResult struct {
	Kind    string
	ID      string // FILE_ID for a note, PAGEID for a page
	Outcome MigrateOutcome
	Err     error
}

// MigrateAll migrates every note.json and page JSON in the archive: note first,
// then its pages, in List/sorted order. The only top-level error is an enumeration
// failure (a directory that cannot be read); per-file failures land in the results.
func (a *Archive) MigrateAll() ([]MigrateResult, error) {
	fileIDs, err := a.List()
	if err != nil {
		return nil, err
	}
	var out []MigrateResult
	for _, fileID := range fileIDs {
		out = append(out, a.migrateNote(fileID))
		pageIDs, err := a.sortedPageIDs(fileID)
		if err != nil {
			return nil, err
		}
		for _, pid := range pageIDs {
			out = append(out, a.migratePage(fileID, pid))
		}
	}
	return out, nil
}

// MigratePages migrates the given pages' JSON plus each owning note's note.json
// (once per note). Notes come before their pages, both in sorted order. An unknown
// PAGEID yields a result with Err rather than aborting the batch.
func (a *Archive) MigratePages(pageIDs []string) ([]MigrateResult, error) {
	index, err := a.pageIndex()
	if err != nil {
		return nil, err
	}

	// Group requested pages by owning note, preserving uniqueness.
	notes := map[string][]string{}
	var noteOrder []string
	var out []MigrateResult
	for _, pid := range pageIDs {
		fileID, ok := index[pid]
		if !ok {
			out = append(out, MigrateResult{Kind: kindPage.String(), ID: pid,
				Err: fmt.Errorf("page %s not found in archive", pid)})
			continue
		}
		if _, seen := notes[fileID]; !seen {
			noteOrder = append(noteOrder, fileID)
		}
		notes[fileID] = append(notes[fileID], pid)
	}
	sort.Strings(noteOrder)

	for _, fileID := range noteOrder {
		out = append(out, a.migrateNote(fileID))
		pages := notes[fileID]
		sort.Strings(pages)
		for _, pid := range pages {
			out = append(out, a.migratePage(fileID, pid))
		}
	}
	return out, nil
}

// pageIndex maps every archived PAGEID to its owning FILE_ID, built once from the
// un-gated directory listing (cheaper than a FindPage scan per page, and it works on
// a stale archive where the gated readers would fail).
func (a *Archive) pageIndex() (map[string]string, error) {
	fileIDs, err := a.List()
	if err != nil {
		return nil, err
	}
	index := map[string]string{}
	for _, fileID := range fileIDs {
		ids, err := archivedPageIDs(filepath.Join(a.Root, fileID))
		if err != nil {
			return nil, err
		}
		for id := range ids {
			index[id] = fileID
		}
	}
	return index, nil
}

// sortedPageIDs returns the note's PAGEIDs in deterministic order.
func (a *Archive) sortedPageIDs(fileID string) ([]string, error) {
	ids, err := archivedPageIDs(filepath.Join(a.Root, fileID))
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(ids))
	for id := range ids {
		out = append(out, id)
	}
	sort.Strings(out)
	return out, nil
}

func (a *Archive) migrateNote(fileID string) MigrateResult {
	path := filepath.Join(a.Root, fileID, "note.json")
	outcome, err := a.migrateFile(path, kindNote)
	return MigrateResult{Kind: kindNote.String(), ID: fileID, Outcome: outcome, Err: err}
}

func (a *Archive) migratePage(fileID, pageID string) MigrateResult {
	path := filepath.Join(a.Root, fileID, pageID+".json")
	outcome, err := a.migrateFile(path, kindPage)
	return MigrateResult{Kind: kindPage.String(), ID: pageID, Outcome: outcome, Err: err}
}

// migrateFile walks one JSON file forward to CurrentSchemaVersion and rewrites it in
// the canonical Doc form. It reads raw (no verifySchema) since it is the only reader
// meant to touch stale grammars.
func (a *Archive) migrateFile(path string, kind migKind) (MigrateOutcome, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", filepath.Base(path), err)
	}
	m, err := decodeObject(b)
	if err != nil {
		return "", fmt.Errorf("parse %s: %w", filepath.Base(path), err)
	}

	v, err := schemaVersionOf(m)
	if err != nil {
		return "", fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	switch {
	case v > CurrentSchemaVersion:
		return "", fmt.Errorf("%s: schema version %d is newer than this binary's %d — upgrade snorg",
			filepath.Base(path), v, CurrentSchemaVersion)
	case v == CurrentSchemaVersion:
		return MigrateCurrent, nil
	}

	for ; v < CurrentSchemaVersion; v++ {
		if err := schemaMigrations[v](kind, m); err != nil {
			return "", fmt.Errorf("%s: migrate v%d→v%d: %w", filepath.Base(path), v, v+1, err)
		}
		m["schema_version"] = v + 1
	}

	doc, err := canonicalDoc(kind, m)
	if err != nil {
		return "", fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	if err := writeJSONIfChanged(path, doc); err != nil {
		return "", err
	}
	return MigrateUpgraded, nil
}

// decodeObject parses JSON into a generic object, keeping numbers exact
// (json.Number) so integer fields round-trip without float64 drift.
func decodeObject(b []byte) (map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		return nil, err
	}
	return m, nil
}

// schemaVersionOf reads the object's schema_version; an absent field is version 0
// (pre-versioning). A present-but-non-integer value is an error.
func schemaVersionOf(m map[string]any) (int, error) {
	raw, ok := m["schema_version"]
	if !ok {
		return 0, nil
	}
	n, ok := raw.(json.Number)
	if !ok {
		return 0, fmt.Errorf("schema_version is not a number: %v", raw)
	}
	v, err := n.Int64()
	if err != nil {
		return 0, fmt.Errorf("invalid schema_version %q: %w", n, err)
	}
	return int(v), nil
}

// canonicalDoc materializes the migrated object into the current typed Doc (per
// kind), so the subsequent marshal emits exactly the bytes ingest would write
// (canonical field order, schema_version first) and any field absent from the
// current grammar is dropped.
func canonicalDoc(kind migKind, m map[string]any) (any, error) {
	raw, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	switch kind {
	case kindNote:
		var nd NoteDoc
		if err := json.Unmarshal(raw, &nd); err != nil {
			return nil, err
		}
		return nd, nil
	default:
		var pd PageDoc
		if err := json.Unmarshal(raw, &pd); err != nil {
			return nil, err
		}
		return pd, nil
	}
}
