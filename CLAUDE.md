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
  out to `supernote-tool` (supernotelib) — not parsed natively (yet) — the
  export `org` filter shells out to `pandoc`, and `internal/textmerge` shells out
  to `git` (diff/merge for edit-preserving analysis).
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
- `go run ./cmd/snorg -a <archive-path> query <filter> [arg]` — print PAGEIDs of matching pages (one per line); one filter per call: `all`, `note <FILE_ID>`, `unanalyzed`, `keyword <regexp>` (matches `Keyword.Text`), `starred`, `date <spec>` (day from the PAGEID's leading 8 digits; spec = `today`/`yesterday`/`YYYY-MM-DD`/`FROM..TO` with open ends); a `not <filter>` **prefix** inverts any filter (`query not starred` == the non-starred pages), so under piping `query A | query not B` == A minus B; pipes into `retrieve`/`analyze`/`export` (all three take PAGEIDs as args or stdin lines). `query` **itself** reads PAGEIDs from stdin when piped, restricting the filter to that set — so filters intersect: `query keyword foo | query date today` == foo ∩ today
- `go run ./cmd/snorg -a <archive-path> retrieve [PAGEID ...]` — the read interface: assembles the selected pages into a JSON **array of NoteViews** grouped per owning note (full note metadata, only the requested pages, placement order); whole note = `query note <FILE_ID> | retrieve`; unknown PAGEID errors
- `go run ./cmd/snorg -a <archive-path> [-c ...] analyze [--force] [PAGEID ...]` — incremental vision-LLM analysis; PAGEIDs from args or stdin (pipe from `query`); unchanged pages (path-geometry hash == `analysis.source_hash`; invariant under recolor/background/overlays) are skipped without an LLM call (no rasterize), changed ones re-transcribed through the update prompt + the previous **AI base** (`<PAGEID>.md` with the user's edit diff reverse-applied — user edits never reach the LLM; minimal diff), then 3-way merged with any user edits (overlap → outcome `conflict`, markers in the md, resolve via `analyze-edit`); writes `<PAGEID>.md` (effective content) + `<PAGEID>.json` (per-title/link `analysis.name`, `analysis.{source_hash,fields}`; fields generated from the effective content); a title/link name marked `analysis.edited` (a user override from `analyze-edit`) is kept as-is and its region is **not** re-transcribed, even under `--force`; provider + prompts from the config (`docs/config.md`); `api_key` falls back to `api_key_command` stdout then `OPENAI_API_KEY`
- `go run ./cmd/snorg -a <archive-path> analyze-edit <PAGEID>` — open the page's transcription in `$VISUAL`/`$EDITOR` (exactly one PAGEID, no stdin — the editor needs the terminal; no provider config needed). The buffer is content **plus** the title/link names: when the page has any title/link the editor opens a header of `<!-- title N (h..) -->` / `<!-- link N → target -->` markers (context after the index is informational — snorg keys off kind+index only) with each name below, then a `<!-- content -->` marker and the content (a page with no regions opens as bare content, as before). On save the content section flows through the usual sidecars — `<PAGEID>.md` stays the effective content, the divergence from the AI base is stored as `<PAGEID>.md.diff` (unified diff base→md, exists iff they diverge) — and each changed name becomes a **user override** (`analysis.edited`) in `<PAGEID>.json` that `analyze` keeps (and skips re-transcribing) until the region's rect changes. Works on never-analyzed pages too (empty content → hand-written transcription, empty base ⇒ first `analyze` conflicts once); needs `git` on PATH
- `go run ./cmd/snorg -a <archive-path> [-c ...] export [PAGEID ...]` — PAGEIDs from args or stdin (pipe from `query`); groups pages per owning note like `retrieve` and renders the config's single `export.template` (pongo2/Jinja2) **once** over the whole result to stdout; template context is the `retrieve` JSON array under the `notes` key (snake_case: `notes[].pages[].titles/keywords/links/analysis.content`, `title.analysis.name`, `link.analysis.name`), so one template can span notes; filters: `denote` (FILE_ID/PAGEID → denote id), `org` (markdown→org via pandoc), `html` (markdown→HTML via pandoc), `nestorgheadings:N` (demote org headings), `nestmdheadings:N` (demote Markdown headings); needs no `provider` creds; see `examples/config.yaml` (Markdown), `examples/emacs/orgmode.yaml` (org), and `examples/web/` (static HTML site: `export.sh` runs `index.yaml` + per-note `note.yaml` over piped PAGEIDs, copies the pages' SVGs)
- `go run ./cmd/snorg -a <archive-path> serve [-l ADDR] [PAGEID ...]` — the built-in, zero-setup HTTP viewer (no provider/config needed). PAGEIDs from args or stdin; **neither = the whole archive**. Assembles the pages like `retrieve` and serves a minimal, dependency-free HTML site on `-l`/`--listen` (default `127.0.0.1:8080`): `/` = note gallery (name from `note.json` `source` sans `.note`, first-page SVG thumbnail) → `/note/<FILE_ID>` = that note's page gallery with a click-to-enlarge lightbox (←/→ pages, Esc) → `/svg/<FILE_ID>/<PAGEID>.svg` streams the page SVG straight from the archive (only pages in the served set; nothing copied to disk — in-memory viewer)

## Architecture

Flow: `cmd/snorg` → `internal/ingest` orchestrates `snote.Source.Read` → render SVGs → `archive.Write`.

- `internal/snote` — device-agnostic domain model (`Note`/`Page`/`Title`/`Keyword`/`Link`)
  and the `Source` interface, the **seam isolating the format**. A native-Go parser would
  be a new `Source` impl, leaving callers untouched.
- `internal/snote/sntool` — `Source` impl shelling to `supernote-tool` (`analyze` + `convert -t svg`);
  `footer.go` is the risky part (page association, see gotchas).
- `internal/archive` — owns the on-disk layout `<archive>/<FILE_ID>/{note.json,<PAGEID>.json,<PAGEID>.md[.diff],<PAGEID>.svg}`;
  `doc.go` is the JSON serialization boundary (the stable plaintext contract; add fields freely —
  per-title/per-link `analysis` is nested on the items (`name` + `edited` = user-override flag), page
  `analysis` holds `source_hash`+`fields`, the content transcription lives in the `<PAGEID>.md`
  sidecar); `Write` runs the config-driven
  SVG pipeline (background mode `extract`/`inline`/`blank`/`remove` → `recolor` pen-shade fills →
  `injectNav` prev/next half-page zones → `injectLinks` note links → `formatSVG`; nav is emitted
  before links so links win hit-testing — SVG picks the later element);
  `read.go` are layout-aware accessors (`List`/`ReadNote`/`ReadPage`/`ReadSVG`/`SVGRel`/`FindPage`)
  plus `WritePage` and `ReadAnalysisMD`/`WriteAnalysisMD` (the sidecar pair; `NormMD` is the stored
  form — one trailing newline, empty collapses to empty); `editdiff.go` owns the `<PAGEID>.md.diff`
  sidecar and its invariant (**md = effective content; diff = AI base → md, exists iff they
  diverge**): `ReadAnalysisBase` (reverse-applies the diff; fails loudly with a recovery hint when
  the md was edited outside the tool), `WriteAnalysisEdit` (store an edit; removes both sidecars
  when base and content are empty), `MergeAnalysis` (3-way merge of a fresh transcription with the
  user's edits; theirs becomes the new base, conflict markers land in the md).
- `internal/retrieve` — **platform-agnostic read contract** (docs/retrieval.md): `Get(a, pageIDs)`
  assembles `note.json` + `<PAGEID>.json` + `<PAGEID>.md` into `[]*NoteView` grouped per owning
  note (List order, pages in placement order, unknown PAGEID errors); svg paths archive-relative;
  the view **mirrors the on-disk structure** (`title.analysis.name`, `link.analysis.name`,
  `page.analysis.{content,fields}`) via its own view types — the internal `edited` override flag is
  **not** exposed; `analysis.content` is exposed whenever the md exists (AI or
  hand-written), fields only once AI-analyzed. Consumers talk to snorg only via
  `list`/`query`/`retrieve`; read-only.
- `internal/serve` — the built-in HTTP viewer (`serve` cmd): `Handler(a, views)` builds a
  `net/http.ServeMux` over the assembled `[]*retrieve.NoteView` — `/` (note gallery: name +
  first-page thumbnail), `/note/{fid}` (that note's page gallery + a vanilla-JS lightbox), and
  `/svg/{fid}/{name}` (page SVG streamed via `archive.ReadSVG`, gated to pages in the served set
  so the viewer never exposes the whole archive). Self-contained HTML/CSS/JS via `html/template`,
  no external deps; read-only, nothing written to disk. Testable via `httptest` (no network).
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
- `internal/export` — generic exporter (`export` cmd): renders the retrieved `[]*retrieve.NoteView`
  through one pongo2 template in a single pass (`Render(views, template)`). Marshals the views to JSON
  then back (numbers via `UseNumber` so levels/page numbers render as ints) into the pongo2 context
  under the `notes` key, so templates bind to the `retrieve` json array verbatim — no render-time
  enrichment — and can span notes. pongo2 is isolated here;
  read-only, output to stdout. Renders via a package `TemplateSet` with
  `TrimBlocks`+`LStripBlocks` on (clean multi-line templates; note: a line must not end with a block
  tag, and space before a `{% %}` is stripped — see `docs/config.md`). Filters live one file per
  concern: `denote.go` (FILE_ID/PAGEID → Emacs denote id `YYYYMMDDTHHMMSS`), `orgmode.go`
  (org-exclusive: `org` = markdown→org via pandoc shell-out, skipping pandoc for empty input;
  `nestorgheadings:N` = demote org headings by N stars), `html.go` (HTML-exclusive: `html` =
  markdown→HTML fragment via pandoc shell-out, marked safe/unescaped, same empty-input skip) and
  `markdown.go` (Markdown-exclusive: `nestmdheadings:N` = demote Markdown `#` headings by N). Tests
  skip when pandoc is absent.
- `internal/analyze` — **incremental** vision-LLM analysis (`analyze` cmd): fingerprints the page by
  its **path geometry** (`pathHash`: the `d` of every `<path>`, whitespace-normalized — immune to
  recolor `fill`, background mode, links/nav/format), skips when it matches `analysis.source_hash`
  without rasterizing (unless `--force`). Otherwise rasterizes the page SVG (`oksvg`/`rasterx`) and
  transcribes it — through `Spec.Update` + the previous **AI base** (`archive.ReadAnalysisBase`;
  user edits never reach the LLM) when one exists (minimal-diff updates) — merges the result with
  any user edits (`archive.MergeAnalysis`), crops title/link rects via a `Transcriber`, runs custom
  `Spec.Fields` via a `Generator` (text→text over the **effective** content, **no image** — cheaper).
  Both seams are one openai-go client (endpoint/model/key from config). Writes `<PAGEID>.md` +
  `<PAGEID>.json`; returns `skipped|analyzed|updated|conflict` (conflict ≠ error; the batch continues).
  Has external deps; the seams keep it testable without a network.
- `internal/edit` — `analyze-edit` orchestration: `buffer.go` serializes the page to one editable
  document (per-region name header + `<!-- content -->` + content; parses back keying regions by
  marker kind+index, ignoring the informational context, content taken verbatim after the first
  content marker) and opens it in the editor (`sh -c` so `$EDITOR` may carry args, terminal inherited,
  temp copy — an aborted editor changes nothing); the content flows through `archive.WriteAnalysisEdit`
  and each changed name becomes an `Edited` override via `archive.WritePage`. Returns the content
  outcome (`unchanged|edited|reverted`) plus the count of changed names; a malformed header (wrong
  marker count, missing content marker) errors and writes nothing. Checks git availability **before**
  launching the editor.
- `internal/textmerge` — the only place that executes `git` (`Diff`/`Unapply`/`Merge` +
  `Available`); pure text-in/text-out plumbing, byte-faithful (normalization is the archive's job),
  user/system git config disabled. Tests (here and in dependents) skip when git is absent.
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
