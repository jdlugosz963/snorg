# Architecture

## CLI

```
snorg [-a <archive-path>] [-c config.yaml ...] [--no-user-config] [-v] <command> [command flags] [args]

snorg [-a <archive-path>] ingest <file-or-dir>
snorg [-a <archive-path>] list [-l] | list keywords|tags [-l]
snorg [-a <archive-path>] query <expr>
snorg [-a <archive-path>] tag [-r] <tag> [PAGEID ...] | tag -n [-r] <tag> [FILE_ID ...]
snorg [-a <archive-path>] retrieve [PAGEID ...]
snorg [-a <archive-path>] analyze [--force] [PAGEID ...]
snorg [-a <archive-path>] analyze-edit <PAGEID>
snorg [-a <archive-path>] export [PAGEID ...]
snorg [-a <archive-path>] serve [-l ADDR] [--flat] [PAGEID ...]
snorg [-a <archive-path>] migrate [PAGEID ...]
```

The archive path comes from the `-a`/`--archive` global flag (optional when the XDG
user config sets `archive:`; the flag wins) and comes before the command, together
with the global config flags: the root's `Before`
hook loads the merged config once (see [config.md](config.md)) and hands it to the
command, which picks and validates only the sections it uses. The CLI is built on
`urfave/cli/v3`. `ingest` takes
a single `.note` or a directory (walked recursively for `*.note`) and ingests the
notes **one at a time** — snorg is single-threaded outside `internal/serve`, and
`Write` reaches across the whole archive (see Archive layout), so ingest is
single-writer by design; `-c` config
controls the SVG pipeline (see below). A failed note never aborts the batch — all
are attempted and failures summarized (non-zero exit). Re-ingest reconciles the
note's directory in place (see Archive layout); it is the update path. A write also
reaches beyond that directory in one case: a page the device moved into this note
from another is adopted rather than duplicated (see Page moves).

**Reporting.** The batch commands (`ingest`, `analyze`, `migrate`) report per item on
**stderr**, so stdout stays the machine-readable channel and `2>/dev/null` silences
progress without losing data. Three modes: an animated bar when stderr is a terminal,
plain one-line-per-item when it is not, and — under the global `-v`/`--verbose` — a
detailed account of what each item changed, diff included, instead of a bar. Under
`-v` an ingested note also reports the page moves: `adopted from <FILE_ID>`, `parked
<PAGEID> (transcription kept)` and `repaired <FILE_ID>/note.json`. `migrate`
draws a live counter rather than a bar, because one page can yield two results (the
JSON walk and the `.md.diff` normalization) so no denominator is knowable up front.
`query`/`list`/`retrieve`/`export` write only their data, on stdout; `tag` and
`analyze-edit` report a completed write rather than progress, so their result line
also stays on stdout.

