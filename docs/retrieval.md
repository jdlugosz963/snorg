# Retrieval interface

The read side of snorg: a **platform-agnostic** contract an external tool uses to
turn the archive into a human-readable form. It makes no assumptions about the
consumer (org-mode, Markdown, a web view, …); the consumer talks to snorg only
through the CLI process boundary and stable JSON below.

## Commands

```
snorg list     <archive-path>            # one FILE_ID per line
snorg retrieve <archive-path> <FILE_ID>  # one assembled note as indented JSON
```

`list` enumerates notes; `retrieve` returns a single `NoteView` — a denormalized
join of `note.json` and every `<PAGEID>.json`, so the consumer never needs to know
the on-disk file split.

## NoteView JSON

```json
{
  "file_id": "F...", "signature": "...", "device": "...", "source": "note.note",
  "pages": [{
    "number": 1, "page_id": "P...", "starred": false,
    "svg": "F.../P....svg",
    "titles":   [{"rect": {"x":0,"y":0,"w":0,"h":0}, "level": 1, "text": ""}],
    "keywords": [{"text": "fizyka"}],
    "links":    [{"rect": {"x":0,"y":0,"w":0,"h":0}, "target_page_id": "P...",
                  "target_file_id": "F...", "name": "linked-note", "internal": true}]
  }]
}
```

- `pages` are in placement order (1-based `number`).
- `svg` is **relative to the archive root**; resolve it as `join(<archive-path>, svg)`.
  This keeps the contract portable (no machine-specific absolute paths).
- `internal` is derived: `target_file_id == file_id` (link stays within this note).
- link `name` is the target note's human name, decoded from the `.note`'s `LINKFILE`
  (base64 device path → basename without extension); `""` when unknown.
- `text` on titles is empty until the future vision-LLM phase fills it. Fields may be
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
for id in `snorg list <archive>`:
    view = json(`snorg retrieve <archive> id`)
    doc  = open_or_create(outdir/(id + ".ext"))

    root = find_managed_root(doc, key = id) or create_managed_root(doc, key = id,
                                                                   title = view.source or id)
    # Each managed section below is reset (regenerated) on every run.
    reset_section(root, "Pages",    render_pages(view))     # per page: heading + link(join(archive, p.svg))
    reset_section(root, "Titles",   render_titles(view))    # rect/level now, text once LLM fills it
    reset_section(root, "Keywords", render_keywords(view))
    reset_section(root, "Links",    render_links(view))     # internal vs external (target_file_id/target_page_id)
    # future: reset_section(root, "Analysis", render_analysis(view))
    write(doc)                                              # user headings outside managed sections untouched
```

## Status

The first consumer will be a separate repo (planned git submodule) holding an Elisp
org-mode generator. It is intentionally not built yet — this interface is the
prerequisite that unblocks it.
