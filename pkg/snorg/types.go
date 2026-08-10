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
)

// The device-agnostic domain model, produced by ingest (see IngestResult.Note). A
// page's keywords are reachable as Page.Keywords (the element type is not aliased,
// so the name Keyword can denote the query constructor — see query.go).
type (
	Note  = snote.Note
	Page  = snote.Page
	Title = snote.Title
	Link  = snote.Link
	Rect  = snote.Rect
)

// Query primitives: a Predicate filters pages, Client.Query returns the Matches.
type (
	Match     = query.Match
	Predicate = query.Predicate
)

// IngestResult is one note's ingest outcome (Err set when it failed).
type IngestResult = ingest.Result

// Migrate results, returned by Client.Migrate / Client.MigrateAll.
type (
	MigrateResult  = archive.MigrateResult
	MigrateOutcome = archive.MigrateOutcome
)

const (
	MigrateCurrent  = archive.MigrateCurrent
	MigrateUpgraded = archive.MigrateUpgraded
)

// CurrentSchemaVersion is the archive schema version stamped by every writer;
// migrate upgrades older files to it.
const CurrentSchemaVersion = archive.CurrentSchemaVersion

// Analyze primitives. Transcriber (image→text) and Generator (text→text) are the
// two provider seams; a Provider (see NewOpenAIProvider) satisfies both.
type (
	Spec        = analyze.Spec
	Field       = analyze.Field
	Outcome     = analyze.Outcome
	Transcriber = analyze.Transcriber
	Generator   = analyze.Generator
)

const (
	Skipped    = analyze.Skipped
	Analyzed   = analyze.Analyzed
	Updated    = analyze.Updated
	Conflicted = analyze.Conflicted
)

// EditOutcome is what ApplyPage did with an edited page buffer.
type EditOutcome = edit.Outcome

const (
	EditUnchanged = edit.Unchanged
	EditEdited    = edit.Edited
	EditReverted  = edit.Reverted
)