`list`, `query` and `retrieve` are the read side — the platform-agnostic interface
external tools build on (see [retrieval.md](retrieval.md)). `list` prints FILE_IDs,
or (with the `keywords`/`tags` subcommands) the archive's distinct labels. `query`
takes one boolean **expression** — terms joined by `AND`/`OR`/`NOT` and grouped
with parentheses — and prints the PAGEID of each matching page, one per line.
A term is a standalone word (`all`, `starred`, `unanalyzed`, `templated` — the
page's `background_hash` resolves to a config `templates:` entry, so `NOT
templated` selects the free-form pages) or a field with an operator and a value:
`note`, `keyword` (device keywords, matched against `Keyword.Text`), `tag`
(snorg-managed tags, the page's own plus the ones inherited from its note),
`content` (the page's transcribed `<PAGEID>.md`), `region[<box-id>]` (the text of
one template box inside that file; unscoped = any box) and `ctime`/`mtime`/`dtime:<spec>`
(page created per its PAGEID / `modified_at` / `device_modified_at`). The
operator chooses the match: `:` substring (case-insensitive), `~` regexp, `=`
exact. So `snorg query 'starred AND (tag:work OR ctime:2026-04-04..)'`. `retrieve`, `analyze` and `export` all take PAGEIDs
as arguments, or read them one-per-line from stdin when none are given, so
`query` pipes into any of them. `tag` is the one write on the read side: it adds or
removes a snorg-managed tag on the PAGEIDs (args or stdin), the archive-side
organizing mechanism independent of device keywords. With `-n` the ids are FILE_IDs
instead (pipe from `list`) and the tag is stored on the note — in `note.json` only —
and inherited by every page of that note, so a whole note is labelled by one write.

`retrieve` prints the selected pages assembled into a JSON **object**
`{archive, notes}`: `archive` is the absolute archive root the pages'
archive-relative `svg` paths resolve against, `notes` the array of `NoteView`s
grouped per owning note (full note metadata, only the requested pages); a whole
note is `query note=<FILE_ID> | retrieve`. `export` groups the same way and
renders the config's template once over that whole object, so one template
invocation sees every selected note and can build absolute svg paths from
`{{ archive }}`.

Both `list` and `query` have a `-l`/`--long` form for human browsing: `list -l`
appends the note name (`<FILE_ID>\t<name>`), and `query -l` switches to an
annotated, tab-separated line per page (`<PAGEID>\t<note>\tp<page#>\t<*?>\t<headings>\t#<keyword>…\t@<tag>…`).
PAGEID stays the first field so `cut -f1`/`awk '{print $1}'` still extracts it,
but the annotated form is **browse-only** — never feed it downstream as the
bare-PAGEID pipe contract. The `keywords`/`tags` subcommands take `-l` too, where
it appends a per-value page count.

`analyze` processes its pages sequentially (LLM rate limits; a failed page
never aborts the batch). Unchanged pages are skipped without an LLM call (see
[config.md](config.md), "Incremental analysis"), so the canonical batch run is:

```sh
snorg -a <archive> query all | snorg -c cfg.yaml -a <archive> analyze
```

`analyze-edit` opens the page's transcription in `$VISUAL`/`$EDITOR` (exactly one
PAGEID, no stdin — the editor needs the terminal) and needs no provider config:
a page can be transcribed entirely by hand, without any LLM involved. Manual
edits survive re-analysis (see "User edits" below); `analyze` sets `Conflicts` on the
affected text where an edit and the new transcription overlap, resolved by another
`analyze-edit`.

`serve` is the built-in, zero-setup viewer: it assembles the selected pages
(`retrieve.Get`) and stands up a local HTTP site (`-l`/`--listen`, default
`127.0.0.1:8080`) — a gallery of notes (name + first-page thumbnail), each opening
a gallery of that note's pages with a click-to-enlarge lightbox that also shows the
page's transcription under the enlarged page (←/→ pages, Esc). `--flat`/`-f` drops the
per-note grouping: `/` is then one flat gallery of all selected pages (across notes,
each captioned `<note name> · <page number>`) with the same lightbox. The grouped/flat
choice lives behind a small `layout` seam (`groupedLayout` vs `flatLayout`) chosen once,
so the routes carry no mode branch. The enlarged page is an
`<object>` (a live SVG document) so its baked links stay clickable; the `/svg` route
retargets them (with `target="_top"`) to the layout's `pageHref` — `/note/{fid}?page={pid}`
grouped, `/?page={pid}` flat — so a tap reopens the right index and enlarges the target
page, and makes the SVG root responsive so it scales to fit instead of clipping. Thumbnails are small rasterized PNGs (the SVG rendered at ~400px,
so far fewer bytes than the vector) and both the thumbnail and full-SVG routes carry
`ETag`/`Cache-Control` for browser caching. No PAGEIDs and no pipe means the whole archive. Everything is in-memory: the
views are computed once, thumbnails are memoized, and the page SVGs are streamed
straight from the archive, nothing copied to disk. Needs no provider config.

