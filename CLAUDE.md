# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

SNORG (supernote-organizer) ingests Supernote `.note` files into a plaintext,
machine-readable, VCS-friendly *archive*, so notes can later be retrieved and exported.
Read `docs/principles.md` (project rules), `docs/architecture.md` (modules/CLI),
`docs/supernote-format.md` (`.note` format + tooling), `docs/templates.md`
(template regions) before substantial work.

## Project rules

- Go. External deps by package: `internal/analyze` and `internal/serve` use
  `oksvg`/`rasterx` (SVG rasterization — analyze for the vision prompt, serve for
  gallery thumbnails); `internal/analyze` also uses `openai-go` (LLM client);
  `internal/config` uses `gopkg.in/yaml.v3` (YAML parsing); `internal/export` uses `pongo2/v6`
  (Jinja2-style templating); `cmd/snorg` uses `urfave/cli/v3` (CLI framework);
  `internal/snote/sntool` uses `github.com/jdlugosz963/sntool` (the native-Go
  `.note` parser + SVG renderer, pure Go via `gotranspile/gotrace` potrace), so
  the `.note` format is parsed and rendered in-process with no PATH tool;
  `internal/textmerge` uses `github.com/njchilds90/go-diffpatch` (line diff
  compute + reverse-apply for `Diff`/`Unapply`) +
  `github.com/sourcegraph/go-diff` (renders/parses the stored patch as a normal
  unified diff) + `github.com/epiclabs-io/diff3` (3-way merge for `Merge`),
  so edit-preserving analysis is pure Go with no PATH tool. PATH tool (not a
  go.mod dep): the export `org`/`html` filters shell out to `pandoc`.
- No backward compatibility: when something must change, rewrite it cleanly rather
  than preserving legacy.
- All operational data is plaintext; all docs in English, maximally concise.

## Commands

