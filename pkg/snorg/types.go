package snorg

import (
	"github.com/jdlugosz963/snorg/internal/analyze"
	"github.com/jdlugosz963/snorg/internal/archive"
	"github.com/jdlugosz963/snorg/internal/config"
	"github.com/jdlugosz963/snorg/internal/edit"
	"github.com/jdlugosz963/snorg/internal/ingest"
	"github.com/jdlugosz963/snorg/internal/query"
	"github.com/jdlugosz963/snorg/internal/retrieve"
	"github.com/jdlugosz963/snorg/internal/snote"
)

// The public snorg API re-exports the internal packages' types as aliases so a
// consumer that imports only this package can name and traverse every value the
// Client returns (the internal/* packages are not importable from outside the
// module). Aliases are the same type — field access and methods work unchanged.

// Config and its nested sections, as loaded from YAML by LoadConfig. The provider
// credentials are reachable as Config.Provider (the type is not aliased, so the name
// Provider can denote the analysis-backend interface — see analyze.go).
type (
	Config     = config.Config
	Analysis   = config.Analysis
	Export     = config.Export
	Ingest     = config.Ingest
	Task       = config.Task
	SVGToggles = config.SVGToggles
)

// Result is the read contract returned by Client.Retrieve: an absolute archive
// root plus the requested pages grouped per owning note. The view tree mirrors the
// on-disk JSON.
type (
	Result           = retrieve.Result
	NoteView         = retrieve.NoteView
	PageView         = retrieve.PageView
	TitleView        = retrieve.TitleView
	KeywordView      = retrieve.KeywordView
	LinkView         = retrieve.LinkView
	PageAnalysisView = retrieve.PageAnalysisView
	NameAnalysisView = retrieve.NameAnalysisView
	RegionView       = retrieve.RegionView
)

// The raw on-disk JSON documents, returned by Client.ReadNote / Client.ReadPage for
// consumers that need lower-level access than the Result view tree.
type (
	NoteDoc       = archive.NoteDoc
	NotePageRef   = archive.NotePageRef
	PageDoc       = archive.PageDoc
	PageAnalysis  = archive.PageAnalysis
	TitleDoc      = archive.TitleDoc
	KeywordDoc    = archive.KeywordDoc
	LinkDoc       = archive.LinkDoc
	TitleAnalysis = archive.TitleAnalysis
	LinkAnalysis  = archive.LinkAnalysis
	RegionDoc     = archive.RegionDoc
)

// The device-agnostic domain model, produced by ingest (see IngestResult.Note).
// NotePage is the device-parsed page; the archived page a query predicate examines
// is the separate Page in query.go.
type (
	Note     = snote.Note
	NotePage = snote.Page
	Title    = snote.Title
	Keyword  = snote.Keyword
	Link     = snote.Link
	Rect     = snote.Rect
)

// Match is one page a query predicate accepted, as returned by Client.Query. The
// Page a predicate examines and the Predicate type itself live in query.go.
type Match = query.Match

// The template regions a page can be drawn on: the resolved set (Client.Templates),
// one template and one of its boxes. Built from the config's templates: section.
type (
	Templates = archive.Templates
	Template  = archive.Template
	Box       = archive.Box
)

// ValueCount is one distinct label with its page count, as returned by the archive
// inventory accessors Client.Keywords / Client.Tags (behind `list keywords`/`tags`).
type ValueCount = query.ValueCount

// IngestResult is one note's ingest outcome (Err set when it failed); on success
// its Report says what the incremental reconcile changed on disk.
type IngestResult = ingest.Result

// The ingest change report, reachable as IngestResult.Report: which pages were newly
// written or changed, which files changed on each, which pages were pruned (and which
// of those had their state parked for reclaim), which pages were adopted from another
// note the device moved them out of, and which notes that repair rewrote.
type (
	WriteReport     = archive.WriteReport
	PageWriteReport = archive.PageWriteReport
	DroppedRegion   = archive.DroppedRegion
)

// Migrate results, returned by Client.Migrate / Client.MigrateAll.
type (
	MigrateResult  = archive.MigrateResult
	MigrateOutcome = archive.MigrateOutcome
)

// The MigrateOutcome values, reported per file in a MigrateResult: a file already
// at CurrentSchemaVersion, or one walked forward to it.
const (
	MigrateCurrent  = archive.MigrateCurrent
	MigrateUpgraded = archive.MigrateUpgraded
)

// CurrentSchemaVersion is the archive schema version stamped by every writer;
// migrate upgrades older files to it.
const CurrentSchemaVersion = archive.CurrentSchemaVersion

// The shared change vocabulary: a text document's before/after, a title/link
// rename, and one template box's change.
type (
	TextKind     = archive.TextKind
	TextChange   = archive.TextChange
	NameChange   = archive.NameChange
	RegionChange = archive.RegionChange
)

// The TextKind values a TextChange.Kind takes; only an edit produces TextReverted.
const (
	TextUnchanged = archive.TextUnchanged
	TextNew       = archive.TextNew
	TextUpdated   = archive.TextUpdated
	TextCleared   = archive.TextCleared
	TextReverted  = archive.TextReverted
)

// The per-operation results built from those parts: one analyzed page, one of its
// template boxes, one editor round-trip.
type (
	PageResult   = analyze.PageResult
	RegionResult = analyze.RegionResult
	PageEdit     = edit.PageEdit
)

// Analyze primitives. Transcriber (image→text) and Generator (text→text) are the
// two provider seams; a Provider (see NewOpenAIProvider) satisfies both.
type (
	Spec        = analyze.Spec
	Field       = analyze.Field
	Transcriber = analyze.Transcriber
	Generator   = analyze.Generator
)
