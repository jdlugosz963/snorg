# Library API (`pkg/snorg`)

snorg's capabilities are usable from Go, not only the CLI. Import the public
package; the `internal/*` packages are not importable from outside the module, so
this package is the whole supported surface.

```go
import snorg "github.com/jdlugosz963/snorg/pkg/snorg"
```

This page is the guide: what the pieces are for and why they are shaped as they
are. For the mechanical reference — every signature — read the package on
pkg.go.dev or render it from the source with `gomarkdoc ./pkg/snorg`; no generated
reference is checked in.

## Client

A `Client` bundles an archive root with merged configuration.

- `Open(archivePath string, cfg *Config) (*Client, error)` — explicit root; `cfg`
  may be `nil` for built-in defaults. Build `cfg` with `LoadConfig(paths)`.
- `Resolve(ResolveOptions) (*Client, error)` — the CLI's resolution: archive path
  from the option or the config's `archive:` key (`~` expanded), config layered
  XDG user → `-c` files (later wins).

`Client.ArchivePath()` / `Client.Config()` expose the resolved root and config.
`Config()` is a **snapshot**: mutating it changes nothing (configure through
`LoadConfig`/`Open`), which is also what keeps the credential `NewProvider` resolves
off a struct callers can marshal.

Errors worth branching on are sentinels for `errors.Is`: `ErrNotFound` (an unknown
PAGEID/FILE_ID), `ErrSchemaVersion` (a stale file — run `Migrate`) and
`ErrNoExportTemplate`.

## Capabilities

| Method | Does |
|---|---|
| `List()` | archived FILE_IDs |
| `Keywords()` / `Tags()` | distinct device keywords / snorg tags with per-value page counts (`[]ValueCount`) |
| `Query(pred)` | pages matching a `Predicate` |
| `ParseQuery(expr)` (package func) | compile a query expression into a `Predicate` (terms joined by `AND`/`OR`/`NOT`; see `QuerySyntax`) |
| `ReadAnalysis(fileID, pageID)` | a page's transcription (the `<PAGEID>.md` sidecar); empty when never analyzed |
| `Templates()` | the template set built from the config's `templates:` section (already resolved by `Open`) |
| `Tag(tag, pageIDs, remove)` | add/remove a snorg-managed tag on pages (independent of device keywords); returns the count changed |
| `TagNote(tag, fileIDs, remove)` | same, but note-scoped: stored in `note.json` only and inherited by every page of the note; returns the count of notes changed |
| `EffectiveTags(nd, pd)` (package func) | a page's effective tag set: its own tags unioned with its note's — the inheritance rule every read surface applies |
| `Retrieve(pageIDs)` | assemble pages into a `*Result` (`{Archive, Notes}`) |
| `ReadNote/ReadPage/ReadSVG/FindPage` | raw on-disk document access |
| `Ingest(paths, opts)` | register `.note` files (`NoteFiles(dir)` enumerates them); `IngestOptions.OnResult` streams each note as it lands; each `IngestResult.Report` (`*WriteReport`) says what the incremental reconcile changed |
| `Export(pageIDs)` | render through the config's template (`RenderTemplate` for an arbitrary one) |
| `ServeHandler(pageIDs, flat)` | the built-in viewer as an `http.Handler` (empty = whole archive) |
| `NewProvider()` | resolve the API key, validate the provider config, build the configured backend — call once at startup, reuse across batches |
| `Analyze(ctx, prov, pageIDs, opts)` | vision-LLM transcription with a caller-owned `Provider`; each `AnalyzeResult` embeds a `PageResult` saying what the page cost and what moved |
| `Migrate(pageIDs, opts)` / `MigrateAll(opts)` | schema upgrade; `MigrateOptions{OnResult}` streams per-file results |
| `PageBuffer(id)` / `ApplyPage(id, buf)` | programmatic transcription edit — no `$EDITOR`; returns a `PageEdit` |
| `TextChangeOf/TextDiff/TextStat` (package funcs) | build a `TextChange`, render it as a unified diff, count its `±` lines |
| `RenderRegion(fileID, pageID, rect, styled)` | crop a page rect to PNG bytes at native 1920x2560 resolution |

### Predicates

A `Predicate` is `func(Page) bool`, where `Page` is one candidate: the two documents
as stored (`Note`, `Doc`) plus the `Client` they came from. The client is what makes
every filter a standalone function — `MatchContent` reads the transcription through
`p.Client.ReadAnalysis`, `MatchRegion` resolves boxes through `p.Client.Templates`,
and a hand-written predicate can do the same:

