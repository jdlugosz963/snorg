# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

SNORG (supernote-organizer) ingests Supernote `.note` files into a plaintext,
machine-readable, VCS-friendly *archive*, so notes can later be retrieved and exported.
Read `docs/principles.md` (project rules), `docs/architecture.md` (modules/CLI),
`docs/supernote-format.md` (`.note` format + tooling) before substantial work.

## Project rules

- Go. External deps by package: `internal/analyze` uses `openai-go` (LLM client)
  and `oksvg`/`rasterx` (SVG rasterization); `internal/config` uses
  `gopkg.in/yaml.v3` (config parsing); `internal/export` uses `pongo2/v6`
  (Jinja2-style templating); `cmd/snorg` uses `urfave/cli/v3` (CLI framework).
  PATH tools (not go.mod deps): the `.note` binary format is handled by shelling
  out to `supernote-tool` (supernotelib) — not parsed natively (yet) — and the
  export `org` filter shells out to `pandoc`.
- No backward compatibility: when something must change, rewrite it cleanly rather
  than preserving legacy.
- All operational data is plaintext; all docs in English, maximally concise.

## Commands

- `go build ./... && go vet ./... && gofmt -l .` — build, vet, format check (gofmt output must be empty)
- `go test ./...` — all tests
- `go test ./internal/snote/sntool` — fast unit tests (footer-key parsing)
- `go test -run TestIngestSampleNote ./internal/ingest` — e2e ingest; **slow (~1 min**, renders SVGs via supernote-tool) and skips if `supernote-tool` is not on PATH
- CLI shape: `snorg -a <archive-path> [-c config.yaml ...] [--no-archive-config] <command> [command flags] [args]` — the archive is a **required global flag** (`-a`/`--archive`) and the config flags are global too, so all come **before** the command; the root's `Before` hook loads the merged config once (`<archive-path>/config.yaml` first if present, then `-c` files, later wins) and hands it to the command, which validates only the sections it uses
- `go run ./cmd/snorg -a <archive-path> [-c ...] ingest [-j N] <file-or-dir>` — register a note (or all `*.note` under a dir) into the archive; `-j` caps concurrent notes (default NumCPU); config drives the SVG pipeline (`ingest.svg.{links,navigation,format}` bools default true; `background` mode `extract`(default)`|inline|blank|remove`; `colors` remaps the 4 default pen-shade fills). None of these change the analyze fingerprint (path geometry), so restyling never re-transcribes.
- `go run ./cmd/snorg -a <archive-path> list` — list FILE_IDs (one per line)
- `go run ./cmd/snorg -a <archive-path> retrieve <FILE_ID>` — assembled note as JSON (the read interface)
- `go run ./cmd/snorg -a <archive-path> query <filter> [arg]` — print PAGEIDs of matching pages (one per line); one filter per call: `all`, `note <FILE_ID>`, `unanalyzed`, `keyword <regexp>` (matches `Keyword.Text`), `starred`
- `go run ./cmd/snorg -a <archive-path> [-c ...] analyze [--force] [PAGEID ...]` — incremental vision-LLM analysis; PAGEIDs from args or stdin (pipe from `query`); unchanged pages (path-geometry hash == `analysis.source_hash`; invariant under recolor/background/overlays) are skipped without an LLM call (no rasterize), changed ones re-transcribed through the update prompt + previous `<PAGEID>.md` (minimal diff); writes `<PAGEID>.md` (content) + `<PAGEID>.json` (per-title/link `analysis.name`, `analysis.{source_hash,fields}`); provider + prompts from the config (`docs/config.md`); `api_key` falls back to `api_key_command` stdout then `OPENAI_API_KEY`
- `go run ./cmd/snorg -a <archive-path> [-c ...] export <FILE_ID>` — render the retrieved note JSON through the config's single `export.template` (pongo2/Jinja2) to stdout; template context is the `retrieve` JSON verbatim (snake_case keys: `pages[].titles/keywords/links/analysis.content`, `title.analysis.name`, `link.analysis.name`); filters: `denote` (FILE_ID/PAGEID → denote id), `org` (markdown→org via pandoc), `nestorgheadings:N` (demote org headings), `nestmdheadings:N` (demote Markdown headings); needs no `provider` creds; see `examples/config.yaml` (Markdown) and `examples/emacs/orgmode.yaml` (org)

## Architecture

Flow: `cmd/snorg` → `internal/ingest` orchestrates `snote.Source.Read` → render SVGs → `archive.Write`.

- `internal/snote` — device-agnostic domain model (`Note`/`Page`/`Title`/`Keyword`/`Link`)
  and the `Source` interface, the **seam isolating the format**. A native-Go parser would
  be a new `Source` impl, leaving callers untouched.
- `internal/snote/sntool` — `Source` impl shelling to `supernote-tool` (`analyze` + `convert -t svg`);
  `footer.go` is the risky part (page association, see gotchas).
- `internal/archive` — owns the on-disk layout `<archive>/<FILE_ID>/{note.json,<PAGEID>.json,<PAGEID>.md,<PAGEID>.svg}`;
  `doc.go` is the JSON serialization boundary (the stable plaintext contract; add fields freely —
  per-title/per-link `analysis` is nested on the items, page `analysis` holds `source_hash`+`fields`,
  the content transcription lives in the `<PAGEID>.md` sidecar); `Write` runs the config-driven
  SVG pipeline (background mode `extract`/`inline`/`blank`/`remove` → `recolor` pen-shade fills →
  `injectNav` prev/next half-page zones → `injectLinks` note links → `formatSVG`; nav is emitted
  before links so links win hit-testing — SVG picks the later element);
  `read.go` are layout-aware accessors (`List`/`ReadNote`/`ReadPage`/`ReadSVG`/`SVGRel`/`FindPage`)
  plus `WritePage` and `ReadAnalysisMD`/`WriteAnalysisMD` (the sidecar pair).
