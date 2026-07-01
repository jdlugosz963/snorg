# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

SNORG (supernote-organizer) ingests Supernote `.note` files into a plaintext,
machine-readable, VCS-friendly *archive*, so notes can later be retrieved and exported.
Read `docs/principles.md` (project rules), `docs/architecture.md` (modules/CLI),
`docs/supernote-format.md` (`.note` format + tooling) before substantial work.

## Project rules

- Go, stdlib only — **except** `internal/analyze` (the AI layer), which may use
  `openai-go` (LLM client) and `oksvg`/`rasterx` (SVG rasterization),
  `internal/config`, which may use `gopkg.in/yaml.v3` (config parsing), and
  `internal/export`, which may use `pongo2/v6` (Jinja2-style templating). Everything else
  stays stdlib-only. The `.note` binary format is handled by shelling out to
  `supernote-tool` (supernotelib) — not parsed natively (yet).
- No backward compatibility: when something must change, rewrite it cleanly rather
  than preserving legacy.
- All operational data is plaintext; all docs in English, maximally concise.

## Commands

- `go build ./... && go vet ./... && gofmt -l .` — build, vet, format check (gofmt output must be empty)
- `go test ./...` — all tests
- `go test ./internal/snote/sntool` — fast unit tests (footer-key parsing)
- `go test -run TestIngestSampleNote ./internal/ingest` — e2e ingest; **slow (~1 min**, renders SVGs via supernote-tool) and skips if `supernote-tool` is not on PATH
- `go run ./cmd/snorg ingest [-j N] <file-or-dir> <archive-path>` — register a note (or all `*.note` under a dir) into an archive; `-j` caps concurrent notes (default NumCPU)
- `go run ./cmd/snorg list <archive-path>` — list FILE_IDs (one per line)
- `go run ./cmd/snorg retrieve <archive-path> <FILE_ID>` — assembled note as JSON (the read interface)
- `go run ./cmd/snorg query <archive-path> <filter> [arg]` — print PAGEIDs of matching pages (one per line); one filter per call: `keyword <regexp>` (matches `Keyword.Text`), `starred`
- `go run ./cmd/snorg analyze [-c config.yaml ...] -page-id <PAGEID> <archive-path>` — vision-LLM analysis of one page → writes `analysis` (content + per-title/link transcriptions + custom `fields`) into `<PAGEID>.json`; provider (endpoint/model/key) and prompts come from merged YAML config (`docs/config.md`); `api_key` falls back to `OPENAI_API_KEY`
- `go run ./cmd/snorg export [-c config.yaml ...] <archive-path> <FILE_ID>` — render the retrieved note JSON through the config's single `export.template` (pongo2/Jinja2) to stdout; template context is the `retrieve` JSON verbatim (snake_case keys: `pages[].titles/keywords/links/analysis.content`); needs no `provider` creds (`docs/config.md`)

## Architecture

Flow: `cmd/snorg` → `internal/ingest` orchestrates `snote.Source.Read` → render SVGs → `archive.Write`.

- `internal/snote` — device-agnostic domain model (`Note`/`Page`/`Title`/`Keyword`/`Link`)
  and the `Source` interface, the **seam isolating the format**. A native-Go parser would
  be a new `Source` impl, leaving callers untouched.
- `internal/snote/sntool` — `Source` impl shelling to `supernote-tool` (`analyze` + `convert -t svg`);
  `footer.go` is the risky part (page association, see gotchas).
- `internal/archive` — owns the on-disk layout `<archive>/<FILE_ID>/{note.json,<PAGEID>.json,<PAGEID>.svg}`;
  `doc.go` is the JSON serialization boundary (the stable plaintext contract; add fields freely —
  `PageDoc.Analysis` holds derived AI output); `Write` also bakes each page's links into the
  SVG as relative `<a>` hyperlinks (`injectLinks`, resolver pipeline open for future link kinds —
  only note-page targets resolve today); `read.go` are layout-aware accessors
  (`List`/`ReadNote`/`ReadPage`/`ReadSVG`/`SVGRel`/`FindPage`) plus `WritePage` (single-page write).
- `internal/retrieve` — **platform-agnostic read contract** (docs/retrieval.md): assembles
  `note.json` + `<PAGEID>.json` into one `NoteView` JSON (svg paths archive-relative). Consumers
  (e.g. a future org-mode generator) talk to snorg only via `list`/`retrieve`; snorg stays read-only.
- `internal/config` — loads merged YAML config (provider creds + analysis prompts + `export.template`;
  `docs/config.md`). `Load(paths)` deep-merges files (later wins), defaults unset prompts, falls back
  `api_key` to `OPENAI_API_KEY`. **Load does not enforce required fields** — each command validates its
  own section (analyze: `ValidateProvider`; export: non-empty `Export.Template`), so an export-only
  config needs no provider. Uses `yaml.v3` (its allowed external dep).
- `internal/export` — generic exporter (`export` cmd): renders a `retrieve.NoteView` through one pongo2
  template (`Render(view, template)`). Marshals the view to JSON then back into a `map[string]any`
  (numbers via `UseNumber` so levels/page numbers render as ints) for the pongo2 context, so templates
  bind to the `retrieve` json keys. pongo2 is isolated here (its allowed external dep); read-only,
  output to stdout. Testable without network or `supernote-tool`. Renders via a package `TemplateSet`
  with `TrimBlocks`+`LStripBlocks` on (clean multi-line templates; note: a line must not end with a
  block tag, and space before a `{% %}` is stripped — see `docs/config.md`). Two template helpers: a
  custom `denote` pongo2 filter (`filters.go`: FILE_ID → Emacs denote id `YYYYMMDDTHHMMSS`), and
  `enrichAnalysis` zips each page's index-aligned `analysis.links/titles` into `links[i]`/`titles[i]`
  as a nested `analysis` so templates reach `link.analysis.name` / `title.analysis.name`.
- `internal/analyze` — vision-LLM analysis of one page (`analyze` cmd): rasterizes the page SVG
  (`oksvg`/`rasterx`), crops title/link rects, transcribes via a `Transcriber`, and runs custom
  `Spec.Fields` via a `Generator` (text→text over the transcribed content, **no image** — cheaper).
  Both seams are implemented by one openai-go client (endpoint/model/key from config). Writes
  `analysis` into `<PAGEID>.json`. Has external deps; the seams keep it testable without a network.
- `internal/ingest` — orchestrator. Re-ingest does an **incremental reconcile** per `FILE_ID`:
  prunes removed pages (all their `<PAGEID>.*`), writes only changed files, **preserves a page's
  `analysis`** across re-write, and **preserves other `<PAGEID>.*` artifacts** — never a full rebuild.

## `.note` format gotchas (full notes in docs/supernote-format.md)

- `supernote-tool convert -n` is **0-indexed**; footer index keys are **1-based** → footer page N == `-n (N-1)`.
- Page association comes from footer KEYS (`TITLE_`/`KEYWORD_`/`LINKO_` + 4-digit page), **not** from
  structured records; `KEYWORDPAGE` is unreliable (yields -1 on the sample).
- Keywords are invisible page-level metadata (no handwriting/region); text is in the `KEYWORD` field.
- Pages render **1920×2560**; all `*RECT` values are in that pixel space (crop = `x,y,x+w,y+h`).
- Star = per-page `FIVESTAR`; a link is internal iff `LINKFILEID == FILE_ID`; `LINKFILE` is a base64 path.
  Link target page = `PAGEID` (stable id, stored) not `OBJPAGE` (volatile number, dropped).
  Link `name` = base64-decoded `LINKFILE` basename without ext (target note's human name).
