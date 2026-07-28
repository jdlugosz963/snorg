# Architecture

## CLI

```
snorg -a <archive-path> [-c config.yaml ...] [--no-archive-config] <command> [command flags] [args]

snorg -a <archive-path> ingest [-j N] <file-or-dir>
snorg -a <archive-path> list
snorg -a <archive-path> query <filter> [arg]
snorg -a <archive-path> retrieve [PAGEID ...]
snorg -a <archive-path> analyze [--force] [PAGEID ...]
snorg -a <archive-path> analyze-edit <PAGEID>
snorg -a <archive-path> export [PAGEID ...]
snorg -a <archive-path> serve [-l ADDR] [PAGEID ...]
```

The archive path is a required global flag (`-a`/`--archive`, never hardcoded) and
comes before the command, together with the global config flags: the root's `Before`
hook loads the merged config once (see [config.md](config.md)) and hands it to the
command, which picks and validates only the sections it uses. The CLI is built on
`urfave/cli/v3`. `ingest` takes
a single `.note` or a directory (walked recursively for `*.note`) and ingests
notes through a worker pool: `-j N` caps concurrent notes, default
`runtime.NumCPU()` (work is CPU-bound supernote-tool rendering); `-c` config
controls the SVG pipeline (see below). A failed note never aborts the batch — all
are attempted and failures summarized (non-zero exit). Re-ingest reconciles the
note's directory in place (see Archive layout); it is the update path.

`list`, `query` and `retrieve` are the read side — the platform-agnostic interface
external tools build on (see [retrieval.md](retrieval.md)). `query` takes one
filter per call — `all`, `note <FILE_ID>`, `unanalyzed`, `keyword <regexp>`
(matched against `Keyword.Text`), `starred`, `date <spec>`, and a `not <filter>`
prefix that inverts any of them — and prints the PAGEID of each
matching page, one per line. `retrieve`, `analyze` and `export` all take PAGEIDs
as arguments, or read them one-per-line from stdin when none are given, so
`query` pipes into any of them.

`retrieve` prints the selected pages assembled into a JSON array of `NoteView`s,
grouped per owning note (full note metadata, only the requested pages); a whole
note is `query note <FILE_ID> | retrieve`. `export` groups the same way and
renders the config's template once over the whole array (context key `notes`),
so one template invocation sees every selected note.

`analyze` processes its pages sequentially (LLM rate limits; a failed page
never aborts the batch). Unchanged pages are skipped without an LLM call (see
[config.md](config.md), "Incremental analysis"), so the canonical batch run is:

```sh
snorg -a <archive> query all | snorg -c cfg.yaml -a <archive> analyze
```

`analyze-edit` opens the page's transcription in `$VISUAL`/`$EDITOR` (exactly one
PAGEID, no stdin — the editor needs the terminal) and needs no provider config:
a page can be transcribed entirely by hand, without any LLM involved. Manual
edits survive re-analysis (see "User edits" below); `analyze` reports `conflict`
where an edit and the new transcription overlap, resolved by another
`analyze-edit`.

`serve` is the built-in, zero-setup viewer: it assembles the selected pages
(`retrieve.Get`) and stands up a local HTTP site (`-l`/`--listen`, default
`127.0.0.1:8080`) — a gallery of notes (name + first-page thumbnail), each opening
a gallery of that note's pages with a click-to-enlarge lightbox that also shows the
page's transcription under the enlarged page (←/→ pages, Esc). The enlarged page is an
`<object>` (a live SVG document) so its baked links stay clickable; the `/svg` route
retargets them to `/note/{fid}?page={pid}` (with `target="_top"`) so a tap opens that
note and enlarges the page, and makes the SVG root responsive so it scales to fit
instead of clipping. Thumbnails are small rasterized PNGs (the SVG rendered at ~400px,
so far fewer bytes than the vector) and both the thumbnail and full-SVG routes carry
`ETag`/`Cache-Control` for browser caching. No PAGEIDs and no pipe means the whole archive. Everything is in-memory: the
views are computed once, thumbnails are memoized, and the page SVGs are streamed
straight from the archive, nothing copied to disk. Needs no provider config.

## Archive layout (plaintext contract)