`migrate` upgrades stale archive JSON to the current `schema_version` (see "Schema
versioning" below). It is the one command that reads pre-versioning/older grammars —
every other command hard-errors on a version mismatch, pointing here. Selection is
like `serve` (PAGEID args / stdin / bare = whole archive); a page selection also
migrates its owning `note.json`. Idempotent, needs no provider config.

## Archive layout (plaintext contract)

```
<archive>/config.yaml         # optional per-archive config (not auto-loaded; pass with -c); may hold the templates: section
<archive>/templates/*.png     # optional device-form background PNGs (template selectors);
                              #   image: paths are relative to the declaring config file (see docs/templates.md)
<archive>/<FILE_ID>/
    note.json          # schema_version + file metadata + tags[] (snorg-managed, note-scoped:
                       #   inherited by every page of the note) + ordered page placement (id, number)
    <PAGEID>.json      # schema_version + per page: starred, background_hash,
                       # modified_at/device_modified_at (+ device_hash), tags[] (snorg-managed),
                       # titles(rect,level,analysis), keywords(text), links(...,analysis),
                       # analysis{source_hash, fields, regions[](id,source_hash)}
    <PAGEID>.md        # per page: the transcription (Markdown), AI-produced and/or
                       # user-edited — always the effective content; for a templated
                       # page this holds the id-keyed per-box region sections instead
    <PAGEID>.md.diff   # only while user edits diverge from the AI transcription:
                       # unified diff AI-base → md (analyze-edit)
    <PAGEID>.svg       # per page rendered vector (one <path>/command per line)
    backgrounds/
        <sha256>.png   # page backgrounds, content-addressed, deduped per note
<archive>/orphans/            # the orphan store: page state parked when a page leaves
    <PAGEID>.{json,md,md.diff}#   a note, reclaimed when another note claims it
```
Stable filenames + indented JSON → clean VCS diffs. The page transcription lives
in the `<PAGEID>.md` sidecar — multiline Markdown diffs like prose, not like a
JSON string. SVGs are reflowed so each `<path>` and each `d` command sits on its
own line (`archive.formatSVG`), so a changed stroke touches only its own lines
instead of one giant line. The renderer embeds the same template background
inline in every page; `archive.extractBackground` lifts it into the note's
`backgrounds/` subfolder and the SVG references it by descendant href
(`backgrounds/<sha256>.png`), so the giant base64 line is gone and one PNG is
stored per note instead of once per page. The href must stay a descendant
path — librsvg-based viewers (Emacs, imv, rsvg) refuse to load resources from a
parent directory, so a shared archive-root folder would render only in browsers;
the per-note copy renders everywhere at the cost of duplicating templates across
notes. Page SVGs are therefore **not self-contained** — the note's `backgrounds/`
folder must travel with it. This is the contract retrieval commands and user export
scripts depend on.

**Clickable links.** Each page's tap-targets are also baked into the SVG as real
hyperlinks (`archive.injectLinks`): an invisible `<a>`-wrapped `<rect>` at the link's
pixel rect, so the region navigates without altering the note's appearance. Resolution
branches on the link **kind** (`archive.linkAnchor`): a **note** link's href is
**relative to the page SVG** — `<PAGEID>.svg` for a same-note jump, `../<FILE_ID>/<PAGEID>.svg`
for another note. A same-note jump resolves only to a page written in this `Write`;
a cross-note jump is baked **unconditionally** to its deterministic archive-relative path,
so it works regardless of ingest order — the href simply dangles until the target note is
ingested. A **web** link bakes the URL as an external `<a target="_blank">`. **File** and
**unknown** links carry only a device-local path (not in the archive), so they bake nothing.

**Page navigation.** `archive.injectNav` additionally bakes two invisible
half-page zones into each SVG: tapping the left half opens the previous page,
the right half the next one (same-note relative hrefs; first/last page get only
one zone). Nav anchors are emitted **before** the link overlays — SVG hit-testing
picks the last element in document order on overlap, so handwriting links always
win over navigation. Because each SVG embeds its neighbors, reordering pages
rewrites the affected SVGs (inherent to the feature).

**SVG pipeline stages.** The rewrites are configurable via `ingest.svg`
(`archive.SVGPipeline`): `links`/`navigation`/`format` are booleans (default on);
`background` is a mode — `extract` (default; lift the inline base64 into
`backgrounds/`), `inline` (leave it), `blank` (replace with a white rect) or
`remove` (delete the `<image>`); `colors` optionally remaps the renderer's four
default pen-shade `fill=` values (`archive.recolor`, verbatim substitution, so the
`fill="none"` overlays are untouched). Order in `Write`: background → recolor →
navigation → links → format. With overlays off, `background: inline` and no
`colors`, the renderer's SVG is stored byte-verbatim. None of these stages changes
the analyze fingerprint (the canonical black-on-white rasterization, which forces
pen colors black and drops overlays). See [config.md](config.md).

**User edits.** `analyze-edit` opens the transcription in the user's editor.
`<PAGEID>.md` always holds the *effective* content — what `retrieve`/`export`
show — while `<PAGEID>.md.diff` records how it diverges from the last
AI-produced transcription (the *base*, reconstructed by reverse-applying the
patch; the file exists iff they diverge). On re-analysis the LLM is prompted
with the base — **user edits never reach the LLM** — and the fresh output is
3-way merged (base, user's md, new transcription); the md becomes the merge
result and the diff is rebased onto the new base. Overlaps leave standard
conflict markers (`<<<<<<< edited` / `>>>>>>> reanalyzed`) in the md and set
`TextChange.Conflicts` on the reported change; resolving is another `analyze-edit`. A page never analyzed
by AI has an empty base, so a hand-written transcription meets its first AI run
as one conflict to resolve once. The diff/merge is pure Go (no PATH tool),
isolated in `internal/textmerge`.

**Page moves.** A PAGEID is minted by the device and names exactly one page in
exactly one note directory. That is an invariant `archive.Write` **enforces**, not one
it assumes: a page can be moved to another note on the tablet, and re-ingesting only
that note would otherwise store the page twice — the new copy stripped of the
analysis, tags and transcription still sitting in the old one, which the old note's
next ingest would then delete. So `Write` looks archive-wide for any page it is about
to write (`archive.locateForeign`, in the preflight, through the gated readers, before
the first mutation — a stale donor aborts the write with nothing moved). A page found
elsewhere is **moved**: its non-regenerable sidecars are renamed into the new note,
its analysis and tags are carried through the same path re-ingest already uses for its
own pages, and the note it left is repaired — dropped from that `note.json`'s page
placement with the survivors renumbered from 1, which is byte-for-byte what the
donor's own next ingest will write, so the repair converges instead of producing a
second diff later. The donor's `note.json` is rewritten *before* the page's files are
deleted there: the reverse order leaves a crash window in which a note lists a page
whose file is gone, and that is precisely the state `query` and `retrieve` hard-error
on. More than one donor means something outside snorg put a copy there; all of them are
purged (the copy holding a transcription supplies the state) — the note being ingested
is the source of truth for the pages it claims.

The other half is ordering. Whichever note is ingested first, the transcription must
survive, so a page that **leaves** a note is not simply deleted: if it carries
anything that cannot be rebuilt from the `.note` — a `<PAGEID>.md`, a `.md.diff`,
snorg tags or an analysis — that state is parked in `<archive>/orphans/`
(`archive.prunePage`), where the note that later claims the page reclaims it. A page
with none of that is deleted as before, so the store stays empty in ordinary use. The
store is deliberately a note directory without a `note.json`: `List` only reports
directories holding one, so it is invisible to every read surface, and the same
accessors address it by passing `orphans` where a FILE_ID goes — reclaiming a parked
page and adopting one from a live note are one code path. It holds no `.svg` (that is
re-derivable, and its background href only resolves beside a note's `backgrounds/`
folder). Nothing collects it automatically, because an entry is the last copy of text
snorg cannot reproduce; entries vanish when a note claims the page, and the rest are a
few KB of plaintext to delete by hand. `migrate` walks the store explicitly, since
`List` cannot reach it and adoption reads a parked doc through the gated reader.

Two residues are left deliberately. The note a page moved **out** of keeps stale
`<PAGEID>.svg` nav/link hrefs pointing at it until that note is itself re-ingested —
the same designed condition as the dangling cross-note links above — and its
`backgrounds/` PNG may become unreferenced, which nothing has ever collected.

**Incremental update.** Re-ingest does not rebuild the directory (that would discard
expensive per-page LLM analyses). Instead `archive.Write` reconciles: pages dropped
from the note have all their `<PAGEID>.*` files pruned, their reclaimable state parked
first (see Page moves); `note.json`
and per-page files are written only when their bytes change; any other `<PAGEID>.*`
artifacts of pages that remain are left untouched. A page's `analysis` (fields +
source hash) is carried over, and per-title/per-link transcriptions are carried by
**exact rect match** (`archive.carryRegionAnalyses`) — a moved region drops its
transcription and is re-transcribed by the next `analyze` run.

**Schema versioning.** Both JSON docs carry a `schema_version` (first field); every
writer stamps `archive.CurrentSchemaVersion`. `ReadNote`/`ReadPage` **hard-reject**
any file whose version differs (`ErrSchemaVersion`, "run `snorg migrate`"), so no
command ever misreads a grammar it doesn't understand — the whole tool refuses a
stale archive, and re-ingest aborts rather than clobbering a stale page. When the
JSON contract changes we bump the constant and append one step function to
`archive.schemaMigrations` (indexed by source version, so `len == CurrentSchemaVersion`).

The `migrate` command (`archive.MigrateAll`/`MigratePages` in `migrate.go`) is the
**only** reader that walks stale grammars: it reads each file **raw** (no
`verifySchema`) and enumerates the archive by `os.Stat`/glob (`List` +
`archivedPageIDs`), never the gated readers — so it works on exactly the stale
archive it repairs. Each file is walked **one version at a time** (v→v+1→…→current)
through `schemaMigrations` (steps mutate a generic `map[string]any`, `kind`-aware for
note vs page), then re-serialized through the canonical `NoteDoc`/`PageDoc` struct so
the bytes match ingest exactly (`schema_version` first, unknown fields dropped). It is
idempotent (already-current files report `current` and are not rewritten) and refuses
to downgrade a file newer than the binary. Selection mirrors `serve` (PAGEID args /
stdin / bare = whole archive), but a page selection also migrates its owning
`note.json`. Version 0 (absent field) = pre-versioning, migrated to 1 by the first
(no-op) step. The v5→v6 step backfills a page's `modified_at`/`device_modified_at` from
its PAGEID's creation time (no history exists to recover them from).

`migrate` additionally normalizes each page's `<PAGEID>.md.diff`
(`archive.migrateEditDiff`). That sidecar carries no `schema_version`, so there is no
chain to walk: it is **content-sniffed** instead, and a legacy JSON `go-diffpatch`
patch is rewritten as a unified diff via `textmerge.ConvertLegacyDiff`. This runs
independently of the JSON version walk — idempotent, so it self-heals a crash between
the two writes — and is reported as its own `diff <PAGEID>: migrated` result. That is
why one page can yield two results, and hence why `migrate` draws a counter rather
than a bar.

## Packages

- `cmd/snorg` — CLI entry: one `urfave/cli/v3` command tree, thin actions over the
  public `pkg/snorg` package (not the internal packages directly). The root `Before`
  hook builds one `snorg.Client` (via `snorg.Resolve`) shared by every command; the
  actions parse flags, source PAGEIDs from args or stdin, and format results. What
  stays CLI-only: flag parsing, the PAGEID stdin conventions, and result formatting
  (`printQueryLong`, JSON, exit codes) plus two files of reporting: `progress.go` (the
  stderr reporter — three modes, a fixed-width bar or a counter, TTY-detected with the
  same `os.Stat`/`ModeCharDevice` idiom as `stdinPiped`, zero new dependencies) and
  `describe.go` (pure functions turning the change vocabulary into the printed lines,
  so an analyzed page, an `analyze-edit` save and an ingested note read alike).
- `pkg/snorg` — the **public Go API** (import `github.com/jdlugosz963/snorg/pkg/snorg`).
  A `Client` (built by `Open` — explicit root + optional config — or `Resolve` — the
  CLI's config layering) bundles an archive with merged config and exposes every
  capability as a method: `List`/`Query`/`Retrieve`/`Export`/`ServeHandler` (read),
  `RenderRegion` (a page rect → PNG bytes, canonical or styled), `Ingest`/`Migrate`
  (write), `NewProvider`/`Analyze` (LLM — the caller builds the
  provider once and passes it in), `PageBuffer`/`ApplyPage`
  (programmatic, no-`$EDITOR` edit) and `EditPage` (the interactive convenience the
  CLI uses). It imports the internal packages and re-exports, as type **aliases**,
  every type its signatures return (`Result`, `NoteView`, `Match`, `Spec`, config
  types, …) so an external consumer that imports only this package can name and
  traverse them — the sole seam that lets snorg be used as a library. See
  [library.md](library.md) for the guide; the per-identifier reference is rendered
  on demand (`gomarkdoc ./pkg/snorg`, or pkg.go.dev), not checked in.
- `internal/snote` — device-agnostic domain model (`Note`/`Page`/`Title`/`Keyword`/`Link`)
  and the `Source` interface (the format seam); the concrete impl sits behind it.
- `internal/snote/sntool` — the `Source` impl: the native-Go
  `github.com/jdlugosz963/sntool` library (`sntool.Open` + `render.SVG`), pure Go, no
  external tool. sntool resolves footer-key page association itself, so this adapter just
  maps its domain model onto snorg's and renders each page to SVG in-process.
- `internal/archive` — owns the on-disk layout; `doc.go` is the JSON serialization boundary
  (per-title/per-link `analysis` nested on the items; page-level `analysis` holds
  `source_hash` + `fields` + `regions[]` (per-box fingerprint state); the top-level
  `background_hash` (ingest-stamped selector) + `analysis.regions[]` support template regions;
  the top-level `tags` are the snorg-managed labels, note-scoped in `note.json` and
  page-scoped in `<PAGEID>.json`; the page's change stamps `modified_at` (owned by
  `WritePage`, the single page-doc writer: bumped when the doc's content differs from the
  stored one or a sibling svg/md changed in the same operation, which is why callers write
  those first) and `device_modified_at` (owned by `Write`: bumped when `device_hash` —
  sha256 of the device-derived doc + the raw pre-pipeline SVG — moves, so restyling never
  counts; an empty stored hash after `migrate` is recorded as a baseline, not a change), see
  `stamp.go`; both
  docs carry `schema_version` = `CurrentSchemaVersion`);
  `Write` reconciles a note's directory in place, stamps `background_hash` (sha256 of the
  decoded background, from `background.go`) and runs the
  SVG pipeline (background mode → `recolor` → `injectNav` → `injectLinks` → `formatSVG`,
  each configurable); `read.go` are the layout-aware accessors (`List`/`ReadNote`/`ReadPage`/
  `ReadSVG`/`SVGRel`/`FindPage` — `ReadNote`/`ReadPage` gate on `schema_version`, erroring
  `ErrSchemaVersion` on a mismatch) plus `WritePage` and the sidecar readers; `editdiff.go`
  owns the `.md.diff` sidecar and its invariant (`ReadAnalysisBase`/`WriteAnalysisEdit`/
  `MergeAnalysis`), which serves both a normal page's content and a templated page's region
  sections — they share the one `<PAGEID>.md[.diff]` pair, since a page is one or the other;
  `templates.go` builds the template set from specs injected via `SetTemplateSpecs`
  (`TemplateSpec`/`Template`/`Box`, `MatchBackground` by image hash — the specs come from the
  merged config's `templates:` section, bridged in by `pkg/snorg.Open`) and `regions.go`
  (de)serializes the id-keyed region sections held in `.md` (see `docs/templates.md`);
  `tags.go` owns the snorg-managed labels and the single inheritance rule every read
  surface applies (`EffectiveTags` = the sorted union of a note's tags and a page's own);
  `change.go` is the **shared change vocabulary** both write paths speak
  (`TextKind`/`TextChange` + the total `TextChangeOf`, `NameChange`, `RegionChange` —
  orthogonal facts, sparse, a zero meaning "did not happen");
  `migrate.go` is the un-gated schema upgrader (`MigrateAll`/`MigratePages` + the version-indexed
  `schemaMigrations` chain, plus `migrateEditDiff` for the unversioned `.md.diff` sidecar).
- `internal/retrieve` — platform-agnostic read contract: assembles `note.json` + each
  `<PAGEID>.json` + `<PAGEID>.md` into denormalized `NoteView`s (the stable JSON consumers
  depend on). `Get` takes PAGEIDs and returns a `*Result` — `Archive`, the **absolute**
  archive root (`filepath.Abs`) the pages' relative svg paths resolve against, so the
  payload is self-contained, plus `Notes`, the views grouped per owning note (archive
  List order, pages in placement order); an unknown PAGEID is an error. A templated page's `.md` is the region-section
  document: its transcription surfaces as `analysis.regions[]` (`RegionView{id,label,rect,
  content}`, label/rect resolved from the template config) with an empty `analysis.content`.
- `internal/query` — the read-only walk: `Pages(a, match)` visits every note/page via the
  `archive` accessors and returns the ones a `func(NoteDoc, PageDoc) bool` accepts. It is only
  the walk — the filter vocabulary itself (the `Match*` predicates and the compiler for the
  expression language) lives in `pkg/snorg`, where the `Client` a predicate needs is in scope;
  the language's *syntax* is one layer further down, in `internal/querylang`; `Client.Query` is the one
  adapter, wrapping each candidate as a `snorg.Page{Client, Note, Doc}`. The match function gets
  the whole `NoteDoc` alongside the `PageDoc`, so note-level state is in scope without a second
  read (`MatchTag` uses it for tags inherited from the note). The same walk backs the inventory
  aggregators `Keywords(a)`/`Tags(a)` (distinct labels + per-value page counts, behind
  `list keywords`/`list tags`; tag counts are per page over the effective set).
- `internal/querylang` — the query language's **syntax**: `Parse(string)` turns an expression
  into an AST (`Expr`/`AndExpr`/`Unary`/`Term{Field, Scope, Op, Value, Pos}`) and nothing more.
  It is the only package that knows `participle`, and it knows no field names — an unknown term,
  a value on a term that takes none, a bad regexp or a bad date spec all parse here and are
  rejected by `pkg/snorg`'s compiler, which has the `Term.Pos` to point at. The lexer is
  **stateful**: a value is lexed by different rules than an expression (so
  `ctime:2026-04-04..2026-09-12` is one token), and the value state has no whitespace rule, which
  is what makes `content: x` an error rather than a quietly different query. Quote handling
  (`"…"` with `\"`/`\\` escapes, `'…'` verbatim) is done at capture, so a `Term.Value` is
  already the literal string the user meant. External dep: `participle/v2`.
- `internal/config` — loads + deep-merges YAML config (provider creds, analysis prompts,
  `ingest.svg` toggles, `export.template`, `templates:` regions); `Load` expands each file's
  `include:` list (other configs merged *over* the includer — "above includer" — recursively,
  with cycle detection) and resolves each `templates[].image` to an absolute path relative to
  the declaring file, then parses + defaults — commands validate the section they use. The
  `templates:` specs are bridged to the archive by `pkg/snorg.Open` (`config.TemplateSpec` →
  `archive.TemplateSpec`), keeping config and archive mutually independent. External deps:
  `yaml.v3`, `internal/snote` (`snote.Rect` for template boxes).
- `internal/analyze` — incremental vision-LLM analysis of one page (by PAGEID): a single
  **canonical black-on-white rasterization** (`geom.go`: `canonicalSVG` forces every `<path>`
  black and drops background/nav/link overlays, `oksvg`/`rasterx`) drives everything —
  its 1-bit ink `mask` gives `analysis.source_hash` (`mask.hash`) for the skip check, the
  per-box fingerprints, and the LLM crops, so a `Page` call rasterizes exactly once
  (including on a skip). Unchanged pages are skipped, and a page with **zero ink**
  (`mask.blank`) transcribes to empty with no LLM call at all — no content call and no custom
  fields, since there is nothing to read; otherwise it transcribes the page
  (through the update prompt + the previous AI base when one exists — never the user-edited
  text), 3-way merges the result with any user edits (`archive.MergeAnalysis`, which returns the
  `TextChange` — overlap sets its `Conflicts`), crops title/link rects, runs the custom fields
  over the effective content, and writes `<PAGEID>.md` + `<PAGEID>.json`. It reports a
  `PageResult`: the page's `Content`/`Regions` change plus the facts only analysis can produce —
  a fingerprint `Skipped`, a `Blank` box, the `Calls` the page cost. The mask depends only on the
  handwriting geometry, so it is invariant under recolor/background/overlays and restyling
  never re-triggers analysis. **Template regions**: a page whose `background_hash` matches
  `a.Templates()` is analyzed per box instead — `mask.regionHash` hashes the box rect cropped
  out of the page mask (pixel-space, so a stroke crossing the edge counts only its in-box
  pixels), so an unchanged box is skipped and a moved/added box rect re-triggers (the
  page-level skip is template-aware); a box with no ink (`mask.blankRegion`, the same clamped
  crop) is transcribed as empty without a call, so a partly-filled form costs one call per box
  actually written in; the id-keyed region document is 3-way merged into the
  page's `<PAGEID>.md` (in place of free-form content). `render.go` also exports
  `RenderRegion(svg, rect, canonical)` — rasterize + crop a page rect to PNG bytes, canonical
  (what the model sees) or the styled SVG as-is — the package's image entry point outside the
  analysis flow, exposed as `pkg/snorg.Client.RenderRegion`. External deps: `openai-go`,
  `oksvg`/`rasterx`.
- `internal/edit` — the `analyze-edit` command's orchestration: opens the page's
  transcription in the user's editor (`sh -c`, terminal inherited, temp copy so an
  aborted editor changes nothing) and stores the effective md via `archive.WriteAnalysisEdit`;
  a templated page's body is `<!-- region <id> -->` sections (keyed by id, no content marker)
  assembled into that same md. Returns a `PageEdit` — the content `TextChange`, the per-section
  `RegionChange`s and the `NameChange`s — in the vocabulary `internal/archive/change.go` defines.
