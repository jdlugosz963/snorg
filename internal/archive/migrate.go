package archive

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/jdlugosz963/snorg/internal/snote"
	"github.com/jdlugosz963/snorg/internal/textmerge"
)

// Schema migration: the one place allowed to read stale on-disk grammars. Every
// other reader gates on schema_version (verifySchema); migrate walks each versioned
// JSON file forward one version at a time (v→v+1→…→CurrentSchemaVersion) through the
// schemaMigrations chain, then re-serializes it through the canonical Doc struct so
// the bytes match what ingest would write. It never uses the gated accessors and
// enumerates the archive via os.Stat/glob, so it works on exactly the stale archive
// it exists to repair. It also normalizes the unversioned <PAGEID>.md.diff sidecar
// (legacy JSON patch → unified diff) by content detection, since that file carries
// no schema_version to walk.

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
	// v1 → v2: links gained a `kind` (note/file/web/unknown). Pre-v2 links were all
	// treated as note jumps, so stamp `note` on any link that carries a real target
	// file+page id (the shape only note links produce) and `unknown` otherwise. The
	// decoded `target` was never captured pre-v2; re-ingest repopulates it.
	func(k migKind, m map[string]any) error {
		if k != kindPage {
			return nil
		}
		links, _ := m["links"].([]any)
		for _, raw := range links {
			l, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if realID(l["target_file_id"]) && realID(l["target_page_id"]) {
				l["kind"] = string(snote.LinkNote)
			} else {
				l["kind"] = string(snote.LinkUnknown)
			}
			// Drop the device "none"/empty sentinels so migrated links match a
			// fresh ingest (target ids are note-link-only, now omitempty).
			if !realID(l["target_file_id"]) {
				delete(l, "target_file_id")
			}
			if !realID(l["target_page_id"]) {
				delete(l, "target_page_id")
			}
		}
		return nil
	},
	// v2 → v3: the <PAGEID>.md.diff sidecar changed from a JSON-encoded
	// go-diffpatch patch to a normal unified diff. The versioned JSON grammar is
	// unchanged, so this step is a no-op; the bump exists only to make the gated
	// readers reject stale archives and funnel to migrate, whose sidecar pass
	// (migrateEditDiff) does the actual conversion — the .md.diff carries no
	// schema_version, so it cannot ride this chain.
	func(migKind, map[string]any) error { return nil },
	// v3 → v4: pages gained a snorg-managed `tags` list. It is purely additive and
	// omitempty, so an absent key unmarshals to nil and canonicalDoc reproduces a
	// fresh-ingest byte layout — no transformation needed.
	func(migKind, map[string]any) error { return nil },
	// v4 → v5: notes gained their own snorg-managed `tags` list (inherited by every
	// page of the note). Additive and omitempty like the page tags before it, so an
	// absent key unmarshals to nil — no transformation needed.
	func(migKind, map[string]any) error { return nil },
}

