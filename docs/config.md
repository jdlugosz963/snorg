# Configuration

`snorg analyze` is driven by YAML config files, holding provider credentials and
analysis prompts. Pass one or more with repeatable `-c`; later files override
earlier ones (deep-merge). Split secrets from committed config:

```sh
snorg analyze -c secrets.yaml -c analysis.yaml -page-id <PAGEID> <archive>
```

## Schema

```yaml
provider:
  endpoint: https://openrouter.ai/api/v1   # OpenAI-compatible base URL (required)
  api_key: sk-or-...                        # required; if empty, falls back to $OPENAI_API_KEY
  model: openai/gpt-4o                      # required; one global model for every task

analysis:
  content:                                  # whole-page transcription (vision)
    prompt: "..."                           # optional; built-in default if unset
  titles:                                   # per-title crop transcription (vision)
    prompt: "..."
  links:                                    # per-link crop transcription (vision)
    prompt: "..."
  fields:                                   # optional custom outputs (text)
    summary:
      prompt: "Summarize the following note page in 2 sentences:"
    todos:
      prompt: "List any action items mentioned in this note:"

export:
  template: |                               # single pongo2 (Jinja2-style) template
    {% for page in pages %}* Page {{ page.number }}
    {{ page.analysis.content }}
    {% endfor %}
```

The `provider` and `analysis` sections are only needed by `analyze`; the `export`
section is only needed by `export`. Either may stand alone in its own config file.

## Export template

`snorg export <archive> <FILE_ID>` renders the assembled note through `export.template`
to stdout. The template context is the `snorg retrieve` JSON verbatim (snake_case keys),
so iterate `pages`, then each page's `titles` / `keywords` / `links` / `analysis`:

```jinja
{% for page in pages %}
{{ page.analysis.content }}
{% for t in page.titles %}- P{{ page.number }} L{{ t.level }} {{ t.text }}
{% endfor %}{% endfor %}
```

Pages not yet `analyze`d have no `analysis`, which renders blank (no error). Templating
is [pongo2](https://github.com/flosch/pongo2): Django-style filters (`{{ name|cut:".note" }}`),
not Python-Jinja `replace(...)`.

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

### Export-specific extras

- **`denote` filter** — turns a `FILE_ID` into an Emacs [denote](https://protesilaos.com/emacs/denote)
  identifier (`YYYYMMDDTHHMMSS`), so a link's target resolves to its denote note:

  ```jinja
  [[denote:{{ link.target_file_id|denote }}][{{ link.name|cut:".note" }}]]
  ```

  e.g. `F20260414171729084889FDefCgWZgV3D` → `20260414T171729`. Unrecognised ids pass
  through unchanged.

- **Per-item analysis** — `page.links` / `page.titles` and the transcriptions in
  `page.analysis.links` / `page.analysis.titles` are index-aligned arrays. For
  convenience each `page.links[i]` / `page.titles[i]` is given a nested `analysis`
  (with `.name`) whenever the page was analyzed, so the transcribed name sits next to
  the deterministic one:

  ```jinja
  {% for link in page.links %}{{ link.name }}{% if link.analysis %} — {{ link.analysis.name }}{% endif %}
  {% endfor %}
  ```

## Merge semantics

Each `-c` file is parsed and deep-merged: scalars (and sequences) are overwritten
by later files; nested maps merge per key. So `analysis.fields` from different files
union by name. After merge, unset prompts get built-in defaults.

## Fields are derived from content, not the image

`content`, `titles` and `links` are vision tasks over the page image. Each entry in
`fields` is a **text** task: after `content` is transcribed, the field's prompt is
sent together with the content text (no image). This keeps custom fields cheap and
independent of rasterization. The result lands under `analysis.fields.<name>` in
`<PAGEID>.json`.

## Credentials

`api_key` may be left empty in every file and supplied via the `OPENAI_API_KEY`
environment variable instead, so no secret need be written to disk. `endpoint`,
`model` and a key (file or env) are required; missing any is an error.