```
<archive>/<FILE_ID>/
    note.json          # file metadata + ordered page placement (id, number)
    <PAGEID>.json      # per page: starred, titles(rect,level,analysis), keywords(text),
                       # links(...,analysis), analysis{source_hash,fields}
    <PAGEID>.md        # per page: the content transcription (Markdown), AI-produced
                       # and/or user-edited — always the effective content
    <PAGEID>.md.diff   # only while user edits diverge from the AI transcription:
                       # unified diff AI-base → md (analyze-edit)
    <PAGEID>.svg       # per page rendered vector (one <path>/command per line)
    backgrounds/
        <sha256>.png   # page backgrounds, content-addressed, deduped per note
```
Stable filenames + indented JSON → clean VCS diffs. The page transcription lives
in the `<PAGEID>.md` sidecar — multiline Markdown diffs like prose, not like a
JSON string. SVGs are reflowed so each `<path>` and each `d` command sits on its
own line (`archive.formatSVG`), so a changed stroke touches only its own lines
instead of one giant line. supernote-tool embeds the same template background
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
pixel rect, so the region navigates without altering the note's appearance. The href is
**relative to the page SVG** — `<PAGEID>.svg` for a same-note jump, `../<FILE_ID>/<PAGEID>.svg`
for another note. A same-note jump resolves only to a page written in this `Write`;
a cross-note jump is baked **unconditionally** to its deterministic archive-relative path,
so it works regardless of ingest order — the href simply dangles until the target note is
ingested. Resolution is a small ordered pipeline (`archive.linkHref`) so other link kinds
(e.g. web links) can be added later without touching callers; today only note-page targets resolve.

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
the analyze fingerprint (path geometry only). See [config.md](config.md).

**User edits.** `analyze-edit` opens the transcription in the user's editor.
`<PAGEID>.md` always holds the *effective* content — what `retrieve`/`export`
show — while `<PAGEID>.md.diff` records how it diverges from the last
AI-produced transcription (the *base*, reconstructed by reverse-applying the
diff; the file exists iff they diverge). On re-analysis the LLM is prompted
with the base — **user edits never reach the LLM** — and the fresh output is
3-way merged (`git merge-file`: base, user's md, new transcription); the md
becomes the merge result and the diff is rebased onto the new base. Overlaps
leave standard conflict markers (`<<<<<<< edited` / `>>>>>>> reanalyzed`) in
the md and the `conflict` outcome; resolving is another `analyze-edit`. A page
never analyzed by AI has an empty base, so a hand-written transcription meets
its first AI run as one conflict to resolve once. git is a PATH tool here
(like supernote-tool and pandoc), isolated in `internal/textmerge`.

**Incremental update.** Re-ingest does not rebuild the directory (that would discard
expensive per-page LLM analyses). Instead `archive.Write` reconciles: pages dropped
from the note have all their `<PAGEID>.*` files pruned (`.md` included); `note.json`
and per-page files are written only when their bytes change; any other `<PAGEID>.*`
artifacts of pages that remain are left untouched. A page's `analysis` (fields +
source hash) is carried over, and per-title/per-link transcriptions are carried by
**exact rect match** (`archive.carryRegionAnalyses`) — a moved region drops its
transcription and is re-transcribed by the next `analyze` run.

## Packages

- `cmd/snorg` — CLI entry: one `urfave/cli/v3` command tree, thin actions over the
  internal packages. The root action loads the merged
  config once into an `app` shared by every command and dispatches by hand
  (the archive path precedes the command name, which the library's own
  subcommand matching cannot express).
- `internal/snote` — device-agnostic domain model (`Note`/`Page`/`Title`/`Keyword`/`Link`)
  and the `Source` interface (the format seam).
- `internal/snote/sntool` — `Source` impl shelling out to `supernote-tool`
  (`analyze` + `convert -t svg`). `footer.go` parses page association from footer keys.
  Replaceable by a native-Go parser without touching callers.
- `internal/archive` — owns the on-disk layout; `doc.go` is the JSON serialization boundary
  (per-title/per-link `analysis` nested on the items; page-level `analysis` holds
  `source_hash` + `fields`); `Write` reconciles a note's directory in place and runs the
  SVG pipeline (background mode → `recolor` → `injectNav` → `injectLinks` → `formatSVG`,
  each configurable); `read.go` are the layout-aware accessors (`List`/`ReadNote`/`ReadPage`/
  `ReadSVG`/`SVGRel`/`FindPage`) plus `WritePage` and the `<PAGEID>.md` sidecar pair
  `ReadAnalysisMD`/`WriteAnalysisMD`; `editdiff.go` owns the `<PAGEID>.md.diff`
  sidecar and its invariant (`ReadAnalysisBase`/`WriteAnalysisEdit`/`MergeAnalysis`).