- `internal/textmerge` — diff/patch and 3-way-merge plumbing, pure Go (no PATH tool):
  `Diff`/`Unapply` (line diff + reverse-apply of a patch) via
  `github.com/njchilds90/go-diffpatch`, serialized to and from a **normal unified diff**
  (`patchToFileDiff`/`fileDiffToPatch` via `github.com/sourcegraph/go-diff`), so the
  stored `.md.diff` is a readable `--- /+++ /@@ /+/-` diff rather than a struct dump —
  `ConvertLegacyDiff` upgrades a pre-unified JSON-patch sidecar to that form for
  `migrate`; `Stat` (added/removed line counts off the same
  patch — lazy, called only when something is displayed), `HasConflicts` (marker sniff
  for one slice of an already-merged document), `Merge` (3-way merge, git-standard
  conflict markers) via `github.com/epiclabs-io/diff3`; pure text-in/text-out.
- `internal/export` — renders the retrieved `*retrieve.Result` through one pongo2
  template in a single pass (`Render(res, template)`, `export` cmd): the whole Result →
  JSON → back into the context map as-is (`archive` + `notes`), so templates bind to the
  `retrieve` shape verbatim, can span notes and can resolve svg paths against
  `{{ archive }}`; output to stdout. Filters, one file per concern: `denote.go`
  (FILE_ID/PAGEID → denote id), `orgmode.go` (org-mode-only: `org` via pandoc
  shell-out, `nestorgheadings:N`), `html.go` (HTML-only: `html` via pandoc shell-out,
  marked safe), `markdown.go` (Markdown-only: `nestmdheadings:N`).
  External dep: `pongo2/v6`; PATH tool: `pandoc` (the `org` and `html` filters).
