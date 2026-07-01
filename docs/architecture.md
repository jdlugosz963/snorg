# Architecture

## CLI

```
snorg ingest   [-j N] <file-or-dir> <archive-path>
snorg list     <archive-path>
snorg retrieve <archive-path> <FILE_ID>
snorg query    <archive-path> <filter> [arg]
snorg export   [-c config.yaml ...] <archive-path> <FILE_ID>
```
Archive path is a positional argument (never hardcoded). `ingest` takes a single
`.note` or a directory (walked recursively for `*.note`) and ingests notes through
a worker pool: `-j N` caps concurrent notes, default `runtime.NumCPU()` (work is
CPU-bound supernote-tool rendering). A failed note never aborts the batch — all are
attempted and failures summarized (non-zero exit). Re-ingest reconciles the
note's directory in place (see Archive layout); it is the update path. `list`,
`retrieve` and `query` are the read side — the platform-agnostic interface external
tools build on (see [retrieval.md](retrieval.md)). `query` takes one filter per call
(`keyword <regexp>` matched against `Keyword.Text`, or `starred`) and prints the
PAGEID of each matching page, one per line.

## Archive layout (plaintext contract)

```
<archive>/<FILE_ID>/
    note.json          # file metadata + ordered page placement (id, number)
    <PAGEID>.json      # per page: starred, titles(rect,level), keywords(text), links
    <PAGEID>.svg       # per page rendered vector (one <path>/command per line)
    backgrounds/
        <sha256>.png   # page backgrounds, content-addressed, deduped per note
```
Stable filenames + indented JSON → clean VCS diffs. SVGs are reflowed so each
`<path>` and each `d` command sits on its own line (`archive.formatSVG`), so a
changed stroke touches only its own lines instead of one giant line. supernote-tool
embeds the same template background inline in every page; `archive.extractBackground`
lifts it into the note's `backgrounds/` subfolder and the SVG references it by
descendant href (`backgrounds/<sha256>.png`), so the giant base64 line is gone and
one PNG is stored per note instead of once per page. The href must stay a descendant
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
for another note — and is only emitted when the target page's SVG exists in the archive
(a same-note target is always written this `Write`; a cross-note target must already be
ingested, otherwise the link is skipped until a later re-ingest). Resolution is a small
ordered pipeline (`archive.linkHref`) so other link kinds (e.g. web links) can be added
later without touching callers; today only note-page targets resolve.

**Incremental update.** Re-ingest does not rebuild the directory (that would discard
expensive per-page LLM analyses). Instead `archive.Write` reconciles: pages dropped
from the note have all their `<PAGEID>.*` files pruned; `note.json` and per-page files
are written only when their bytes change; any other `<PAGEID>.*` artifacts of pages that
remain are left untouched.

## Packages

- `cmd/snorg` — CLI entry + subcommand dispatch (stdlib, no framework).
- `internal/snote` — device-agnostic domain model (`Note`/`Page`/`Title`/`Keyword`/`Link`)
  and the `Source` interface (the format seam).
- `internal/snote/sntool` — `Source` impl shelling out to `supernote-tool`
  (`analyze` + `convert -t svg`). `footer.go` parses page association from footer keys.
  Replaceable by a native-Go parser without touching callers.
- `internal/archive` — owns the on-disk layout; `doc.go` is the JSON serialization boundary
  (`PageDoc.Analysis` carries derived AI output); `Write` reconciles a note's directory in place
  (preserving each page's `analysis`) and bakes each page's links into the SVG as relative
  `<a>` hyperlinks (`injectLinks`), `read.go` are the layout-aware accessors
  (`List`/`ReadNote`/`ReadPage`/`ReadSVG`/`SVGRel`/`FindPage`) plus `WritePage`.
- `internal/retrieve` — platform-agnostic read contract: assembles `note.json` + each
  `<PAGEID>.json` into one denormalized `NoteView` (the stable JSON consumers depend on).
- `internal/query` — read-only metadata filter: walks every note/page via the `archive`
  accessors and returns the pages matching a `Predicate` (`Starred`, `Keyword(regexp)`).
- `internal/config` — loads + deep-merges YAML config (provider creds, analysis prompts,
  `export.template`); `Load` parses + defaults only — commands validate the section they use.
  External dep: `yaml.v3`.
- `internal/analyze` — vision-LLM analysis of one page (by PAGEID): rasterizes the page SVG
  (`oksvg`/`rasterx`), crops title/link rects, transcribes them and the page via a `Transcriber`
  (openai-go; endpoint/model/key from config), and writes the result into `<PAGEID>.json` under
  `analysis`. External deps: `openai-go`, `oksvg`/`rasterx`.
- `internal/export` — renders a `retrieve.NoteView` through one pongo2 template (`export` cmd):
  view → JSON → `map[string]any` context, so templates bind to the `retrieve` json keys; output
  to stdout. External dep: `pongo2/v6`.
- `internal/ingest` — orchestrator: `Source.Read` → render SVGs → `Archive.Write`.

## Extension points

New retrieval/export commands add cases in `cmd/snorg` and read via `archive`
accessors, projecting into a `retrieve` view. Vision-LLM analysis enriches the `Doc`
JSON schemas (and the views) with new fields (free to add — no backcompat). A native
parser is a new `snote.Source` implementation.