- `internal/retrieve` — platform-agnostic read contract: assembles `note.json` + each
  `<PAGEID>.json` + `<PAGEID>.md` into denormalized `NoteView`s (the stable JSON
  consumers depend on). `Get` takes PAGEIDs and groups them per owning note (archive
  List order, pages in placement order); an unknown PAGEID is an error.
- `internal/query` — read-only metadata filter: walks every note/page via the `archive`
  accessors and returns the pages matching a `Predicate` (`All`, `Starred`, `Unanalyzed`,
  `InNote(fileID)`, `Keyword(regexp)`).
- `internal/config` — loads + deep-merges YAML config (provider creds, analysis prompts,
  `ingest.svg` toggles, `export.template`); `Load` parses + defaults only — commands
  validate the section they use. External dep: `yaml.v3`.
- `internal/analyze` — incremental vision-LLM analysis of one page (by PAGEID): fingerprints
  the page by its path geometry (`pathHash`, `analysis.source_hash`) and skips unchanged
  pages without even rasterizing; otherwise rasterizes the SVG (`oksvg`/`rasterx`),
  transcribes the page (through the update prompt + the previous AI base when one exists —
  never the user-edited text), 3-way merges the result with any user edits
  (`archive.MergeAnalysis`; overlap → the `conflict` outcome), crops title/link rects, runs
  the custom fields over the effective content, and writes `<PAGEID>.md` + `<PAGEID>.json`.
  The geometry hash is invariant under recolor/background/overlays, so restyling never
  re-triggers analysis. External deps: `openai-go`, `oksvg`/`rasterx`.
- `internal/edit` — the `analyze-edit` command's orchestration: opens the page's
  transcription in the user's editor (`sh -c`, terminal inherited, temp copy so an
  aborted editor changes nothing) and stores the result via `archive.WriteAnalysisEdit`.
- `internal/textmerge` — unified-diff/3-way-merge plumbing shelling out to `git`
  (`Diff`/`Unapply`/`Merge`); pure text-in/text-out, the only place that executes git.
  PATH tool: `git`.
- `internal/export` — renders the retrieved `[]*retrieve.NoteView` through one pongo2
  template in a single pass (`export` cmd): views → JSON → context under the `notes`
  key, so templates bind to the `retrieve` json array verbatim and can span notes;
  output to stdout. Filters, one file per concern: `denote.go`
  (FILE_ID/PAGEID → denote id), `orgmode.go` (org-mode-only: `org` via pandoc
  shell-out, `nestorgheadings:N`), `markdown.go` (Markdown-only: `nestmdheadings:N`).
  External dep: `pongo2/v6`; PATH tool: `pandoc` (only for the `org` filter).
- `internal/serve` — the built-in HTTP viewer (`serve` cmd): `Handler(a, views)` builds
  a `net/http.ServeMux` over the assembled `[]*retrieve.NoteView` — `/` (note gallery),
  `/note/{fid}` (page gallery + `<object>` lightbox with the page transcription),
  `/thumb/{fid}/{name}` (small rasterized-PNG thumbnail via `thumb.go`/`oksvg`/`rasterx`,
  memoized in-memory) and `/svg/{fid}/{name}` (full page SVG; `svglinks.go` retargets baked links
  to viewer routes and makes the root responsive, memoized). Both image routes carry
  `ETag`/`Cache-Control` (`writeAsset`) and serve only pages in the served set. Self-contained
  HTML/CSS/JS via `html/template`. Read-only. Deps: `oksvg`/`rasterx`.
- `internal/ingest` — orchestrator: `Source.Read` → render SVGs → `Archive.Write`.
- `examples/emacs/snorg.el` — Emacs org consumer (outside the Go tree): drives the CLI
  (`list`/`query`/`retrieve`/`export`) to import notes into a pluggable backend (denote or
  org-roam; the backend owns the FILE_ID→note-id translation), adds the `snorg:` (page SVG)
  and backend-agnostic `snorg-note:` (page-jump) org links, and a dual-window review mode.

## Extension points

New retrieval/export commands add cases in `cmd/snorg` and read via `archive`
accessors, projecting into a `retrieve` view. Analysis enriches the `Doc`
JSON schemas (and the views) with new fields (free to add — no backcompat). A native
parser is a new `snote.Source` implementation. New link kinds extend
`archive.linkHref`; new export filters register in their own file in
`internal/export` (grouped by target format, like `orgmode.go`).