```go
c.Query(func(p snorg.Page) bool {
	md, err := p.Client.ReadAnalysis(p.Note.FileID, p.Doc.PageID)
	return err == nil && p.Doc.Starred && strings.Contains(md, "TODO")
})
```

The family is `MatchAll`, `MatchStarred`, `MatchUnanalyzed`, `MatchTemplated`,
`MatchNot`, `MatchAnd`, `MatchOr`, `MatchIDs`, `MatchNote`, `MatchKeyword`,
`MatchTag`, `MatchDate`, `MatchContent`, `MatchRegion` — one prefix, so they cluster
in the reference and leave the plain nouns to the types. `MatchTag` matches inherited
tags because the candidate carries the owning note; `MatchTemplated`/`MatchRegion`
need no error return because `Open` resolves the `templates:` section once and fails
there if it is broken.

Every text filter takes a `TextMatcher` (`func(string) bool`) rather than a regexp,
which is why one `MatchTag` covers all three of the language's operators:

```go
snorg.MatchTag(snorg.Substring("work"))                          // tag:work
snorg.MatchTag(snorg.Regexp(regexp.MustCompile("^wo")))          // tag~^wo
snorg.MatchTag(snorg.Exact("work"))                              // tag=work
snorg.MatchTag(func(s string) bool { return len(s) > 8 })        // no spelling at all
```

`ParseQuery` is the same vocabulary reached from a string — it parses the expression
(`internal/querylang` owns the grammar), then compiles each term into exactly these
predicates, so a front-end can accept the user's expression verbatim and get a
`Predicate` it can still combine in Go.

### Rendering

`RenderRegion` is the way to get pixels out of the archive. `rect` is a `Rect` in
the page's 1920x2560 pixel space — the same shape and units as a `PageDoc` title
or link rect and a `templates:` box — so a rect read from the archive or the
config crops exactly its region; pass `Rect{W: 1920, H: 2560}` for the whole page.

`styled` picks the image. The default (`false`) is the **canonical** render —
every stroke forced black, nav/link overlays dropped, invariant under the
`ingest.svg` styling config — byte-identical to the crop `Analyze` sends to the
vision model, so it is reproducible across restyles. `true` rasterizes the stored
SVG as-is, keeping pen shades and baked overlays. Neither draws the template
background image.

```go
fid, _ := c.FindPage(pageID)
png, err := c.RenderRegion(fid, pageID, snorg.Rect{X: 0, Y: 0, W: 1920, H: 384}, false)
```

### Analyzing

The `Provider` (a vision `Transcriber` + text `Generator`) is the caller's to own:
build it once with `NewProvider()` — which resolves the key (`api_key` >
`api_key_command` stdout > `$OPENAI_API_KEY`), validates the `provider:` section and
constructs the backend — and pass it to every `Analyze`. Bad credentials then fail at
startup instead of on the first page, and `api_key_command` runs once rather than per
batch — the resolved key is cached on the `Client`, deliberately not written back into
the configuration. `NewOpenAIProvider(endpoint, key, model)` builds one from explicit credentials
instead of the config; any type satisfying `Provider` works, so a test double or an
alternate backend drops straight in.

```go
prov, err := c.NewProvider()
if err != nil { log.Fatal(err) } // credentials checked here, once

_, err = c.Analyze(ctx, prov, ids, snorg.AnalyzeOptions{
    Force:    false,
    OnResult: func(r snorg.AnalyzeResult) {
        switch {
        case r.Err != nil:
            log.Printf("%s: %v", r.PageID, r.Err)
        case r.Skipped:
            log.Printf("%s: skipped", r.PageID)
        default:
            added, removed, _ := snorg.TextStat(*r.Content)
            log.Printf("%s: %s +%d/-%d (%d calls)", r.PageID, r.Content.Kind, added, removed, r.Calls)
        }
    },
})
```

A page with **no ink** — and, on a templated page, each box whose rect holds no
ink — is transcribed as empty without any model call (custom fields are skipped
too, since there is no content to derive them from). The page still runs the
ordinary chain, so an erased page's text and fields clear through the same 3-way
merge (leaving no `<PAGEID>.md` at all — an empty transcription is no
transcription), and a transcription the user typed by hand on a blank page survives
untouched rather than conflicting. On a templated page a blank box is visible as
`RegionResult.Blank`; at page level there is no separate flag, because
`PageResult.Calls` already says the page cost nothing and `Content.Kind` says what
became of its text.