// realID reports whether a decoded JSON value is a present, non-"none" id string —
// the device writes the literal "none" for a link with no note target.
func realID(v any) bool {
	s, ok := v.(string)
	return ok && s != "" && s != "none"
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

// MigrateOptions tunes a migration batch.
type MigrateOptions struct {
	// OnResult, when set, is called with each file's result as it lands. The walk
	// is sequential, so results arrive in exactly the returned slice's order,
	// letting a caller report progress instead of waiting for the whole archive.
	OnResult func(MigrateResult)
}

// emit records results in order and streams them to opts.OnResult, so the
// callback can never diverge from the returned slice.
func emit(out *[]MigrateResult, opts MigrateOptions, rs ...MigrateResult) {
	for _, r := range rs {
		*out = append(*out, r)
		if opts.OnResult != nil {
			opts.OnResult(r)
		}
	}
}

// MigrateAll migrates every note.json and page JSON in the archive: note first,
// then its pages, in List/sorted order. The only top-level errors are an enumeration
// failure (a directory that cannot be read) and a cancelled ctx, which stops the walk
// between files and returns the results so far; per-file failures land in the results.
func (a *Archive) MigrateAll(ctx context.Context, opts MigrateOptions) ([]MigrateResult, error) {
	fileIDs, err := a.List()
	if err != nil {
		return nil, err
	}
	var out []MigrateResult
	for _, fileID := range fileIDs {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		emit(&out, opts, a.migrateNote(fileID))
		pageIDs, err := a.sortedPageIDs(fileID)
		if err != nil {
			return nil, err
		}
		for _, pid := range pageIDs {
			if err := ctx.Err(); err != nil {
				return out, err
			}
			emit(&out, opts, a.migratePage(fileID, pid)...)
		}
	}
	// The orphan store holds schema-versioned page docs too, and List cannot reach it
	// (it has no note.json). Walking it here is what keeps a parked page from a note
	// deleted long ago blocking an unrelated ingest: adoption reads it through the
	// gated reader, so a stale entry would abort a write it has nothing to do with.
	parked, err := a.sortedPageIDs(orphanDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	for _, pid := range parked {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		emit(&out, opts, a.migratePage(orphanDir, pid)...)
	}
	return out, nil
}

// MigratePages migrates the given pages' JSON plus each owning note's note.json
// (once per note). Notes come before their pages, both in sorted order. An unknown
// PAGEID yields a result with Err rather than aborting the batch; a cancelled ctx
// stops it between files, as in MigrateAll.
func (a *Archive) MigratePages(ctx context.Context, pageIDs []string, opts MigrateOptions) ([]MigrateResult, error) {
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
			emit(&out, opts, MigrateResult{Kind: kindPage.String(), ID: pid,
				Err: fmt.Errorf("page %s: %w", pid, ErrNotFound)})
			continue
		}
		if _, seen := notes[fileID]; !seen {
			noteOrder = append(noteOrder, fileID)
		}
		notes[fileID] = append(notes[fileID], pid)
	}
	sort.Strings(noteOrder)

	for _, fileID := range noteOrder {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		emit(&out, opts, a.migrateNote(fileID))
		pages := notes[fileID]
		sort.Strings(pages)
		for _, pid := range pages {
			if err := ctx.Err(); err != nil {
				return out, err
			}
			emit(&out, opts, a.migratePage(fileID, pid)...)
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

// migratePage migrates the page's <PAGEID>.json plus, as a second result, its
// unversioned <PAGEID>.md.diff sidecar when that still holds the legacy JSON form
// (migrateEditDiff). The sidecar pass is content-sniffed and runs regardless of
// the JSON outcome, so it is idempotent and self-heals a crash between the two
// writes; it contributes a result only when it actually converted or failed.
func (a *Archive) migratePage(fileID, pageID string) []MigrateResult {
	path := filepath.Join(a.Root, fileID, pageID+".json")
	outcome, err := a.migrateFile(path, kindPage)
	res := []MigrateResult{{Kind: kindPage.String(), ID: pageID, Outcome: outcome, Err: err}}
	if r, ok := a.migrateEditDiff(fileID, pageID); ok {
		res = append(res, r)
	}
	return res
}

// migrateEditDiff converts a page's <PAGEID>.md.diff from the legacy JSON patch to
// a unified diff. ok is false when there is nothing to report (no sidecar, or it
// is already unified). It reads/writes the sidecar raw (like every other migrate
// reader), so it works on a stale archive. A degenerate legacy patch (no hunks)
// removes the sidecar, since it encodes no divergence.
func (a *Archive) migrateEditDiff(fileID, pageID string) (MigrateResult, bool) {
	fail := func(err error) (MigrateResult, bool) {
		return MigrateResult{Kind: "diff", ID: pageID, Err: err}, true
	}
	old, err := a.readEditDiff(fileID, pageID)
	if err != nil {
		return fail(err)
	}
	if old == "" {
		return MigrateResult{}, false
	}
	unified, wasLegacy, err := textmerge.ConvertLegacyDiff(old)
	if err != nil {
		return fail(fmt.Errorf("%s.md.diff: %w", pageID, err))
	}
	if !wasLegacy {
		return MigrateResult{}, false // already a unified diff.
	}
	if unified == "" {
		if err := a.removeEditDiff(fileID, pageID); err != nil {
			return fail(err)
		}
	} else if err := a.writeEditDiff(fileID, pageID, unified); err != nil {
		return fail(err)
	}
	return MigrateResult{Kind: "diff", ID: pageID, Outcome: MigrateUpgraded}, true
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
	if _, err := writeJSONIfChanged(path, doc); err != nil {
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
