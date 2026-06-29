# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

SNORG (supernote-organizer) ingests Supernote `.note` files into a plaintext,
machine-readable, VCS-friendly *archive*, so notes can later be retrieved and exported.
Read `docs/principles.md` (project rules), `docs/architecture.md` (modules/CLI),
`docs/supernote-format.md` (`.note` format + tooling) before substantial work.

## Project rules

- Go, stdlib only. The `.note` binary format is handled by shelling out to
  `supernote-tool` (supernotelib) — not parsed natively (yet).
- No backward compatibility: when something must change, rewrite it cleanly rather
  than preserving legacy.
- All operational data is plaintext; all docs in English, maximally concise.

## Commands

- `go build ./... && go vet ./... && gofmt -l .` — build, vet, format check (gofmt output must be empty)
- `go test ./...` — all tests
- `go test ./internal/snote/sntool` — fast unit tests (footer-key parsing)
- `go test -run TestIngestSampleNote ./internal/ingest` — e2e ingest; **slow (~1 min**, renders SVGs via supernote-tool) and skips if `supernote-tool` is not on PATH
- `go run ./cmd/snorg ingest <file.note> <archive-path>` — register a note into an archive
- `go run ./cmd/snorg list <archive-path>` — list FILE_IDs (one per line)
- `go run ./cmd/snorg retrieve <archive-path> <FILE_ID>` — assembled note as JSON (the read interface)

## Architecture

Flow: `cmd/snorg` → `internal/ingest` orchestrates `snote.Source.Read` → render SVGs → `archive.Write`.

- `internal/snote` — device-agnostic domain model (`Note`/`Page`/`Title`/`Keyword`/`Link`)
  and the `Source` interface, the **seam isolating the format**. A native-Go parser would
  be a new `Source` impl, leaving callers untouched.
- `internal/snote/sntool` — `Source` impl shelling to `supernote-tool` (`analyze` + `convert -t svg`);
  `footer.go` is the risky part (page association, see gotchas).
- `internal/archive` — owns the on-disk layout `<archive>/<FILE_ID>/{note.json,<PAGEID>.json,<PAGEID>.svg}`;
  `doc.go` is the JSON serialization boundary (the stable plaintext contract; add fields freely);
  `read.go` are layout-aware read accessors (`List`/`ReadNote`/`ReadPage`/`SVGRel`).
- `internal/retrieve` — **platform-agnostic read contract** (docs/retrieval.md): assembles
  `note.json` + `<PAGEID>.json` into one `NoteView` JSON (svg paths archive-relative). Consumers
  (e.g. a future org-mode generator) talk to snorg only via `list`/`retrieve`; snorg stays read-only.
- `internal/ingest` — orchestrator. Re-ingest does an **incremental reconcile** per `FILE_ID`:
  prunes removed pages (all their `<PAGEID>.*`), writes only changed files, and **preserves other
  `<PAGEID>.*` artifacts** (future per-page LLM analyses) — never a full rebuild.

## `.note` format gotchas (full notes in docs/supernote-format.md)

- `supernote-tool convert -n` is **0-indexed**; footer index keys are **1-based** → footer page N == `-n (N-1)`.
- Page association comes from footer KEYS (`TITLE_`/`KEYWORD_`/`LINKO_` + 4-digit page), **not** from
  structured records; `KEYWORDPAGE` is unreliable (yields -1 on the sample).
- Keywords are invisible page-level metadata (no handwriting/region); text is in the `KEYWORD` field.
- Pages render **1920×2560**; all `*RECT` values are in that pixel space (crop = `x,y,x+w,y+h`).
- Star = per-page `FIVESTAR`; a link is internal iff `LINKFILEID == FILE_ID`; `LINKFILE` is a base64 path.
  Link target page = `PAGEID` (stable id, stored) not `OBJPAGE` (volatile number, dropped).
  Link `name` = base64-decoded `LINKFILE` basename without ext (target note's human name).
