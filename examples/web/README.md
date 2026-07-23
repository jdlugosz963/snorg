# examples/web — static HTML site export

Turn a selection of pages into a self-contained folder you can serve over HTTP:
an `index.html` listing the notes, one `<FILE_ID>.html` per note (page SVGs shown
inline, the analysis rendered as HTML), and the SVG assets copied alongside.

## Use

```sh
snorg -a <archive> query <filter> | examples/web/export.sh <archive> <dest>
# e.g. everything:
snorg -a ~/notes/archive query all | examples/web/export.sh ~/notes/archive /tmp/site
# or a subset (filters intersect):
snorg -a ~/notes/archive query starred | examples/web/export.sh ~/notes/archive /tmp/site
python3 -m http.server -d /tmp/site   # then open http://localhost:8000
```

`export.sh` reads PAGEIDs on stdin and, for that same set, runs `snorg export`
with the two configs beside it:

- **`index.yaml`** → `<dest>/index.html` — one pass over every selected note, each
  linking to its `<FILE_ID>.html`.
- **`note.yaml`** → `<dest>/<FILE_ID>.html` — run once per selected note (via
  `query note <FILE_ID>`, which intersects the piped PAGEIDs with the note's
  pages). It also copies each selected page's SVG to
  `<dest>/<FILE_ID>/<PAGEID>.svg` so the inline `<img>` resolves. No JSON is copied.

The destination must be empty; a non-empty one prompts before it is wiped.

## Requirements

- `snorg` and `pandoc` on PATH — the `note.yaml` template uses the `html` filter
  (Markdown → HTML via pandoc).

## Env knobs

- `SNORG` — snorg binary to use (default: `snorg` on PATH).
- `FORCE=1` or `-y` — wipe a non-empty `<dest>` without prompting.

## Caveat

An internal note link whose target note was **not** part of the export set yields
a dangling `<FILE_ID>.html` link. Fine for a snapshot; export the whole set
(`query all`) if you want every internal link to resolve.