- `internal/retrieve` — **platform-agnostic read contract** (docs/retrieval.md): assembles
  `note.json` + `<PAGEID>.json` + `<PAGEID>.md` into one `NoteView` JSON (svg paths archive-relative)
  that **mirrors the on-disk structure** (`title.analysis.name`, `link.analysis.name`,
  `page.analysis.{content,fields}`). Consumers talk to snorg only via `list`/`retrieve`; read-only.
- `internal/config` — loads merged YAML config (provider creds + analysis prompts incl.
  `content.update_prompt` + `ingest.svg` toggles + `export.template`; `docs/config.md`). `Load(paths)`
  deep-merges files (later wins), defaults unset prompts and nil toggles (to true). Provider key
  resolution is a separate `ResolveAPIKey` (called by analyze, not `Load`, so export never runs it):
  literal `api_key` > `api_key_command` shell stdout > `OPENAI_API_KEY`. **Load does not enforce required fields** — each command validates its
  own section (analyze: `ValidateProvider`; export: non-empty `Export.Template`), so an export-only
  config needs no provider. Uses `yaml.v3`. `cmd/snorg`'s root command
  builds the path list (`configPaths`: `<archive-path>/config.yaml` first if present —
  `--no-archive-config` skips it — then the `-c` files, so `-c` overrides via the later-wins merge),
  loads once in the root's `Before` hook, and shares the result with the command via the `app`
  struct in `main.go` (the archive is the required global `-a`/`--archive` flag, so urfave's natural
  subcommand dispatch handles routing — no manual dispatch).
- `internal/export` — generic exporter (`export` cmd): renders a `retrieve.NoteView` through one pongo2
  template (`Render(view, template)`). Marshals the view to JSON then back into a `map[string]any`
  (numbers via `UseNumber` so levels/page numbers render as ints) for the pongo2 context, so templates
  bind to the `retrieve` json keys verbatim — no render-time enrichment. pongo2 is isolated here;
  read-only, output to stdout. Renders via a package `TemplateSet` with
  `TrimBlocks`+`LStripBlocks` on (clean multi-line templates; note: a line must not end with a block
  tag, and space before a `{% %}` is stripped — see `docs/config.md`). Filters live one file per
  concern: `denote.go` (FILE_ID/PAGEID → Emacs denote id `YYYYMMDDTHHMMSS`), `orgmode.go`
  (org-exclusive: `org` = markdown→org via pandoc shell-out, skipping pandoc for empty input;
  `nestorgheadings:N` = demote org headings by N stars) and `markdown.go` (Markdown-exclusive:
  `nestmdheadings:N` = demote Markdown `#` headings by N). Tests skip when pandoc is absent.
- `internal/analyze` — **incremental** vision-LLM analysis (`analyze` cmd): fingerprints the page by
  its **path geometry** (`pathHash`: the `d` of every `<path>`, whitespace-normalized — immune to
  recolor `fill`, background mode, links/nav/format), skips when it matches `analysis.source_hash`
  without rasterizing (unless `--force`). Otherwise rasterizes the page SVG (`oksvg`/`rasterx`) and
  transcribes it — through `Spec.Update` + the previous `<PAGEID>.md` when one exists (minimal-diff updates) —
  crops title/link rects via a `Transcriber`, runs custom `Spec.Fields` via a `Generator` (text→text
  over the transcribed content, **no image** — cheaper). Both seams are one openai-go client
  (endpoint/model/key from config). Writes `<PAGEID>.md` + `<PAGEID>.json`; returns
  `skipped|analyzed|updated`. Has external deps; the seams keep it testable without a network.
- `internal/ingest` — orchestrator. Re-ingest does an **incremental reconcile** per `FILE_ID`:
  prunes removed pages (all their `<PAGEID>.*`, `.md` included), writes only changed files,
  **preserves a page's `analysis`** across re-write (per-region transcriptions carried by exact
  rect match — moved regions drop theirs), and **preserves other `<PAGEID>.*` artifacts** — never
  a full rebuild. Note: page reorder rewrites neighbor SVGs (baked nav zones follow the order).

## `.note` format gotchas (full notes in docs/supernote-format.md)

- `supernote-tool convert -n` is **0-indexed**; footer index keys are **1-based** → footer page N == `-n (N-1)`.
- Page association comes from footer KEYS (`TITLE_`/`KEYWORD_`/`LINKO_` + 4-digit page), **not** from
  structured records; `KEYWORDPAGE` is unreliable (yields -1 on the sample).
- Keywords are invisible page-level metadata (no handwriting/region); text is in the `KEYWORD` field.
- Pages render **1920×2560**; all `*RECT` values are in that pixel space (crop = `x,y,x+w,y+h`).
- Star = per-page `FIVESTAR`; a link is internal iff `LINKFILEID == FILE_ID`; `LINKFILE` is a base64 path.
  Link target page = `PAGEID` (stable id, stored) not `OBJPAGE` (volatile number, dropped).
  Link `name` = base64-decoded `LINKFILE` basename without ext (target note's human name).