`AnalyzeOptions.OnResult` fires with each page's result as it lands — before the next
page is analyzed — so a consumer can log or commit incrementally instead of waiting on
the whole batch (pages run sequentially for LLM rate limits). Every result also comes
back in the returned slice. A page failure lands in its `AnalyzeResult.Err` and the
batch continues; a cancelled `ctx` stops it, returning the results so far plus
`ctx.Err()`. `AnalyzeOptions.Spec` overrides the config's prompts (`nil` =
`AnalyzeSpec()`), which is how a single page runs with a bespoke prompt.

### Knowing what changed

Every write-side call reports what it did, in one shared vocabulary, so a consumer
never re-reads the archive to find out.

**The parts.** A `TextChange` is one text document's transition: `Kind`
(`TextNew`/`TextUpdated`/`TextCleared`/`TextUnchanged`/`TextReverted`), `Conflicts`
(3-way-merge markers were written), and `Was`/`Now` — the actual before and after
text in stored form, so you can render the change yourself. `Changed()` is the
filter to apply before saying anything. Line counts are *not* stored: call
`TextStat(c)` for `±` counts or `TextDiff(c)` for a unified diff, so a caller that
never displays a change never pays for its diff. `NameChange` is one title/link
rename — `Kind` (`"title"`/`"link"`), 1-based `Index` (the key the `analyze-edit`
buffer's markers use), `Was`/`Now`, and `Override` (stored as a user override that
analysis then honors). `RegionChange` is one template box's `ID`/`Label` plus its
`Text`.

`TextReverted` is only ever produced by an edit that lands exactly on the stored AI
base; analysis cannot revert, and emptying a page that never had an AI base reads
`TextCleared` rather than "reverted to nothing".

**The assemblies.** `Analyze` returns `PageResult`s: `Skipped` (fingerprint matched,
no work), `Content` (`*TextChange`, nil on a templated page), `Regions`
(`[]RegionResult`, nil on every other page), `Names`, `Fields` (regenerated field
names, sorted), `Calls` (LLM round-trips this page cost) and `JSONChanged`. A
`RegionResult` embeds a `RegionChange` and adds the two cost facts only analysis can
produce — `Skipped` (fingerprint matched, previous text reused) and `Blank` (the
rect held no ink). Those live there rather than on the shared `RegionChange` so no
field on a value you hold is permanently zero: an edit produces regions, but never
those.

`EditPage`/`ApplyPage` return a `PageEdit`: `Content`, `Regions`
(`[]RegionChange` — one per section written, including `analyze:false` boxes and
tombstones, which the editor does let a user edit), `Names` and `JSONChanged`.

**Ingest** keeps its own report, since it reconciles files rather than text:
`IngestResult.Report` (`*WriteReport`) gives `NoteChanged`, the `Pruned` page ids,
and `Pages` (`[]PageWriteReport`) — one entry per page newly written (`New`) or
whose bytes changed (`JSONChanged`/`SVGChanged`/`BackgroundChanged`); an unchanged
page produces no entry, so an already-current re-ingest yields an empty report. Each
`PageWriteReport.DroppedRegions` lists title/link analyses that could not be carried
forward because their rect moved (they are re-transcribed on the next `Analyze`).

**Streaming.** All three batch calls hand results back as they land rather than only
at the end: `AnalyzeOptions.OnResult`, `IngestOptions.OnResult` and
`MigrateOptions.OnResult`. All three run sequentially, so every callback fires in the
returned slice's order, on the calling goroutine — snorg runs no goroutines outside
`internal/serve`.

Editing: `PageBuffer`/`ApplyPage` are the library edit path (round-trip the buffer
yourself). `EditPage(id, editor)` and `EditorFromEnv()` are the interactive
convenience the CLI's `analyze-edit` uses — they spawn `$EDITOR`, so a library that
supplies its own UI should use `PageBuffer`/`ApplyPage` instead.

## Example

```go
c, err := snorg.Open("/path/to/archive", nil)
if err != nil { log.Fatal(err) }

pred, _ := snorg.ParseQuery("date:today AND starred")
matches, _ := c.Query(pred)

ids := make([]string, len(matches))
for i, m := range matches { ids[i] = m.PageID }

res, _ := c.Retrieve(ids) // *snorg.Result
for _, n := range res.Notes {
    for _, p := range n.Pages {
        fmt.Println(p.PageID, p.Analysis.Content)
    }
}
```

All types a method returns are re-exported from this package as aliases
(`snorg.Result`, `snorg.NoteView`, `snorg.Match`, `snorg.Spec`, `snorg.Config`, …),
so importing `pkg/snorg` alone is sufficient — no `internal/*` import is ever needed.
Because they are aliases, a field rename in an internal struct is a public API
change; `TestAliasSurface` names every alias and its fields so such a rename fails
the build.
