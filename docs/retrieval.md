# Retrieval interface

The read side of snorg: a **platform-agnostic** contract an external tool uses to
turn the archive into a human-readable form. It makes no assumptions about the
consumer (org-mode, Markdown, a web view, …); the consumer talks to snorg only
through the CLI process boundary and stable JSON below.

## Commands

```
snorg -a <archive-path> list                  # one FILE_ID per line
snorg -a <archive-path> query <filter> [arg]  # one PAGEID per line
snorg -a <archive-path> retrieve [PAGEID ...] # assembled notes as indented JSON
```

`list` enumerates notes; `query` enumerates pages (`all`, `note <FILE_ID>`,
`unanalyzed`, `keyword <regexp>`, `starred`, `date <spec>` where the day is the
PAGEID's leading 8 digits and spec is `today`/`yesterday`/`YYYY-MM-DD`/`FROM..TO`
with open ends). `query` also reads PAGEIDs from stdin when piped, restricting its
filter to that set, so filters intersect: `query keyword foo | query date today`.
`retrieve` takes PAGEIDs — as
arguments, or one-per-line on stdin when none are given, so `query` pipes
straight into it — and returns a JSON **array of `NoteView`s**: the selected
pages grouped per owning note (archive `list` order), each view carrying the
full note metadata but only the requested pages, in placement order. A
`NoteView` is a denormalized join of `note.json` and the selected `<PAGEID>.json`
files, so the consumer never needs to know the on-disk file split. A whole note
is `query note <FILE_ID> | retrieve`; an unknown PAGEID is an error.

## NoteView JSON

```json
[{
  "file_id": "F...", "signature": "...", "device": "...", "source": "note.note",
  "pages": [{
    "number": 1, "page_id": "P...", "starred": false,
    "svg": "F.../P....svg",
    "titles":   [{"rect": {"x":0,"y":0,"w":0,"h":0}, "level": 1,
                  "analysis": {"name": "Chapter 1"}}],
    "keywords": [{"text": "fizyka"}],
    "links":    [{"rect": {"x":0,"y":0,"w":0,"h":0}, "target_page_id": "P...",
                  "target_file_id": "F...", "name": "linked-note", "internal": true,
                  "analysis": {"name": "see also"}}],
    "analysis": {"content": "# Chapter 1\n\n...", "fields": {"description": "..."}}
  }]
}]
```

- `pages` are in placement order (1-based `number`).
- `svg` is **relative to the archive root**; resolve it as `join(<archive-path>, svg)`.
  This keeps the contract portable (no machine-specific absolute paths).
- `internal` is derived: `target_file_id == file_id` (link stays within this note).
- link `name` is the target note's human name, decoded from the `.note`'s `LINKFILE`
  (base64 device path → basename without extension); `""` when unknown.
- derived data sits under `analysis` keys: per-title/per-link `analysis.name`
  (region transcriptions) and the page-level `analysis` — `content` (the Markdown
  transcription, assembled from the `<PAGEID>.md` sidecar) and `fields` (custom
  configured outputs). `content` is present whenever the page has a transcription —
  AI-produced, user-edited or written entirely by hand via `analyze-edit` — while
  region names and `fields` exist only once the page was AI-analyzed; everything is
  absent before that. After a conflicted re-analysis, `content` carries standard
  merge conflict markers until the user resolves them. The view mirrors the on-disk
  structure, so export templates and external consumers see one shape. Fields may be
  added freely over time — there is no backward-compatibility guarantee, so consumers
  should ignore unknown fields rather than pin a shape.

## Consuming it (intended pattern)

A builder generates one output file per note and re-runs idempotently. The pattern
is **managed regions**: a region owned by the generator is fully regenerated each
run, while user-authored content around it is preserved. Reconciliation,
idempotency and the managed-region convention are entirely **consumer-side** — snorg
stays read-only.

Pseudocode (illustrative; an org-mode generator, but nothing here is org-specific):

```
for id in `snorg -a <archive> list`:
    view = json(`snorg -a <archive> query note id | snorg -a <archive> retrieve`)[0]
    doc  = open_or_create(outdir/(id + ".ext"))

    root = find_managed_root(doc, key = id) or create_managed_root(doc, key = id,
                                                                   title = view.source or id)
    # Each managed section below is reset (regenerated) on every run.
    reset_section(root, "Pages",    render_pages(view))     # per page: heading + link(join(archive, p.svg))
    reset_section(root, "Titles",   render_titles(view))    # rect/level + analysis.name once analyzed
    reset_section(root, "Keywords", render_keywords(view))
    reset_section(root, "Links",    render_links(view))     # internal vs external (target_file_id/target_page_id)
    reset_section(root, "Content",  render_content(view))   # page.analysis.content / .fields
    write(doc)                                              # user headings outside managed sections untouched
```

## Status

The first consumer is `examples/emacs/snorg.el`, an Elisp org/denote generator
built on exactly this pattern (`query note` + `retrieve` + `export` per note).
