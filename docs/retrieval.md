# Retrieval interface

The read side of snorg: a **platform-agnostic** contract an external tool uses to
turn the archive into a human-readable form. It makes no assumptions about the
consumer (org-mode, Markdown, a web view, …); the consumer talks to snorg only
through the CLI process boundary and stable JSON below.

## Commands

```
snorg [-a <archive-path>] list                  # one FILE_ID per line
snorg [-a <archive-path>] list -l               # <FILE_ID>\t<note name>   (browse-only)
snorg [-a <archive-path>] list keywords|tags    # distinct labels (-l adds a count)
snorg [-a <archive-path>] query <expr>          # one PAGEID per line
snorg [-a <archive-path>] query -l <expr>       # annotated page lines      (browse-only)
snorg [-a <archive-path>] retrieve [PAGEID ...] # {archive, notes} as indented JSON
```

The bare forms are the **pipe contract**: one id per line, nothing else. The
`-l`/`--long` forms are for a human (or a fuzzy finder) and are **not** pipe-safe —
`query -l` emits tab-separated columns
`<PAGEID>\t<note>\tp<page#>\t<*?>\t<headings>\t#<keyword>…\t@<tag>…`, with the PAGEID
kept first so `cut -f1` recovers the pipe contract from it.

A PAGEID addresses exactly one page in exactly one note, archive-wide — it is minted
by the device, and snorg keeps the invariant true when a page is moved between notes
there (see "Page moves" in [architecture.md](architecture.md)) — so a consumer can
treat it as a primary key and let `retrieve` resolve the owning note.

`list` enumerates notes (or, with the `keywords`/`tags` subcommands, the archive's
distinct labels); `query` enumerates pages through a boolean **expression** — terms joined by
`AND`/`OR`/`NOT` and grouped with parentheses:

```
starred AND (ctime:2026-04-04..2026-09-12 OR content:"some thing")
```

