# Configuration

`snorg` commands are driven by YAML config files. Pass one or more with the
repeatable global `-c` flag (before the archive path); later files override
earlier ones (deep-merge). Split secrets from committed config:

```sh
snorg -c secrets.yaml -c analysis.yaml -a <archive> analyze <PAGEID>
```

The root command loads the merged config once and hands it to every command.
Each command reads (and validates) only its own sections, so any section can
stand alone in its own file: `ingest` → `ingest`; `analyze` → `provider` +
`analysis`; `export` → `export`. See `examples/config.yaml` (annotated schema
with a Markdown export template) and `examples/emacs/orgmode.yaml` (a complete
org-mode exporter).

## Resolution order

The root loads, in increasing precedence:

1. `<archive-path>/config.yaml` — the archive's own default config, if the file
   exists (self-contained per-archive settings). Pass `--no-archive-config` to
   ignore it.
2. each `-c` file, left to right.

Later sources win via the deep-merge below, so `-c` files override the archive
config per key. A missing archive `config.yaml` is not an error; a malformed one is.

## Schema

```yaml
provider:
  endpoint: https://openrouter.ai/api/v1   # OpenAI-compatible base URL (required by analyze)
  api_key: sk-or-...                        # required; if empty, api_key_command then $OPENAI_API_KEY
  api_key_command: "pass show openai"       # optional; stdout (trimmed) is the key when api_key is empty
  model: openai/gpt-4o                      # required; one global model for every task

analysis:
  content:                                  # whole-page transcription (vision) -> <PAGEID>.md
    prompt: "..."                           # optional; built-in default transcribes to Markdown
    update_prompt: "..."                    # optional; used instead of prompt when the page was
                                            # transcribed before — the previous transcription is
                                            # appended, steering the model to a minimal diff
  titles:                                   # per-title crop transcription (vision)
    prompt: "..."
  links:                                    # per-link crop transcription (vision)
    prompt: "..."
  fields:                                   # optional custom outputs (text)
    description:
      prompt: "Write one very short sentence saying what this page is about."

ingest:
  svg:                                      # SVG rewrites applied on ingest
    links: true                             # bake note links as clickable overlays (default true)
    navigation: true                        # tap left/right half -> previous/next page (default true)
    format: true                            # diff-friendly multiline reflow (default true)
    background: extract                      # extract | inline | blank | remove (default extract):
                                            #   extract = lift inline base64 into backgrounds/ (visible)
                                            #   inline  = leave the base64 image in place (visible)
                                            #   blank   = replace with a white background
                                            #   remove  = drop the background (transparent)
    colors:                                 # optional: remap the four default pen shades
      black: "#000000"                       #   (CSS name or hex; any unset shade keeps its default:
      darkgray: "#9d9d9d"                    #    black #000000, darkgray #9d9d9d, gray #c9c9c9,
      gray: "#c9c9c9"                        #    white #fefefe). Empty = no recolor.
      white: "#fefefe"

export:
  template: |                               # single pongo2 (Jinja2-style) template
    {% for note in notes %}
    {% for page in note.pages %}* Page {{ page.number }}
    {{ page.analysis.content }}
    {% endfor %}
    {% endfor %}
```

Changing any `ingest.svg` option changes SVG bytes, so the next ingest rewrites
every page SVG once — harmless, expected churn. `links`/`navigation`/`format` off
with `background: inline` (and no `colors`) writes the renderer's SVG
byte-verbatim. Note that `navigation` bakes the *neighbor order* into each SVG:
reordering pages legitimately rewrites the affected SVGs.

Crucially, none of these — recolor, background mode, links, navigation, format —
change the `analyze` fingerprint (it is derived from path geometry only; see
"Incremental analysis"), so restyling a note never forces a paid re-transcription.
`colors` remaps only the four exact default fills the renderer emits, so the
link/nav overlays (`fill="none"`) are never touched; `background: blank` inserts a
white rectangle and `remove` deletes the background `<image>` entirely.

## Export template

`snorg -a <archive> export [PAGEID ...]` (PAGEIDs as arguments or stdin lines,
piped from `query`; a whole note is `query note <FILE_ID> | export`) groups the
pages per owning note and renders `export.template` **once** over all of them to
stdout. The template context **is** the `snorg retrieve` JSON array, under the
`notes` key (a template context needs a map root) — same keys, same nesting, no
hidden enrichment. One render sees every note, so a template can put pages from
many notes under one shared root (see `examples/emacs/orgmode-query.yaml`).
Iterate `notes`, then `note.pages`, then each page's `titles` / `keywords` /
`links` / `analysis`:

```jinja
{% for note in notes %}
{% for page in note.pages %}
{{ page.analysis.content }}
{% for t in page.titles %}- P{{ page.number }} L{{ t.level }} {{ t.analysis.name }}
{% endfor %}{% endfor %}{% endfor %}
```

