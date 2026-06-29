# Architecture

## CLI

```
snorg ingest   <file.note> <archive-path>
snorg list     <archive-path>
snorg retrieve <archive-path> <FILE_ID>
```
Archive path is a positional argument (never hardcoded). Re-ingest reconciles the
note's directory in place (see Archive layout); it is the update path. `list` and
`retrieve` are the read side — the platform-agnostic interface external tools build
on (see [retrieval.md](retrieval.md)).

## Archive layout (plaintext contract)

```
<archive>/<FILE_ID>/
    note.json          # file metadata + ordered page placement (id, number, starred)
    <PAGEID>.json      # per page: titles(rect,level), keywords(text), links
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
- `internal/archive` — owns the on-disk layout; `doc.go` is the JSON serialization boundary;
  `write.go`/`Write` reconciles a note's directory in place, `read.go` are the layout-aware
  read accessors (`List`/`ReadNote`/`ReadPage`/`SVGRel`).
- `internal/retrieve` — platform-agnostic read contract: assembles `note.json` + each
  `<PAGEID>.json` into one denormalized `NoteView` (the stable JSON consumers depend on).
- `internal/ingest` — orchestrator: `Source.Read` → render SVGs → `Archive.Write`.

## Extension points

New retrieval/export commands add cases in `cmd/snorg` and read via `archive`
accessors, projecting into a `retrieve` view. Vision-LLM analysis enriches the `Doc`
JSON schemas (and the views) with new fields (free to add — no backcompat). A native
parser is a new `snote.Source` implementation.