- `go build ./... && go vet ./... && gofmt -l .` — build, vet, format check (gofmt output must be empty)
- `go test ./...` — all tests
- `go test -run TestIngestSampleNote ./internal/ingest` — e2e ingest through the native `sntool` backend (parse + render in pure Go, no PATH tool); skips only if the `note.note` fixture is absent
- CLI shape: `snorg [-a <archive-path>] [-c config.yaml ...] [--no-user-config] <command> [command flags] [args]` — the archive is the global `-a`/`--archive` flag and the config flags are global too, so all come **before** the command; the root's `Before` hook loads the merged config once in **increasing precedence**: `$XDG_CONFIG_HOME/snorg/config.yaml` (the XDG **user config**, the only auto-loaded file, `--no-user-config` to skip) → `-c` files (later wins), and hands it to the command, which validates only the sections it uses. An archive's own `config.yaml` is **not** auto-loaded — pass it explicitly with `-c` (or `include:` it). `-a` is **optional** when the merged config sets the top-level `archive:` key (the flag wins; a leading `~` is expanded), so a bare `snorg <command>` targets that archive
- `go run ./cmd/snorg -a <archive-path> [-c ...] ingest [-j N] <file-or-dir>` — register a note (or all `*.note` under a dir) into the archive; `-j` caps concurrent notes (default NumCPU); config drives the SVG pipeline (`ingest.svg.{links,navigation,format}` bools default true; `background` mode `extract`(default)`|inline|blank|remove`; `colors` remaps the 4 default pen-shade fills). None of these change the analyze fingerprint (the canonical black-on-white rasterization — pen colors forced black, overlays dropped), so restyling never re-transcribes.
- `go run ./cmd/snorg -a <archive-path> list [-l]` — the archive-inspection command. Bare `list` prints FILE_IDs (one per line); `-l`/`--long` appends the note name (source sans `.note`, FILE_ID fallback) as a tab-separated column (`<FILE_ID>\t<name>`), FILE_ID kept first so `cut -f1`/`awk '{print $1}'` still extracts it. Subcommands enumerate the archive's **labels**: `list keywords` (distinct device `Keyword.Text`) and `list tags` (distinct snorg-managed tags — see the `tag` command), each printing one value per line sorted, `-l` appending a tab-separated per-value page count (`<value>\t<count>`)
- `go run ./cmd/snorg -a <archive-path> query <filter> [arg]` — print PAGEIDs of matching pages (one per line); one filter per call: `all`, `note <FILE_ID>`, `unanalyzed`, `keyword <regexp>` (matches device `Keyword.Text`), `tag <regexp>` (matches a snorg-managed tag — see the `tag` command), `content <regexp>` (matches the page's transcribed `<PAGEID>.md`), `starred`, `date <spec>` (day from the PAGEID's leading 8 digits; spec = `today`/`yesterday`/`YYYY-MM-DD`/`FROM..TO` with open ends); a `not <filter>` **prefix** inverts any filter (`query not starred` == the non-starred pages), so under piping `query A | query not B` == A minus B; pipes into `retrieve`/`analyze`/`export` (all three take PAGEIDs as args or stdin lines). `query` **itself** reads PAGEIDs from stdin when piped, restricting the filter to that set — so filters intersect: `query keyword foo | query date today` == foo ∩ today. `-l`/`--long` switches to a **browse-only** annotated form: tab-separated columns `<PAGEID>\t<note>\tp<page#>\t<*?>\t<headings>\t#<keyword>…\t@<tag>…` (note = `source` sans `.note`; `*` marks a starred page, empty otherwise; headings = analyzed title names joined ` / `, empty until analyzed; keywords = device metadata as `#kw`; tags = snorg-managed labels as `@tag`; both present even without analysis, so a fuzzy finder can filter on either). Fixed `\t` separators keep the columns machine-splittable (`cut -f`) regardless of value widths. PAGEID stays the **first** whitespace field on purpose (`awk '{print $1}'` extracts it — e.g. `query -l all | fzf -m | awk '{print $1}' | serve -f`). **Not** pipe-safe as-is, so never feed the annotated form downstream
- `go run ./cmd/snorg -a <archive-path> tag [-r] <tag> [PAGEID ...]` — add (or `-r`/`--remove`, remove) a **snorg-managed tag** on pages — the archive-side organizing mechanism, independent of device `keywords` (which are read-only, set on the Supernote). `<tag>` is the first arg; PAGEIDs follow it, or come from stdin (pipe from `query`, e.g. `query keyword foo | tag important`). Stored as a **sorted, de-duplicated** `PageDoc.tags []string` (no provider/config needed); prints `tagged`/`untagged N of M pages`. Tags live in `<PAGEID>.json` and are **carried across re-ingest** (like `analysis`, since they aren't sourced from the `.note`). Query them with `query tag <regexp>`, enumerate with `list tags`; surfaced in `retrieve`/`export` (`page.tags`), `serve` (lightbox), and `query -l` (the `@tag` column)
- `go run ./cmd/snorg -a <archive-path> retrieve [PAGEID ...]` — the read interface: assembles the selected pages into a JSON **object** `{archive, notes}` — `archive` = the **absolute archive root** (so a consumer resolves the pages' archive-relative `svg` paths without knowing where the archive lives), `notes` = the **array of NoteViews** grouped per owning note (full note metadata, only the requested pages, placement order); a templated page carries its transcription in `analysis.regions[]` (one entry per config box, label/rect resolved from the config's `templates:` section) rather than `analysis.content`; whole note = `query note <FILE_ID> | retrieve`; unknown PAGEID errors
- `go run ./cmd/snorg -a <archive-path> [-c ...] analyze [--force] [PAGEID ...]` — incremental vision-LLM analysis; PAGEIDs from args or stdin (pipe from `query`); unchanged pages (canonical-raster mask hash == `analysis.source_hash`; invariant under recolor/background/overlays) are skipped without an LLM call, changed ones re-transcribed through the update prompt + the previous **AI base** (`<PAGEID>.md` with the user's edit diff reverse-applied — user edits never reach the LLM; minimal diff), then 3-way merged with any user edits (overlap → outcome `conflict`, markers in the md, resolve via `analyze-edit`); writes `<PAGEID>.md` (effective content) + `<PAGEID>.json` (per-title/link `analysis.name`, `analysis.{source_hash,fields}`; fields generated from the effective content); a title/link name marked `analysis.edited` (a user override from `analyze-edit`) is kept as-is and its region is **not** re-transcribed, even under `--force`; **template regions**: a page whose `background_hash` matches a config `templates:` entry is analyzed **per box** instead of whole-page — each `analyze:true` box is fingerprinted by its rect cropped out of the canonical-raster mask (pixel-space, so a stroke crossing the edge counts only its in-box pixels; per-box skip, `pd.analysis.regions[].source_hash`), cropped + transcribed, and written to `<PAGEID>.md` (id-keyed region sections in place of free-form content, 3-way merged like content) (`docs/templates.md`); provider + prompts from the config (`docs/config.md`); `api_key` falls back to `api_key_command` stdout then `OPENAI_API_KEY`
- `go run ./cmd/snorg -a <archive-path> analyze-edit <PAGEID>` — open the page's transcription in `$VISUAL`/`$EDITOR` (exactly one PAGEID, no stdin — the editor needs the terminal; no provider config needed). The buffer is content **plus** the title/link names: when the page has any title/link the editor opens a header of `<!-- title N (h..) -->` / `<!-- link N → target -->` markers (context after the index is informational — snorg keys off kind+index only) with each name below, then a `<!-- content -->` marker and the content (a page with no regions opens as bare content, as before). On save the content section flows through the usual sidecars — `<PAGEID>.md` stays the effective content, the divergence from the AI base is stored as `<PAGEID>.md.diff` (a unified diff base→md, exists iff they diverge) — and each changed name becomes a **user override** (`analysis.edited`) in `<PAGEID>.json` that `analyze` keeps (and skips re-transcribing) until the region's rect changes. A **templated** page instead opens one `<!-- region <id> (<label>) -->` section per template box (keyed by id; label informational) after the header as its body (no `<!-- content -->` marker — the regions are the content; one appearing is an error), saved to the same `<PAGEID>.md[.diff]` via the same 3-way merge. Works on never-analyzed pages too (empty content → hand-written transcription, empty base ⇒ first `analyze` conflicts once); pure Go, no PATH tool
- `go run ./cmd/snorg -a <archive-path> [-c ...] export [PAGEID ...]` — PAGEIDs from args or stdin (pipe from `query`); groups pages per owning note like `retrieve` and renders the config's single `export.template` (pongo2/Jinja2) **once** over the whole result to stdout; template context is the **whole `retrieve` object** — `archive` (absolute root) + `notes` (snake_case: `notes[].pages[].tags/titles/keywords/links/analysis.content`, `title.analysis.name`, `link.analysis.name`, and `page.analysis.regions[].{id,label,rect,content}` for a templated page), so one template can span notes and build absolute svg paths (`{{ archive }}/{{ p.svg }}`); filters: `denote` (FILE_ID/PAGEID → denote id), `org` (markdown→org via pandoc), `html` (markdown→HTML via pandoc), `nestorgheadings:N` (demote org headings), `nestmdheadings:N` (demote Markdown headings); needs no `provider` creds; see `examples/config.yaml` (Markdown), `examples/emacs/orgmode.yaml` (org), and `examples/web/` (static HTML site: `export.sh` runs `index.yaml` + per-note `note.yaml` over piped PAGEIDs, copies the pages' SVGs)
- `go run ./cmd/snorg -a <archive-path> serve [-l ADDR] [--flat] [PAGEID ...]` — the built-in, zero-setup HTTP viewer (no provider/config needed). PAGEIDs from args or stdin; **neither = the whole archive**. Assembles the pages like `retrieve` and serves a minimal, dependency-free HTML site on `-l`/`--listen` (default `127.0.0.1:8080`): `/` = note gallery (name from `note.json` `source` sans `.note`, first-page SVG thumbnail) → `/note/<FILE_ID>` = that note's page gallery with a click-to-enlarge lightbox (←/→ pages, Esc) → `/svg/<FILE_ID>/<PAGEID>.svg` streams the page SVG straight from the archive (only pages in the served set; nothing copied to disk — in-memory viewer). `--flat`/`-f` drops the per-note grouping: `/` becomes **one flat gallery of all selected pages** (across notes, each captioned `<note name> · <page number>`) with the same lightbox; in-page SVG links then reopen `/` on the target (`/?page=<PAGEID>`) instead of the note page. Every page holds an SSE stream to `/events` carrying the process's random **boot-id**; on reconnect the viewer reloads to `/` iff the boot-id changed — so restarting `serve` (e.g. over a new selection) auto-refreshes open tabs, while a blip to the same process doesn't (liveness refresh, not live-reload-on-edit)
- `go run ./cmd/snorg -a <archive-path> migrate [PAGEID ...]` — upgrade the archive's `note.json`/`<PAGEID>.json` to the current `schema_version` (no provider/config needed). PAGEIDs from args or stdin; **neither = the whole archive**; a page selection also migrates its owning `note.json`. The **only** command that reads stale grammars — every other command hard-errors (`ErrSchemaVersion`, "run `snorg migrate`") on a version mismatch; `migrate` reads each file **raw** (no `verifySchema`) and enumerates via `os.Stat`/glob (never the gated readers), walks it **one version at a time** through the version-indexed `archive.schemaMigrations` chain (v→v+1→…→`CurrentSchemaVersion`), then re-serializes through the canonical `NoteDoc`/`PageDoc` (bytes identical to ingest). It also normalizes each page's **unversioned** `<PAGEID>.md.diff` sidecar (content-sniffed: legacy JSON `go-diffpatch` patch → unified diff via `textmerge.ConvertLegacyDiff`), reported as a separate `diff <PAGEID>: migrated` result — this runs regardless of the JSON version walk (idempotent, self-heals a crash between the two writes), since the sidecar carries no `schema_version` to walk. Idempotent (already-current = `current`, no rewrite); a file newer than the binary errors rather than downgrading. Bump `CurrentSchemaVersion` + append one `schemaMigrations` step per contract change (`len == CurrentSchemaVersion`)

## Architecture

Flow: `cmd/snorg` → `pkg/snorg` (public API) → `internal/ingest` orchestrates `snote.Source.Read` → render SVGs → `archive.Write`.

- `pkg/snorg` — the **public Go API** (`github.com/jdlugosz963/snorg/pkg/snorg`), the
  only importable surface (everything else is `internal/`). A `Client` (from `Open`
  or `Resolve`) bundles archive + merged config and exposes every capability as a
  method (`List`/`Query`/`Retrieve`/`Export`/`ServeHandler`/`Ingest`/`Migrate`/
  `Analyze`/`PageBuffer`+`ApplyPage` programmatic-edit/`EditPage` interactive); it
  imports the internal packages and re-exports their returned types as **aliases**
  (`Result`, `Match`, `Spec`, `Config`, …) so an external caller names them without
  an `internal/*` import. `cmd/snorg` is a thin CLI over this package (flag parsing +
  PAGEID stdin conventions + formatting stay in `cmd`); the config layering, `~`
  expansion, date-spec and query-filter DSL live here (`Resolve`, `ParseFilter`,
  `ParseDateSpec`). `Open` also **bridges** the config's `templates:` specs into the
  archive (`config.TemplateSpec` → `archive.TemplateSpec` via `SetTemplateSpecs`), keeping
  `internal/config` and `internal/archive` mutually independent. **Change reporting:**
  `Ingest` surfaces the incremental reconcile — each `IngestResult.Report` (`*WriteReport`)
  is the per-note/per-page/per-file change set (`NoteChanged`, `Pages[]` with
  `New`/`JSONChanged`/`SVGChanged`/`BackgroundChanged` + `DroppedRegions` for
  rect-moved title/link analyses, `Pruned`; empty when already current) and `Analyze`
  reports per-template-box outcomes (`AnalyzeResult.Regions []RegionOutcome`,
  `Skipped` vs re-transcribed). See `docs/library.md`. **When adding or changing a capability,
  keep the facade in sync** (a new returned type needs an alias, or an external
  consumer can't name it).

- `internal/snote` — device-agnostic domain model (`Note`/`Page`/`Title`/`Keyword`/`Link`)
  and the `Source` interface, the **seam isolating the format**. The concrete impl lives
  below; a different backend would just be another `Source`, leaving callers untouched.
- `internal/snote/sntool` — the `Source` impl: wraps the native-Go
  `github.com/jdlugosz963/sntool` library (`sntool.Open` → domain model, `render.SVG` per
  page), pure Go, no external tool. sntool resolves each title/keyword/link to its owning
  page itself (`Annotation.PageNumber`) and classifies each link (`Link.Kind()` →
  note/file/web/unknown, `Link.Target()` decoded LINKFILE, `Link.Name()` per-kind label),
  so this adapter is a straight mapping — no footer-key parsing here. Its SVG (pen-shade fills `#000000/#9d9d9d/#c9c9c9/#fefefe`,
  inline `xlink:href="data:image/…"` background, `xmlns:xlink` root, `<path fill d>`) is
  exactly what the archive SVG pipeline (recolor/background/nav/links) and the analyze canonical-raster fingerprint expect.
- `internal/archive` — owns the on-disk layout `<archive>/<FILE_ID>/{note.json,<PAGEID>.json,<PAGEID>.md[.diff],<PAGEID>.svg}` (a templated page's `.md` holds region sections in place of content) (template background images are ordinary files referenced by the config's `templates:` section, e.g. under `<archive>/templates/`);
  `doc.go` is the JSON serialization boundary (the stable plaintext contract; add fields freely —
  per-title/per-link `analysis` is nested on the items (`name` + `edited` = user-override flag), page
  `analysis` holds `source_hash`+`fields`+`regions[].{id,source_hash}` (per-box fingerprint state),
  the content transcription lives in the `<PAGEID>.md`
  sidecar; the top-level `tags` (snorg-managed labels from the `tag` command, sorted/deduped,
  carried across re-ingest like `analysis` since they aren't `.note`-derived); the top-level
  `background_hash` (template selector, sha256 of the decoded background,
  stamped by ingest) + `analysis.regions[]` support template regions —
  `templates.go` builds the template set from specs injected via `SetTemplateSpecs`
  (`TemplateSpec`/`Template`/`Box`, hash each absolute `image` path → `MatchBackground`; the specs
  come from the merged config's `templates:` section, bridged in by `pkg/snorg.Open`, no provider
  creds), `regions.go` (de)serializes the id-keyed
  region sections a templated page holds in `<PAGEID>.md` (`docs/templates.md`)); both `note.json`/`<PAGEID>.json` carry `schema_version` (= `CurrentSchemaVersion`,
  stamped by every writer): `ReadNote`/`ReadPage` **hard-reject** a mismatch (`ErrSchemaVersion`,
  "run `snorg migrate`") so no command touches a grammar it doesn't understand and re-ingest aborts
  rather than clobbering a stale page — the `migrate` command reads old files raw
  and walks them forward one version per step (v→v+1→…→current); bump the constant + add a step on any
  contract change; `Write` runs the config-driven
  SVG pipeline (background mode `extract`/`inline`/`blank`/`remove` → `recolor` pen-shade fills →
  `injectNav` prev/next half-page zones → `injectLinks` (bakes navigable links: note→page-SVG jumps
  and web→`<a target="_blank">` URLs via `linkAnchor`; file/unknown links carry only a device-local
  path so they bake nothing) → `formatSVG`; nav is emitted
  before links so links win hit-testing — SVG picks the later element);
  `read.go` are layout-aware accessors (`List`/`ReadNote`/`ReadPage`/`ReadSVG`/`SVGRel`/`FindPage`)
  plus `WritePage` and `ReadAnalysisMD`/`WriteAnalysisMD` (the sidecar pair; `NormMD` is the stored
  form — one trailing newline, empty collapses to empty); `editdiff.go` owns the `.md.diff`
  sidecar and its invariant (**md = effective content; diff = AI base → md, exists iff they
  diverge**) via `ReadAnalysisBase`/`WriteAnalysisEdit`/`MergeAnalysis` over the one `<PAGEID>.md[.diff]`
  pair — serving both a normal page's content and a templated page's region-section document (a page is
  one or the other, so they share the file): `ReadAnalysisBase`
  reverse-applies the diff (fails loudly with a recovery hint when the md was edited outside the tool),
  `WriteAnalysisEdit` stores an edit (removes both sidecars when base and content are empty),
  `MergeAnalysis` 3-way-merges a fresh transcription with the user's edits (theirs becomes the new base,
  conflict markers land in the md). `migrate.go` is the
  **only** un-gated reader (`MigrateAll`/`MigratePages` + the version-indexed `schemaMigrations`
  chain): reads raw, enumerates via `List`/`archivedPageIDs`, walks each file v→v+1→current, writes
  canonical `NoteDoc`/`PageDoc` bytes, and (per page) normalizes the unversioned `<PAGEID>.md.diff`
  sidecar via `migrateEditDiff` (content-sniffed legacy JSON→unified, separate from the version walk)
  (see the `migrate` command above).
- `internal/retrieve` — **platform-agnostic read contract** (docs/retrieval.md): `Get(a, pageIDs)`
  assembles `note.json` + `<PAGEID>.json` + `<PAGEID>.md` into a `Result{Archive, Notes}` — `Notes`
  are `[]*NoteView` grouped per owning note (List order, pages in placement order, unknown PAGEID
  errors), `Archive` is the **absolute** archive root (`filepath.Abs(a.Root)`) the pages' relative
  svg paths resolve against, so the payload is self-contained; svg paths archive-relative;
  the view **mirrors the on-disk structure** (`title.analysis.name`, `link.analysis.name`,
  `page.analysis.{content,fields}`) via its own view types — the internal `edited` override flag is
  **not** exposed; `analysis.content` is exposed whenever the md exists (AI or
  hand-written), fields only once AI-analyzed. A templated page (matched via `a.Templates()`) adds
  `analysis.regions[]` (`RegionView{id,label,rect,content}` — one per config box, label/rect from the
  config, content sliced from the region sections in `<PAGEID>.md`; tombstones excluded). Consumers talk to snorg only via
  `list`/`query`/`retrieve`; read-only.
- `internal/serve` — the built-in HTTP viewer (`serve` cmd): `Handler(a, views, flat)` builds a
  `net/http.ServeMux` over the assembled `[]*retrieve.NoteView` — `/` (note gallery: name +
  first-page thumbnail), `/note/{fid}` (that note's page gallery + a vanilla-JS lightbox that
  shows the page's transcription under the enlarged page), `/thumb/{fid}/{name}` (small
  rasterized-PNG thumbnail — `thumb.go` renders the SVG at ~400px via `oksvg`/`rasterx`, so it's
  handwriting-on-white and far fewer bytes than the vector SVG; memoized in-memory for the
  session) and `/svg/{fid}/{name}` (full page SVG via `archive.ReadSVG`, used for the enlarged
  view — the lightbox embeds it in an `<object>` so its links stay clickable, unlike an `<img>`).
  The grouped/flat difference lives behind a `layout` **seam** (`layout` interface with
  `render(w)` + `pageHref(fid,pid)`; `groupedLayout` note gallery vs `flatLayout` one page
  gallery — `--flat`): Handler picks one from `flat` in the **single** branch, so the routes stay
  branch-free and both galleries share one `{{define "pagegrid"}}` template over a `pageItem`
  tile (its `Caption` renders only when set — flat sets `<note> · <n>`, a note's own gallery leaves
  it empty). For the enlarged view `svglinks.go` transforms the served SVG: `rewriteViewerLinks`
  retargets each baked page link `xlink:href="…PID.svg"` → `target="_top" xlink:href="<pageHref>"`
  where the active layout supplies `pageHref` (grouped `/note/{fid}?page={pid}`, flat
  `/?page={pid}`); `rewriteViewerLinks` itself is mode-agnostic (takes the href closure). A tap
  thus reopens the right index (note page, or the flat `/`) whose `?page=` reader enlarges the
  target page, and
  `responsiveSVGRoot` gives the root a viewBox + `width/height="100%"` so it scales to the object
  box instead of rendering at native 1920x2560 and clipping (memoized). Both image routes carry
  `ETag` + `Cache-Control` (`writeAsset`, 304 on re-request) and are gated to pages in the served
  set so the viewer never exposes the whole archive.
  Self-contained HTML/CSS/JS via `html/template`, no other deps; read-only, nothing written to
  disk. Testable via `httptest` (no network). Package deps: `oksvg`/`rasterx` (thumbnail raster).
- `internal/config` — loads merged YAML config (provider creds + analysis prompts incl.
  `content.update_prompt` + `ingest.svg` toggles + `export.template` + `templates:` regions; `docs/config.md`). `Load(paths)`
  deep-merges files (later wins), defaults unset prompts and nil toggles (to true). A file may
  carry an `include:` list of other config paths (relative to that file's dir, `~` expanded):
  `loadInto` merges each include **over** the including file ("above includer"), recursively, with
  stack-based cycle detection — so the low→high order for a file is its body then its includes.
  Each `templates[].image` is resolved to an absolute path relative to the declaring file **before**
  merge (provenance survives the wholesale sequence overwrite); the specs are the raw
  `config.TemplateSpec`/`config.Box` (`snote.Rect`), bridged to `archive.TemplateSpec` by
  `pkg/snorg.Open` (config and archive stay mutually independent). Provider key
  resolution is a separate `ResolveAPIKey` (called by analyze, not `Load`, so export never runs it):
  literal `api_key` > `api_key_command` shell stdout > `OPENAI_API_KEY`. **Load does not enforce required fields** — each command validates its
  own section (analyze: `ValidateProvider`; export: non-empty `Export.Template`), so an export-only
  config needs no provider. The top-level `archive:` key is the default archive root when `-a` is
  absent; Load resolves it per-file by context like `include:`/`image:` paths (relative → against the
  declaring file's dir via `resolveArchivePath`/`resolvePath`, so `archive: .` in `<archive>/config.yaml`
  means that dir; absolute/`~` as-is). Uses `yaml.v3`. `cmd/snorg`'s root command
  builds the path list (`configPaths`: XDG user config `$XDG_CONFIG_HOME/snorg/config.yaml`
  first — the only auto-loaded file, `--no-user-config` skips it — then the `-c` files, so later
  layers override via the later-wins merge; the user config skipped when absent/a directory), loads
  once in the root's `Before` hook, and shares the result with the command via the `app` struct in
  `main.go`. `snorg.Resolve` loads the merged config in a **single** pass, then resolves the archive
  path (`-a` flag, CWD-relative; else the merged config's `archive:` key, already file-resolved by
  Load; `~` expanded via `ExpandHome`); a bare
  invocation with neither errors. An archive's own `config.yaml` is never auto-loaded (pass it with
  `-c`). The global `-a`/`--archive` flag is no longer `Required`, so urfave's natural subcommand
  dispatch still handles routing — no manual dispatch.
- `internal/export` — generic exporter (`export` cmd): renders the retrieved `*retrieve.Result`
  through one pongo2 template in a single pass (`Render(res, template)`). Marshals the whole Result to
  JSON then back (numbers via `UseNumber` so levels/page numbers render as ints) into the pongo2
  context **as-is** — a map with `archive` + `notes` — so templates bind to the `retrieve` shape
  verbatim (no bespoke key assignment, no render-time enrichment) and can span notes. pongo2 is isolated here;
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
  a **single canonical rasterization** (`geom.go`: `canonicalSVG` renders every `<path>` forced black,
  overlays/background dropped — immune to recolor `fill`, background mode, links/nav/format; `oksvg`/`rasterx`),
  thresholded to a 1-bit ink `mask` whose `mask.hash` is `analysis.source_hash`. That one raster also
  feeds the per-box region hashes and the LLM crops, so a `Page` call rasterizes **exactly once**
  (including on a skip). Skips when the hash matches (unless `--force`); otherwise transcribes —
  through `Spec.Update` + the previous **AI base** (`archive.ReadAnalysisBase`; user edits never reach
  the LLM) when one exists (minimal-diff updates) — merges the result with any user edits
  (`archive.MergeAnalysis`), crops title/link rects via a `Transcriber`, runs custom `Spec.Fields` via
  a `Generator` (text→text over the **effective** content, **no image** — cheaper).
  **Template regions**: when the page's `background_hash` matches `a.Templates()`, the page takes the
  region path (`analyzeRegions`) instead of whole-page content — `mask.regionHash` hashes a box's rect
  (`snote.Rect`) cropped out of the page mask (pixel-space, so a stroke crossing the edge counts only
  its in-box pixels — editing one box never perturbs a neighbour), so a box is skipped when its
  fingerprint is unchanged and re-cropped/transcribed otherwise; the id-keyed region document is 3-way
  merged into `<PAGEID>.md` via `archive.MergeAnalysis`.
  The page-level skip is **template-aware** (`regionsCurrent`): an unchanged page skips a
  templated page only when every box fingerprint also matches, so a moved/added box rect re-triggers.
  Both seams are one openai-go client (endpoint/model/key from config). Writes `<PAGEID>.md` (region
  sections for a templated page, else content) + `<PAGEID>.json`; returns `skipped|analyzed|updated|conflict` (conflict ≠ error; the batch continues).
  Has external deps; the seams keep it testable without a network.
- `internal/edit` — `analyze-edit` orchestration: `buffer.go` serializes the page to one editable
  document (per-region name header — title/link markers keyed by kind+index, plus, for a templated
  page, `<!-- region <id> (<label>) -->` sections keyed by string id, which are the body — no
  `<!-- content -->` marker — while a normal page has `<!-- content -->` + content parsed verbatim
  after the first content marker) and opens it in the editor (`sh -c` so
  `$EDITOR` may carry args, terminal inherited, temp copy — an aborted editor changes nothing); the
  effective md — the content, or the region sections (rebuilt in canonical order by `regionLayout`)
  assembled — flows through `archive.WriteAnalysisEdit`, and each changed name becomes an `Edited`
  override via `archive.WritePage`. Returns the content outcome (`unchanged|edited|reverted`) plus the
  count of changed names; a malformed header (wrong marker count, missing content marker) errors and
  writes nothing.
- `internal/textmerge` — diff/patch + 3-way-merge plumbing, pure Go, no PATH tool: `Diff`/`Unapply`
  (line diff + reverse-apply via `github.com/njchilds90/go-diffpatch`, serialized to/from a normal
  unified diff via `github.com/sourcegraph/go-diff` — `patchToFileDiff`/`fileDiffToPatch` convert at
  the boundary, so the stored `.md.diff` is a readable `--- /+++ /@@ /+/-` diff, not a struct dump;
  `ConvertLegacyDiff` upgrades a pre-unified JSON-patch sidecar to that form for `migrate`),
  `Merge` (3-way merge, git-standard `<<<<<<< edited`/`>>>>>>> reanalyzed` conflict markers) over
  `github.com/epiclabs-io/diff3` (`Diff3MergeWithOptions`, Myers, false conflicts excluded); pure
  text-in/text-out, byte-faithful (normalization is the archive's job).
- `internal/ingest` — orchestrator. Re-ingest does an **incremental reconcile** per `FILE_ID`:
  prunes removed pages (all their `<PAGEID>.*`, `.md` included), writes only changed files,
  **preserves a page's `analysis`** across re-write (per-region transcriptions carried by exact
  rect match — moved regions drop theirs), and **preserves other `<PAGEID>.*` artifacts** — never
  a full rebuild. Note: page reorder rewrites neighbor SVGs (baked nav zones follow the order).

## `.note` format gotchas (full notes in docs/supernote-format.md)

- Page position is **0-based**; footer index keys are **1-based** → footer page N == position N-1 (sntool resolves this internally).
- Page association comes from footer KEYS (`TITLE_`/`KEYWORD_`/`LINKO_` + 4-digit page), **not** from
  structured records; `KEYWORDPAGE` is unreliable (yields -1 on the sample).
- Keywords are invisible page-level metadata (no handwriting/region); text is in the `KEYWORD` field.
- Pages render **1920×2560**; all `*RECT` values are in that pixel space (crop = `x,y,x+w,y+h`).
- Star = per-page `FIVESTAR`. Link **kind** is `LINKTYPE`: `1`=note (another `.note`; internal iff
  `LINKFILEID == FILE_ID`), `2`=file (non-note document, pdf/epub), `4`=web URL — anything else is
  `unknown`. sntool exposes it as `Link.Kind()`; snorg stores it as `LinkDoc.kind`/`retrieve` `kind`.
  Note links carry a target page = `PAGEID` (stable id, stored) not `OBJPAGE` (volatile, dropped) and
  `LINKFILEID`; file/web links carry `Target` = decoded `LINKFILE` (device path / URL) — their
  `LINKFILEID`/`PAGEID` are the literal `none`, which the adapter normalizes to empty, so
  `target_page_id`/`target_file_id` are **omitted** (note-link-only). A **file** link also stores `OBJPAGE` as `target_doc_page`
  (the page within the linked document; `Link.TargetDocPage()`) — the sole page indicator when there's no
  `PAGEID`; note links drop it. Link `name` (`Link.Name()`) = the URL (web), file
  basename with ext (file), or note name (basename without `.note`). Only note + web links bake a
  clickable SVG anchor; file/unknown links are metadata-only (their path isn't in the archive).