A term is either a standalone word — `all`, `starred`, `unanalyzed`, `templated`
(the page is drawn on a configured template, so `NOT templated` is the free-form
pages) — or a field with an operator and a value: `note`, `keyword` (device
keywords), `tag` (snorg-managed tags, the page's own plus the ones inherited from
its note), `content` (the page's transcribed `<PAGEID>.md`), `region[<box-id>]`
(the text of one template box in that same file; without `[<box-id>]`, any box) and
the three times — `ctime:<spec>` (created: the day in the PAGEID's leading digits),
`mtime:<spec>` (`modified_at`: anything about the page changed in the archive —
re-ingest, restyle, analyze, analyze-edit, tag) and `dtime:<spec>`
(`device_modified_at`: the page changed on the device — new or moved page, handwriting,
titles/links/keywords, star, background) — where spec is
`today`/`yesterday`/`YYYY-MM-DD`/`FROM..TO` with open ends, matched on the local day. The operator picks how
the value is matched: `:` substring (case-insensitive), `~` regexp, `=` exact. A
value must follow its operator immediately; quote it (`"…"` or `'…'`) if it holds
spaces or parens.

`query` also reads PAGEIDs from stdin when piped, restricting its expression to
that set, so selections still intersect across a pipe: `query keyword:foo | query
mtime:today` — the same thing as `query 'keyword:foo AND mtime:today'`.
`retrieve` takes PAGEIDs — as
arguments, or one-per-line on stdin when none are given, so `query` pipes
straight into it — and returns a JSON **object** `{archive, notes}`: `archive`
is the **absolute archive root**, and `notes` is an **array of `NoteView`s** —
the selected pages grouped per owning note (archive `list` order), each view
carrying the full note metadata but only the requested pages, in placement
order. Emitting `archive` makes the payload self-contained: the pages' `svg`
paths are archive-relative, so a consumer resolves them against `archive`
without any out-of-band knowledge of where the archive lives. A `NoteView` is a
denormalized join of `note.json` and the selected `<PAGEID>.json` files, so the
consumer never needs to know the on-disk file split. A whole note is
`query note=<FILE_ID> | retrieve`; an unknown PAGEID is an error.

## Retrieve JSON

```json
{
  "archive": "/home/you/notes/sn",
  "notes": [{
    "file_id": "F...", "signature": "...", "device": "...", "source": "note.note",
    "tags": ["semester-2"],
    "pages": [{
      "number": 1, "page_id": "P...", "starred": false,
      "svg": "F.../P....svg",
      "modified_at": "2026-10-02T08:15:00Z", "device_modified_at": "2026-09-30T19:02:11Z",
      "tags": ["exam", "important"],
      "titles":   [{"rect": {"x":0,"y":0,"w":0,"h":0}, "level": 1,
                    "analysis": {"name": "Chapter 1"}}],
      "keywords": [{"text": "fizyka"}],
      "links":    [{"rect": {"x":0,"y":0,"w":0,"h":0}, "kind": "note", "target_page_id": "P...",
                    "target_file_id": "F...", "name": "linked-note", "internal": true,
                    "analysis": {"name": "see also"}}],
      "analysis": {"content": "# Chapter 1\n\n...", "fields": {"description": "..."},
                   "regions": [{"id": "title", "label": "Title",
                                "rect": {"x":0,"y":0,"w":1920,"h":384}, "content": "..."}]}
    }]
  }]
}
```

- `archive` is the **absolute archive root** the pages' `svg` paths resolve against.
- `pages` are in placement order (1-based `number`).
- `tags` are snorg-managed labels (from the `tag` command), sorted and de-duplicated;
  omitted when empty. Distinct from device `keywords` (read-only, set on the Supernote).
  A note carries its own `tags` (set with `tag -n`, stored in `note.json` only) and every
  page of that note **inherits** them: a page's `tags` is the union of its own and its
  note's, so an inherited label is indistinguishable there — read `notes[].tags` to tell
  which labels are note-wide.
- `svg` is **relative to the archive root**; resolve it as `join(archive, svg)`.
  Per-page paths stay relative (portable); the one absolute root travels in `archive`.
- `kind` classifies the link: `note` (another `.note`), `file` (non-note document on
  device), `web` (URL), or `unknown`. `target_page_id`/`target_file_id` are note-link-only
  (omitted otherwise); file/web links carry the destination (device path or URL) in
  `target` instead, and a file link adds `target_doc_page` — the page within the linked
  document.
- `internal` is derived: `target_file_id == file_id` (a note link within this note).
- link `name` is the human label decoded from `LINKFILE`: the full URL (web), the file
  basename with extension (file), or the note name without `.note` (note); `""` when unknown.
- derived data sits under `analysis` keys: per-title/per-link `analysis.name`
  (region transcriptions) and the page-level `analysis` — `content` (the Markdown
  transcription, assembled from the `<PAGEID>.md` sidecar) and `fields` (custom
  configured outputs). `content` is present whenever the page has a transcription —
  AI-produced, user-edited or written entirely by hand via `analyze-edit` — while
  region names and `fields` exist only once the page was AI-analyzed; everything is
  absent before that. After a conflicted re-analysis, `content` carries standard
  merge conflict markers until the user resolves them. The view mirrors the on-disk
  structure, so export templates and external consumers see one shape.
- `analysis.regions` is present on a **templated** page (its background matched an
  entry in the merged config's `templates:` section): one entry per config box — `id`, `label`, `rect`
  (pixel-space `{x,y,w,h}`, same as a title/link rect) resolved from the config, and `content` (the box's
  transcription). A templated page carries its transcription here, not in
  `analysis.content`. See `docs/templates.md`. Fields may be
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
    data = json(`snorg -a <archive> query note=id | snorg -a <archive> retrieve`)
    view = data.notes[0]                                   # archive root is data.archive
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
built on exactly this pattern (`query note=` + `retrieve` + `export` per note).
