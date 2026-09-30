package snorg_test

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"testing"

	snorg "github.com/jdlugosz963/snorg/pkg/snorg"
)

// TestPublicSurface uses only the public snorg package — importing no internal/*
// path — to prove an external consumer can open an archive and drive it entirely
// through the facade (the re-exported aliases make every returned type nameable).
func TestPublicSurface(t *testing.T) {
	dir := t.TempDir()

	c, err := snorg.Open(dir, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	ids, err := c.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(ids) != 0 {
		t.Errorf("empty archive List = %v, want none", ids)
	}

	// Compose predicates and parse an expression through the public API.
	pred, err := snorg.ParseQuery("NOT starred AND (tag:work OR templated)")
	if err != nil {
		t.Fatalf("ParseQuery: %v", err)
	}
	matches, err := c.Query(snorg.MatchAnd(pred, snorg.MatchAll))
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	// Name the returned alias types to prove they are usable from outside.
	var _ []snorg.Match = matches

	if from, to, err := snorg.ParseDateSpec("2026-07-01.."); err != nil || from != "20260701" || to != "" {
		t.Errorf("ParseDateSpec = (%q, %q, %v)", from, to, err)
	}

	// The matcher constructors and the Match* family they feed are nameable from
	// outside, hand-written matchers included.
	var _ snorg.TextMatcher = snorg.Substring("work")
	var _ snorg.TextMatcher = snorg.Exact("work")
	var _ snorg.TextMatcher = snorg.Regexp(regexp.MustCompile("^work$"))
	var _ snorg.Predicate = snorg.MatchOr(
		snorg.MatchTag(func(s string) bool { return s == "work" }),
		snorg.MatchRegion("grade", snorg.Substring("A")),
		snorg.MatchTemplated,
	)
	// A hand-written predicate names the candidate's fields and reports a read it
	// could not complete, which is the whole error path Query surfaces.
	var _ snorg.Predicate = func(p snorg.Page) bool {
		var _ *snorg.Client = p.Client
		var _ snorg.NoteDoc = p.Note
		var _ snorg.PageDoc = p.Doc
		p.Fail(errors.New("cannot decide"))
		return false
	}
	// A bad regexp in an expression is reported rather than panicking.
	if _, err := snorg.ParseQuery("region[grade]~("); err == nil {
		t.Error("ParseQuery with an invalid regexp should error")
	}
	tags, err := c.Tags()
	if err != nil {
		t.Fatalf("Tags: %v", err)
	}
	if len(tags) != 0 {
		t.Errorf("empty archive Tags = %v, want none", tags)
	}
	// Name the alias, and prove Tag is callable (an unknown page is an error).
	var _ []snorg.ValueCount = tags
	if _, err := c.Tag([]string{"Pmissing"}, "work"); err == nil {
		t.Error("Tag on an unknown PAGEID should error")
	}
	// Same for the note-level tag, plus the inheritance rule it feeds.
	if _, err := c.TagNote([]string{"Fmissing"}, "work"); err == nil {
		t.Error("TagNote on an unknown FILE_ID should error")
	}
	if got := snorg.EffectiveTags(snorg.NoteDoc{Tags: []string{"note"}}, snorg.PageDoc{Tags: []string{"own"}}); len(got) != 2 {
		t.Errorf("EffectiveTags = %v, want the 2-element union", got)
	}

	// Config is nameable and its sections reachable via fields.
	cfg, err := snorg.LoadConfig(nil)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	var _ *snorg.Config = cfg
	_ = cfg.Provider.Model

	// The analyze surface is nameable from outside: the options struct (including
	// the result callback), the Provider seam, and the two entry points. Not
	// called — that would need credentials — but this fails to compile if a type
	// an external consumer must name stops being re-exported.
	spec := c.AnalyzeSpec()
	var _ snorg.AnalyzeOptions = snorg.AnalyzeOptions{
		Force: true,
		Spec:  &spec,
		OnResult: func(r snorg.AnalyzeResult) {
			// Every PageResult fact is promoted through the embedding.
			var _ string = r.PageID
			var _ bool = r.Skipped
			var _ *snorg.TextChange = r.Content
			var _ []snorg.RegionResult = r.Regions
			var _ []snorg.NameChange = r.Names
			var _ []string = r.Fields
			var _ int = r.Calls
			var _ bool = r.JSONChanged
			var _ snorg.PageResult = r.PageResult
		},
	}
	var _ func() (snorg.Provider, error) = c.NewProvider
	var _ func(context.Context, snorg.Provider, []string, snorg.AnalyzeOptions) ([]snorg.AnalyzeResult, error) = c.Analyze

	// Rendering: a rect (the aliased snote.Rect, as carried by a PageDoc title or
	// a templates: box) crops straight to PNG bytes, in either the canonical or
	// the styled form.
	var _ func(string, string, snorg.Rect, bool) ([]byte, error) = c.RenderRegion

	// The change vocabulary and its two renderers. The edit surface had no guard
	// before, which is how its result type could drift from the analyze one.
	var _ snorg.TextKind = snorg.TextUnchanged
	var _ snorg.TextKind = snorg.TextNew
	var _ snorg.TextKind = snorg.TextUpdated
	var _ snorg.TextKind = snorg.TextCleared
	var _ snorg.TextKind = snorg.TextReverted
	var tc snorg.TextChange
	var _ snorg.TextKind = tc.Kind
	var _ bool = tc.Conflicts
	var _, _ string = tc.Was, tc.Now
	var _ bool = tc.Changed()
	var _ func(string, string) snorg.TextChange = snorg.TextChangeOf
	var _ func(snorg.TextChange) (string, error) = snorg.TextDiff
	var _ func(snorg.TextChange) (int, int, error) = snorg.TextStat

	var rc snorg.RegionChange
	var _, _ string = rc.ID, rc.Label
	var _ snorg.TextChange = rc.Text
	var nc snorg.NameChange
	var _ string = nc.Kind
	var _ int = nc.Index
	var _ bool = nc.Override

	// The editor round-trip: buffer out, buffer in, one PageEdit describing what
	// the save changed.
	var _ func(string) (string, error) = c.PageBuffer
	var _ func(string, string) (snorg.PageEdit, error) = c.ApplyPage
	var _ func(string, string) (snorg.PageEdit, error) = c.EditPage
	var pe snorg.PageEdit
	var _ snorg.TextChange = pe.Content
	var _ []snorg.RegionChange = pe.Regions
	var _ []snorg.NameChange = pe.Names

	// The three batch commands all stream their per-item results the same way.
	var _ func([]string, snorg.IngestOptions) ([]snorg.IngestResult, error) = c.Ingest
	var _ func([]string, snorg.MigrateOptions) ([]snorg.MigrateResult, error) = c.Migrate
	var _ func(snorg.MigrateOptions) ([]snorg.MigrateResult, error) = c.MigrateAll
	var _ snorg.IngestOptions = snorg.IngestOptions{OnResult: func(snorg.IngestResult) {}}
	var _ snorg.MigrateOptions = snorg.MigrateOptions{OnResult: func(snorg.MigrateResult) {}}
}

// TestAliasSurface names every re-exported alias and its key fields, plus the
// accessors TestPublicSurface does not reach. The aliases make internal struct
// layout public API, so a rename in internal/* must fail here rather than
// silently break an external caller.
func TestAliasSurface(t *testing.T) {
	// Config sections.
	var cfg snorg.Config
	var _ string = cfg.Archive
	var _ snorg.ProviderConfig = cfg.Provider
	var _ snorg.Analysis = cfg.Analysis
	var _ snorg.Export = cfg.Export
	var _ snorg.Ingest = cfg.Ingest
	var _ []snorg.TemplateConfig = cfg.Templates
	var _, _, _, _ string = cfg.Provider.Endpoint, cfg.Provider.APIKey, cfg.Provider.APIKeyCommand, cfg.Provider.Model
	var _ snorg.Task = cfg.Analysis.Content
	var _, _ snorg.Task = cfg.Analysis.Titles, cfg.Analysis.Links
	var _ map[string]snorg.Task = cfg.Analysis.Fields
	var _, _ string = snorg.Task{}.Prompt, snorg.Task{}.UpdatePrompt
	var _ string = cfg.Export.Template
	var _ snorg.SVGToggles = cfg.Ingest.SVG
	var svg snorg.SVGToggles
	var _, _, _ *bool = svg.Links, svg.Navigation, svg.Format
	var _ string = svg.Background
	var _ map[string]string = svg.Colors
	var tc snorg.TemplateConfig
	var _ string = tc.Image
	var _ []snorg.BoxConfig = tc.Boxes
	var bc snorg.BoxConfig
	var _, _, _ string = bc.ID, bc.Label, bc.Prompt
	var _ snorg.Rect = bc.Rect
	var _ bool = bc.Analyze

	// The retrieve view tree.
	var res snorg.Result
	var _ string = res.Archive
	var _ []*snorg.NoteView = res.Notes
	var nv snorg.NoteView
	var _, _, _, _ string = nv.FileID, nv.Signature, nv.Device, nv.Source
	var _ []string = nv.Tags
	var _ []snorg.PageView = nv.Pages
	var _ func() string = nv.Name
	var pv snorg.PageView
	var _ int = pv.Number
	var _, _ string = pv.PageID, pv.SVG
	var _ bool = pv.Starred
	var _ []string = pv.Tags
	var _ []snorg.TitleView = pv.Titles
	var _ []snorg.KeywordView = pv.Keywords
	var _ []snorg.LinkView = pv.Links
	var pav snorg.PageAnalysisView
	var _ string = pav.Content
	var _ map[string]string = pav.Fields
	var _ []snorg.RegionView = pav.Regions
	var rv snorg.RegionView
	var _, _, _ string = rv.ID, rv.Label, rv.Content
	var _ snorg.Rect = rv.Rect
	var tv snorg.TitleView
	var _ snorg.Rect = tv.Rect
	var _ int = tv.Level
	var _ *snorg.NameAnalysisView = tv.Analysis
	var _ string = snorg.NameAnalysisView{}.Name
	var _ string = snorg.KeywordView{}.Text
	var lv snorg.LinkView
	var _ snorg.Rect = lv.Rect
	var _, _, _, _, _ string = lv.Kind, lv.TargetPageID, lv.TargetFileID, lv.Target, lv.Name
	var _ int = lv.TargetDocPage
	var _ bool = lv.Internal
	var _ *snorg.NameAnalysisView = lv.Analysis

	// The on-disk documents.
	var nd snorg.NoteDoc
	var _ int = nd.SchemaVersion
	var _, _, _, _ string = nd.FileID, nd.Signature, nd.Device, nd.Source
	var _ []string = nd.Tags
	var _ []snorg.NotePageRef = nd.Pages
	var _ string = snorg.NotePageRef{}.ID
	var _ int = snorg.NotePageRef{}.Number
	var pd snorg.PageDoc
	var _ int = pd.SchemaVersion
	var _, _ string = pd.PageID, pd.BackgroundHash
	var _ bool = pd.Starred
	var _ []string = pd.Tags
	var _ []snorg.TitleDoc = pd.Titles
	var _ []snorg.KeywordDoc = pd.Keywords
	var _ []snorg.LinkDoc = pd.Links
	var _ *snorg.PageAnalysis = pd.Analysis
	var pa snorg.PageAnalysis
	var _ string = pa.SourceHash
	var _ map[string]string = pa.Fields
	var _ []snorg.RegionDoc = pa.Regions
	var _, _ string = snorg.RegionDoc{}.ID, snorg.RegionDoc{}.SourceHash
	var td snorg.TitleDoc
	var _ snorg.Rect = td.Rect
	var _ int = td.Level
	var _ *snorg.TitleAnalysis = td.Analysis
	var _ string = snorg.TitleAnalysis{}.Name
	var _ bool = snorg.TitleAnalysis{}.Edited
	var _ string = snorg.KeywordDoc{}.Text
	var ld snorg.LinkDoc
	var _ snorg.Rect = ld.Rect
	var _, _, _, _, _ string = ld.Kind, ld.TargetPageID, ld.TargetFileID, ld.Target, ld.Name
	var _ int = ld.TargetDocPage
	var _ *snorg.LinkAnalysis = ld.Analysis
	var _ string = snorg.LinkAnalysis{}.Name
	var _ bool = snorg.LinkAnalysis{}.Edited

	// The device model.
	var n snorg.Note
	var _, _, _ string = n.FileID, n.Signature, n.Device
	var _ []snorg.NotePage = n.Pages
	var np snorg.NotePage
	var _ string = np.ID
	var _ int = np.Number
	var _ bool = np.Starred
	var _ []snorg.Title = np.Titles
	var _ []snorg.Keyword = np.Keywords
	var _ []snorg.Link = np.Links
	var _ snorg.Rect = snorg.Title{}.Rect
	var _ int = snorg.Title{}.Level
	var _ string = snorg.Keyword{}.Text
	var lk snorg.Link
	var _ snorg.Rect = lk.Rect
	var _ snorg.LinkKind = lk.Kind
	var _ = []snorg.LinkKind{snorg.LinkUnknown, snorg.LinkNote, snorg.LinkFile, snorg.LinkWeb}
	var _, _, _, _ string = lk.TargetPageID, lk.TargetFileID, lk.Target, lk.Name
	var _ int = lk.TargetDocPage
	var r snorg.Rect
	var _, _, _, _ int = r.X, r.Y, r.W, r.H

	// Query, templates, inventory.
	var m snorg.Match
	var _, _ string = m.FileID, m.PageID
	var ts snorg.Templates
	var _ []*snorg.Template = ts.List
	var _ func(string) *snorg.Template = ts.MatchBackground
	var tmpl snorg.Template
	var _, _ string = tmpl.Hash, tmpl.Image
	var _ []snorg.Box = tmpl.Boxes
	var b snorg.Box
	var _, _, _ string = b.ID, b.Label, b.Prompt
	var _ snorg.Rect = b.Rect
	var _ bool = b.Analyze
	var vc snorg.ValueCount
	var _ string = vc.Value
	var _ int = vc.Count

	// Ingest and migrate reports.
	var ir snorg.IngestResult
	var _ string = ir.Path
	var _ *snorg.Note = ir.Note
	var _ *snorg.WriteReport = ir.Report
	var _ error = ir.Err
	var wr snorg.WriteReport
	var _ string = wr.FileID
	var _ bool = wr.NoteChanged
	var _ []snorg.PageWriteReport = wr.Pages
	var _, _, _ []string = wr.Pruned, wr.Parked, wr.Repaired
	var pw snorg.PageWriteReport
	var _ string = pw.PageID
	var _, _, _, _ bool = pw.New, pw.JSONChanged, pw.SVGChanged, pw.BackgroundChanged
	var _ []string = pw.AdoptedFrom
	var _ []snorg.DroppedRegion = pw.DroppedRegions
	var _ string = snorg.DroppedRegion{}.Kind
	var _ snorg.Rect = snorg.DroppedRegion{}.Rect
	var mr snorg.MigrateResult
	var _, _ string = mr.Kind, mr.ID
	var _ snorg.MigrateOutcome = mr.Outcome
	var _ error = mr.Err
	var _ snorg.MigrateOutcome = snorg.MigrateCurrent
	var _ snorg.MigrateOutcome = snorg.MigrateUpgraded
	var _ int = snorg.CurrentSchemaVersion

	// Analyze primitives.
	var sp snorg.Spec
	var _, _, _, _ string = sp.Content, sp.Update, sp.Title, sp.Link
	var _ []snorg.Field = sp.Fields
	var _, _ string = snorg.Field{}.Name, snorg.Field{}.Prompt
	var rr snorg.RegionResult
	var _ snorg.RegionChange = rr.RegionChange
	var _, _ bool = rr.Skipped, rr.Blank
	var _ func(snorg.Transcriber, context.Context, string, []byte) (string, error) = snorg.Transcriber.Transcribe
	var _ func(snorg.Generator, context.Context, string, string) (string, error) = snorg.Generator.Generate
}

// TestAccessorSurface covers the Client accessors and package functions no other
// public test reaches, and the sentinel errors, on an empty archive.
func TestAccessorSurface(t *testing.T) {
	c, err := snorg.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	var _ string = c.ArchivePath()
	var _ *snorg.Config = c.Config()
	var _ func(string) (snorg.NoteDoc, error) = c.ReadNote
	var _ func(string, string) (snorg.PageDoc, error) = c.ReadPage
	var _ func(string, string) ([]byte, error) = c.ReadSVG
	var _ func(string, string) (string, error) = c.ReadAnalysis
	var _ func() *snorg.Templates = c.Templates
	var _ func(string) (string, error) = c.FindPage
	var _ func() ([]snorg.ValueCount, error) = c.Keywords
	var _ func([]string, string) (int, error) = c.Tag
	var _ func([]string, string) (int, error) = c.Untag
	var _ func([]string, string) (int, error) = c.TagNote
	var _ func([]string, string) (int, error) = c.UntagNote
	var _ func(*snorg.Result, snorg.ServeOptions) http.Handler = c.ServeHandler
	var _ func(*snorg.Result, string) (string, error) = snorg.RenderTemplate
	var _ func(string, string, string) (snorg.Provider, error) = snorg.NewOpenAIProvider
	var _ func(string) ([]string, error) = snorg.NoteFiles
	var _ func() (string, error) = snorg.EditorFromEnv
	var _ func([]string) (*snorg.Config, error) = snorg.LoadConfig
	var _ func(snorg.ResolveOptions) (*snorg.Client, error) = snorg.Resolve

	// An unknown id is ErrNotFound on every lookup path.
	if _, err := c.FindPage("Pmissing"); !errors.Is(err, snorg.ErrNotFound) {
		t.Errorf("FindPage err = %v, want ErrNotFound", err)
	}
	if _, err := c.Retrieve([]string{"Pmissing"}); !errors.Is(err, snorg.ErrNotFound) {
		t.Errorf("Retrieve err = %v, want ErrNotFound", err)
	}
	if _, err := c.ReadNote("Fmissing"); !errors.Is(err, snorg.ErrNotFound) {
		t.Errorf("ReadNote err = %v, want ErrNotFound", err)
	}
	if _, err := c.Tag([]string{"Pmissing"}, "work"); !errors.Is(err, snorg.ErrNotFound) {
		t.Errorf("Tag err = %v, want ErrNotFound", err)
	}
	if _, err := c.TagNote([]string{"Fmissing"}, "work"); !errors.Is(err, snorg.ErrNotFound) {
		t.Errorf("TagNote err = %v, want ErrNotFound", err)
	}
	if _, err := c.Export(nil); !errors.Is(err, snorg.ErrNoExportTemplate) {
		t.Errorf("Export err = %v, want ErrNoExportTemplate", err)
	}
	var _ error = snorg.ErrSchemaVersion

	// The rendering and serving entry points run on an empty selection.
	if out, err := snorg.RenderTemplate(&snorg.Result{}, "{{ notes|length }}"); err != nil || out != "0" {
		t.Errorf("RenderTemplate = (%q, %v), want (\"0\", nil)", out, err)
	}
	if h := c.ServeHandler(&snorg.Result{}, snorg.ServeOptions{}); h == nil {
		t.Error("ServeHandler returned nil")
	}
	if kws, err := c.Keywords(); err != nil || len(kws) != 0 {
		t.Errorf("Keywords = (%v, %v), want none", kws, err)
	}

	// The note display name: source sans .note, else the FILE_ID.
	if got := (snorg.NoteDoc{FileID: "F1", Source: "x.note"}).Name(); got != "x" {
		t.Errorf("Name = %q, want x", got)
	}
	if got := (snorg.NoteDoc{FileID: "F1"}).Name(); got != "F1" {
		t.Errorf("Name fallback = %q, want F1", got)
	}
}
