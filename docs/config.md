# Configuration

`snorg` commands are driven by YAML config files. Pass one or more with the
repeatable global `-c` flag (global, so it comes before the command, in any order
relative to `-a`); later files override earlier ones (deep-merge). Split secrets
from committed config:

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

1. `$XDG_CONFIG_HOME/snorg/config.yaml` (i.e. `~/.config/snorg/config.yaml`) —
   the XDG **user config**, if it exists: the one auto-loaded file. Pass
   `--no-user-config` to ignore it.
2. each `-c` file, left to right.

Later sources win via the deep-merge below, so `-c` files override the user
config, per key. A missing user config is not an error; a malformed config is.
An archive's own `config.yaml` is not loaded automatically — pass it explicitly
with `-c <archive>/config.yaml` (or pull it in via `include:`) if you want it.

### Including other configs

Any config file may pull in others with a top-level `include:` list. Paths resolve
relative to the **including file's** directory (a leading `~` is expanded), and are
followed recursively.

An included file is merged **over** the file that includes it — "above includer":
the include's keys win over the including file's own keys. Within one `include:`
list, later entries win over earlier ones, and an include's own includes win over
its body. The top-level layers (user → `-c`) still order lowest to highest as
above; each expands in place, so a `-c` file's includes sit above the user config
but below that `-c` file's own body only where the file itself sets a key.

```yaml
# main.yaml
include:
  - shared.yaml     # shared.yaml's keys override main.yaml's own
provider:
  model: from-main  # overridden if shared.yaml sets provider.model
```

A file that includes itself (directly or through a chain) is an error, not a loop.

## Archive path

The archive root is normally the global `-a`/`--archive` flag. It may instead be
set as the top-level `archive:` key in the merged config (naturally the **user
config**, but a `-c` file works too), so plain `snorg <command>` works with no
`-a` (the flag still wins when both are given). A relative `archive:` is resolved
**relative to the config file that declared it** (like `include:`/`image:` paths),
so `archive: .` in `<archive>/config.yaml` means that file's own directory; an
absolute value is used as-is and a leading `~` expands to your home directory. (The
`-a` flag, by contrast, is relative to the CWD like any shell path.) `archive:` is
only consulted to locate the archive — every other key merges as usual.

```yaml
# ~/.config/snorg/config.yaml
archive: ~/notes/sn
```

## Schema

```yaml
archive: ~/notes/sn                         # optional; default archive root when -a is absent
                                            # (any layer may set it — naturally the user config;
                                            #  -a flag overrides; relative = to this file, ~ expanded)

include:                                    # optional; other configs to pull in (paths relative to
  - shared.yaml                             #   this file, ~ expanded). Included keys override this
  - ~/snorg/base.yaml                       #   file's own keys ("above includer"); recursive; no cycles

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
change the `analyze` fingerprint (it is derived from the canonical
black-on-white rasterization, which forces pen colors black and drops overlays;
see "Incremental analysis"), so restyling a note never forces a paid re-transcription.
`colors` remaps only the four exact default fills the renderer emits, so the
link/nav overlays (`fill="none"`) are never touched; `background: blank` inserts a
white rectangle and `remove` deletes the background `<image>` entirely.

## Export template

`snorg -a <archive> export [PAGEID ...]` (PAGEIDs as arguments or stdin lines,
piped from `query`; a whole note is `query note=<FILE_ID> | export`) groups the
pages per owning note and renders `export.template` **once** over all of them to
stdout. The template context **is** the `snorg retrieve` JSON object — `notes` (the
note array) and `archive` (the absolute archive root) — same keys, same nesting, no
hidden enrichment. One render sees every note, so a template can put pages from
many notes under one shared root, and `archive` is how it builds an absolute path to
a page image: `{{ archive }}/{{ page.svg }}` (each page's `svg` is archive-relative).
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

Each file (including any it pulls in via `include:`) is parsed and deep-merged:
scalars are overwritten by later sources; nested maps merge per key; **sequences
concatenate**, lower layer first. So `analysis.fields` from different files union by
name, and a `templates:` list from every layer adds up rather than the highest one
replacing the rest. Lists are additive only — a higher layer can neither drop nor
replace a lower layer's entries (`templates: []` contributes nothing), and two
layers declaring the same template image is an error (see below). After merge, unset
prompts get built-in defaults and unset `ingest.svg` toggles default to true (an
explicit `false` survives the merge).

## Incremental analysis

`analyze` fingerprints a page by a **single canonical rasterization** — the page
rendered black-on-white (every `<path>` forced black, background/nav/link overlays
dropped) and thresholded to a 1-bit ink mask, hashed into `analysis.source_hash`
(`<PAGEID>.json`) — and **skips** pages whose handwriting is unchanged, with no LLM
call, unless `--force` is given. That one rasterization also feeds the per-box
region hashes and the LLM crops, so a run rasterizes each page exactly once
(including on a skip). Because the mask ignores `fill` (recolor), the background
`<image>` (background mode) and the `<a><rect>` link/nav overlays, restyling a note
via `ingest.svg` never triggers re-analysis; only an actual handwriting edit does.
When a page did change, the previous `<PAGEID>.md` is fed back through
`analysis.content.update_prompt`, so the fresh transcription diffs minimally against
the old one (clean VCS history).

> Migration: this raster-mask fingerprint replaces an earlier stroke-geometry hash.
> Existing `source_hash` values won't match, so the first `analyze` after upgrading
> re-transcribes every page once (through the update prompt → minimal diffs). No
> `snorg migrate` pass is needed — the fingerprint self-heals on that first run.

## Template regions

The `templates:` section declares background templates and their boxes, so
`analyze` transcribes a page drawn on a known template per box into its
`<PAGEID>.md` (as id-keyed region sections, in place of whole-page content). It is
an ordinary config section — it layers, merges and can be included like any other
(no provider credentials needed to read it). A page is matched to a template by its
background-image hash. See **`docs/templates.md`** for the format and workflow; the
short version:

```yaml
templates:
  - image: templates/prawo_jazdy.png   # device-form grayscale PNG; path relative to THIS config file
    boxes:
      - {id: title, label: "Title", rect: {x: 0, y: 0, w: 1920, h: 384}, analyze: true, prompt: "..."}
```

Each `image:` path is resolved **relative to the config file that declared it**
(`~` expanded), so a per-archive config you pass with `-c` can reference images
under the archive while a shared user config carries its own alongside it. The
image is hashed to match a page's background, so register the device-form grayscale
PNG exactly (a re-encode hashes differently).

Because sequences concatenate, the `templates:` lists of the user config, an archive
config, every `-c` file and every `include:` all apply **together** — a file can
declare one template and pull in more, and each generator can ship its own file.
The one thing to watch: nothing is de-duplicated, so registering the same image
twice (e.g. including the same file from two places) makes two templates share a
background hash, which errors on the next command. Include it once.

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