Per-region transcriptions sit on the items themselves (`title.analysis.name`,
`link.analysis.name`); the page transcription is `page.analysis.content` (read
from the `<PAGEID>.md` sidecar); custom fields are `page.analysis.fields.<name>`.
Anything not yet `analyze`d renders blank (no error). Templating is
[pongo2](https://github.com/flosch/pongo2): Django-style filters
(`{{ name|cut:".note" }}`), not Python-Jinja `replace(...)`.

### Whitespace (trim_blocks + lstrip_blocks)

Templates render with `trim_blocks` **and** `lstrip_blocks` enabled, so block tags can
sit on their own indented lines without leaking blank lines or indentation into the
output — write naturally, no `{%- -%}` markers needed:

```jinja
{% for page in pages %}
  {% for link in page.links %}
- {{ link.name }}
  {% endfor %}
{% endfor %}
```

Two rules follow from how the trimming works:

- **Never end an output line with a block tag.** `trim_blocks` removes the newline after
  every `%}`, so a line ending in `{% endif %}` loses its line break and items run
  together. Keep trailing conditionals mid-line (end the line with text or a `{{ var }}`),
  or put `{% if %}`/`{% endif %}` on their own lines around the content line.
- **Space before a `{% %}` tag is stripped** (`lstrip_blocks`, even mid-line). Put spaces
  *inside* a conditional's branches, not before the tag: write
  `:{% if x %} yes{% else %} no{% endif %}`, not `: {% if x %}yes...`.

### Extra filters

Beyond the pongo2 built-ins (registered by `internal/export`):

- **`denote`** — turns a `FILE_ID` **or** `PAGEID` into an Emacs
  [denote](https://protesilaos.com/emacs/denote) identifier (`YYYYMMDDTHHMMSS`):

  ```jinja
  [[denote:{{ link.target_file_id|denote }}][{{ link.analysis.name|default:link.name }}]]
  ```

  e.g. `F20260414171729084889FDefCgWZgV3D` → `20260414T171729`. Unrecognised ids
  pass through unchanged.

- **`org`** *(org-mode only)* — converts Markdown (the analysis content format) to
  org-mode by shelling out to `pandoc` (must be on PATH; empty input renders empty
  without invoking it): `{{ page.analysis.content|org }}`.

- **`html`** *(HTML only)* — converts Markdown to an HTML fragment via `pandoc`
  (same PATH/empty-input rules as `org`); the result is marked safe so it is
  emitted unescaped: `{{ page.analysis.content|html }}` (see `examples/web/`).

- **`nestorgheadings:N`** *(org-mode only)* — demotes org headings by N stars (every
  line starting with `*` gains N), so page content nests under the template's own
  headings: `{{ page.analysis.content|org|nestorgheadings:2 }}`. N defaults to 1.

- **`nestmdheadings:N`** *(Markdown only)* — the Markdown analog: demotes ATX
  headings by N (every line starting with `#` gains N `#`), nesting the page's
  Markdown content under the template's headings:
  `{{ page.analysis.content|nestmdheadings:2 }}`. N defaults to 1.

## Merge semantics

Each `-c` file is parsed and deep-merged: scalars (and sequences) are overwritten
by later files; nested maps merge per key. So `analysis.fields` from different files
union by name. After merge, unset prompts get built-in defaults and unset
`ingest.svg` toggles default to true (an explicit `false` survives the merge).

## Incremental analysis

`analyze` fingerprints a page by its **path geometry** — the `d` attribute of
every `<path>`, whitespace-normalized (`analysis.source_hash` in `<PAGEID>.json`) —
and **skips** pages whose handwriting is unchanged, with no LLM call (and without
even rasterizing), unless `--force` is given. Because the hash ignores `fill`
(recolor), the background `<image>` (background mode) and the `<a><rect>` link/nav
overlays, restyling a note via `ingest.svg` never triggers re-analysis; only an
actual stroke edit (a different traced `d`) does. When a page did change, the
previous `<PAGEID>.md` is fed back through `analysis.content.update_prompt`, so the
fresh transcription diffs minimally against the old one (clean VCS history).

> Migration: this fingerprint replaces an earlier pixel hash. Existing
> `source_hash` values won't match, so the first `analyze` after upgrading
> re-transcribes every page once (through the update prompt → minimal diffs).

## Fields are derived from content, not the image

`content`, `titles` and `links` are vision tasks over the page image. Each entry in
`fields` is a **text** task: after `content` is transcribed, the field's prompt is
sent together with the content text (no image). This keeps custom fields cheap and
independent of rasterization. The result lands under `analysis.fields.<name>` in
`<PAGEID>.json`.

## Credentials

`api_key` may be left empty in every file and supplied instead via, in precedence
order, `api_key_command` (a shell command — run with `sh -c` — whose trimmed stdout is
the key, e.g. `pass show openai`) or the `OPENAI_API_KEY` environment variable, so no
secret need be written to disk. The command runs only for `analyze` (never `export`) and
only when `api_key` is empty; a non-zero exit is an error. `endpoint`, `model` and a key
(literal, command or env) are required; missing any is an error.