- `internal/serve` — the built-in HTTP viewer (`serve` cmd): `Handler(a, views, flat)` builds
  a `net/http.ServeMux` over the assembled `[]*retrieve.NoteView` — `/` (note gallery, or one
  flat page gallery under `--flat`), `/note/{fid}` (page gallery + `<object>` lightbox with the
  page transcription), `/thumb/{fid}/{name}` (small rasterized-PNG thumbnail via
  `thumb.go`/`oksvg`/`rasterx`, memoized in-memory) and `/svg/{fid}/{name}` (full page SVG;
  `svglinks.go` retargets baked links to viewer routes and makes the root responsive, memoized).
  The grouped/flat difference is a `layout` seam (`groupedLayout`/`flatLayout` implement
  `render` + `pageHref`) chosen once, so routes are branch-free and both galleries share one
  `pagegrid` template over a `pageItem` tile. Both image routes carry `ETag`/`Cache-Control`
  (`writeAsset`) and serve only pages in the served set. Self-contained HTML/CSS/JS via
  `html/template`. Read-only. Deps: `oksvg`/`rasterx`.
- `internal/ingest` — orchestrator: `Source.Read` → render SVGs → `Archive.Write`.
- `examples/emacs/snorg.el` — Emacs org consumer (outside the Go tree): drives the CLI
  (`list`/`query`/`retrieve`/`export`) to import notes into a pluggable backend (denote or
  org-roam; the backend owns the FILE_ID→note-id translation), adds the `snorg:` (page SVG)
  and backend-agnostic `snorg-note:` (page-jump) org links — both keyed by a bare PAGEID,
  whose owning note `retrieve` resolves — and a dual-window review mode.
- `examples/web/` — static HTML site consumer (outside the Go tree): `export.sh` reads
  PAGEIDs on stdin and runs `snorg export` over that set twice — `index.yaml` for the
  note index, `note.yaml` once per note — then copies the selected pages' SVGs beside
  the generated HTML. Uses the `html` filter, so it needs `pandoc`.

## Extension points

New retrieval/export commands are thin actions in `cmd/snorg` over a `pkg/snorg`
capability, projecting into a `retrieve` view. Analysis enriches the `Doc`
JSON schemas (and the views) with new fields (free to add — no backcompat). A native
parser is a new `snote.Source` implementation. New link kinds extend
`archive.linkAnchor`; new export filters register in their own file in
`internal/export` (grouped by target format, like `orgmode.go`).
