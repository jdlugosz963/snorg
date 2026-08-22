# Template regions

A Supernote page drawn on a known background template has implicit structure — a
title box, a notes box, a figure box. Template regions teach the archive that
structure: a template is a **schema**, its boxes are **fields**, and the page's
**background image is the schema selector**. When a page's background matches a
template, `analyze` transcribes each `analyze: true` box separately into the page's
`<PAGEID>.md` sidecar as id-keyed sections (in place of the whole-page free-form
content — a page is either one or the other), and `retrieve`/`export` surface the
boxes as structured entries. Pages that match no template are unaffected and keep
the ordinary free-form content.

## Where templates live

The template list is a `templates:` section of the **merged config** (see
`docs/config.md`) — it layers, deep-merges and can be pulled in via `include:` like
any other section, and needs no provider credentials to read. The natural home is
a per-archive config you pass with `-c <archive>/config.yaml`, but the shared user
config works too.

The background **images** are ordinary files on disk; each `image:` path is
resolved relative to the config file that declared it (`~` expanded). A common
layout keeps them beside the archive config:

```
<archive>/config.yaml          # holds the templates: section (pass with -c)
<archive>/templates/*.png      # device-form background images (image: templates/foo.png)
<archive>/<FILE_ID>/
    <PAGEID>.md                # a templated page holds its id-keyed region sections here
    <PAGEID>.md.diff           # user-edit diff, base→md, exists iff edited
```

A templated page reuses the same `<PAGEID>.md[.diff]` pair as a normal page; only
the *content* differs (region sections vs. free-form prose), so there is one
transcription file and one edit-diff invariant, whichever form the page takes.

## Detection: the background hash

Every page stamps `background_hash` in its `<PAGEID>.json` — the `sha256` of the
**decoded** background image — at ingest, under every `ingest.svg.background`
mode. A template is matched to a page by that hash.

The Supernote device stores a page template as an **8-bit grayscale PNG** and
re-renders it byte-identically every time, so the hash is stable across ingests —
a sound single-value selector. Register the **device-form grayscale** image: the
selector is the `sha256` of the referenced image file's bytes, which equals the
page's `background_hash` (and the archive's content-addressed
`<FILE_ID>/backgrounds/<sha256>.png` filename). A *re-encoded* copy — e.g. an
sRGB export from a PDF viewer — hashes differently and will not match. The
simplest way to obtain the right file: ingest a note, then point `image:` at its
`<FILE_ID>/backgrounds/<hash>.png` (or copy that file next to your config).

## The `templates:` section

```yaml
templates:
  - image: templates/prawo_jazdy.png   # device-form grayscale PNG; path relative to this config file
    boxes:
      - id: title                 # stable, author-assigned identity; never changes
        label: "Title"            # human-readable; free to rename anytime
        rect: {x: 0, y: 0, w: 1920, h: 384}   # pixel space (1920x2560), same as a title/link rect
        analyze: true             # send this box to the vision LLM
        prompt: "Transcribe the title."   # optional, per-box; falls back to the page content prompt
      - id: notes
        label: "Notes"
        rect: {x: 0, y: 384, w: 1920, h: 2176}
        analyze: true
```

`rect` is `{x, y, w, h}` in **page pixel space** — the page is 1920×2560, origin
top-left, y-down — exactly the shape and units of the title/link rects you see in
a page's `<PAGEID>.json` (so you can copy a device rect straight in).

- **`id` and `rect` are deliberately separate.** `id` is identity: it never
  changes and it keys the stored transcription. `rect` drives change-detection:
  moving it re-triggers analysis of that box (the strokes it covers changed),
  while the `id` — and any hand-edit — survives.
- **`label`, `rect` and `prompt` are never duplicated into the sidecar.** They are
  resolved from this file at read time, so renaming a label rewrites nothing.
- A box with `analyze: false` is a structural region with no transcription (its
  `rect` is still exposed, e.g. to crop a figure at export time).

## Region analysis

For each `analyze: true` box, `analyze` fingerprints the box by cropping its rect
out of the page's single **canonical black-on-white rasterization** (all
handwriting forced black, background/nav/link overlays dropped) and hashing those
pixels — styling-independent, the same invariant as the page fingerprint. Because
the crop is in pixel space, a stroke crossing the box edge contributes only its
in-box pixels, so editing ink in one box never perturbs a neighbour. A box whose
fingerprint is unchanged is skipped without an LLM call; a changed box is cropped
from that same rasterization and re-transcribed with the box's prompt. So editing
one box re-analyzes only that box. The result is written to `<PAGEID>.md` as
id-keyed sections:

```
<!-- region title (Title) -->
Prawo jazdy — notatki

<!-- region notes (Notes) -->
...the notes text...
```

The label in the marker is informational (for the human reading the file/diff);
snorg keys a section by its **id** only.

## Edits and durability

The region-section `<PAGEID>.md` goes through the same 3-way merge as page
content: the divergence from the AI base is stored in `<PAGEID>.md.diff`, so hand
corrections survive re-analysis (an overlapping change leaves git-standard
conflict markers, resolved with another `analyze-edit`). `analyze-edit` opens the
region sections in the editor alongside any title/link header (a templated page has
no `<!-- content -->` section — the regions are the body).

A section whose `id` disappears from the config is **not deleted** — it is
tombstoned at the end of the md (kept editable, ignored by export), so text is
never silently lost.

## Export / retrieve

`retrieve` exposes a templated page's boxes under `analysis.regions[]`, one entry
per current config box (tombstones excluded):

```json
"analysis": {
  "content": "",
  "regions": [
    {"id": "title", "label": "Title", "rect": {"x":0,"y":0,"w":1920,"h":384}, "content": "..."},
    {"id": "notes", "label": "Notes", "rect": {"x":0,"y":384,"w":1920,"h":2176}, "content": "..."}
  ]
}
```

`export` needs no special handling — the whole `retrieve` object is the template
context, so a pongo2 template reaches
`notes[].pages[].analysis.regions[]` directly. `label` and `rect` let a template
build a per-region cropped view (e.g. an SVG `clipPath` over `{{ archive }}/{{
p.svg }}`).

## Caveats

- `query content <regexp>` matches the `<PAGEID>.md`, which for a templated page is
  the region-section document, so a templated page **is** content-searchable (the
  match text includes the `<!-- region … -->` marker lines).
- On a pre-existing archive, `background_hash` is populated by a re-ingest (cheap,
  idempotent); `migrate` does not backfill it.
- A page analyzed as whole-page content *before* a matching template is added
  re-analyzes into the region-section form on the next `analyze`: the prose `.md` is
  replaced by the regions (there is one `.md` per page). AI-only prose is simply
  re-transcribed into the boxes; **user-edited** prose surfaces as a normal 3-way
  conflict in the merged `.md` (resolve with `analyze-edit`) rather than lingering
  invisibly. The regions are the authoritative transcription for a templated page.
  Run `analyze` before `analyze-edit` after adding a template to an
  already-transcribed page: `analyze-edit` parses the `.md` as region sections, so
  opening it first shows empty boxes rather than the old prose (which is not lost —
  the next `analyze` recovers it from the AI base as a conflict).
