# snorg

Supernote organizer: ingests Supernote `.note` files into a plaintext,
machine-readable, VCS-friendly archive for later retrieval, analysis and export.

The `.note` binary format is handled by shelling out to
[`supernote-tool`](https://github.com/jya-dev/supernote-tool) (must be on `PATH`);
the org-mode export filter shells out to `pandoc`, and `analyze-edit` uses `git`
for its diff/merge plumbing. Written in Go.

## Quick start

Prerequisites: [`supernote-tool`](https://github.com/jya-dev/supernote-tool) on
`PATH` (required for ingest); `pandoc` only if you export to org-mode; `git`
only if you edit transcriptions with `analyze-edit`.

Install the CLI (puts `snorg` on your `PATH` via `$GOBIN`/`$GOPATH/bin`):

```sh
go install github.com/jdlugosz963/snorg/cmd/snorg@latest
# or, from a clone:  go build -o snorg ./cmd/snorg
```

Then archive a note and read it back:

```sh
snorg -a ~/notes/archive ingest ~/supernote/Note/idea.note   # register (a dir works too)
snorg -a ~/notes/archive list                                # FILE_IDs, one per line
snorg -a ~/notes/archive query note <FILE_ID> | \
  snorg -a ~/notes/archive retrieve                          # assembled notes as JSON

# Optional AI pass: transcribe pages with a vision LLM (needs a provider config).
snorg -a ~/notes/archive query all | \
  snorg -c config.yaml -a ~/notes/archive analyze            # skips unchanged pages

# Fix a transcription by hand — or write one without any LLM at all.
# Edits are stored as a diff and survive re-analysis via a 3-way merge.
snorg -a ~/notes/archive analyze-edit <PAGEID>               # opens $VISUAL/$EDITOR

# Export through a template (see examples/config.yaml).
snorg -a ~/notes/archive query note <FILE_ID> | \
  snorg -c examples/config.yaml -a ~/notes/archive export
```

## Usage

Global flags and the archive path come first, then the command; the config
(`<archive>/config.yaml`, overridden by `-c` files) is loaded once and shared:

```
snorg -a <archive> ingest <file.note>    # register a note (dir works too)
snorg -a <archive> list                  # list FILE_IDs
snorg -a <archive> query all             # PAGEIDs of matching pages
snorg -a <archive> query all | \
  snorg -a <archive> retrieve            # assembled notes as JSON (grouped per note)
snorg -a <archive> query all | \
  snorg -c cfg.yaml -a <archive> analyze # LLM analysis (skips unchanged pages)
snorg -a <archive> analyze-edit <PAGEID> # edit a transcription in $VISUAL/$EDITOR
snorg -a <archive> query note <FILE_ID> | \
  snorg -c examples/config.yaml -a <archive> export  # render a template per note
```

`retrieve`, `analyze` and `export` take PAGEIDs as arguments or stdin lines, so
`query` pipes into any of them. `analyze-edit` takes exactly one PAGEID (the
editor needs the terminal); manual edits survive re-analysis — overlaps surface
as merge conflict markers to resolve with another `analyze-edit`.

Archive layout: `<archive>/<FILE_ID>/{note.json,<PAGEID>.json,<PAGEID>.md[.diff],<PAGEID>.svg,backgrounds/}`.

## Shell completion

`snorg` ships completion scripts; source the right one for your shell:

```sh
# .bashrc
source <(snorg completion bash)

# .zshrc
source <(snorg completion zsh)

# fish
snorg completion fish > ~/.config/fish/completions/snorg.fish

# Powershell: write `snorg completion powershell` to a file on your autocomplete path and run it.
```

## Example: Emacs / denote client

[`examples/emacs/`](examples/emacs/) is a worked end-to-end use case: it exports a
snorg archive into a human-readable, organized form — one org note per archived
note under a [denote](https://protesilaos.com/emacs/denote) directory, with
cross-note links resolving inside [Emacs](https://www.gnu.org/software/emacs/). It
pairs an org-mode export template (`examples/emacs/orgmode.yaml`) with `snorg.el`,
which drives the `snorg` CLI to import notes as denote org files and adds org links
that open a page's SVG or jump between notes. See `examples/emacs/README.md`.

## Example: static HTML site

[`examples/web/`](examples/web/) turns a selection of pages into a servable,
static HTML site: `export.sh` reads PAGEIDs on stdin, renders one listing page
(`index.html`) and one page per note over the pages you picked, and copies each
page's SVG so the `<img>` references resolve. Nothing to host but files.

```sh
# Select pages, then build + serve the site.
sort -u \
  <(snorg -a ~/notes/sn query starred) \
  <(snorg -a ~/notes/sn query keyword 'TODO') \
  | snorg -a ~/notes/sn query date 2026-07-01.. \
  | ./examples/web/export.sh -y ~/notes/sn /tmp/snorg-web-export/

python -m http.server -d /tmp/snorg-web-export/   # then open http://localhost:8000
```

The selection is plain set algebra over PAGEID lists — this pipeline builds
**(is starred OR has keyword `TODO`) AND is newer than 2026-07-01**:

Because every step is just lines of PAGEIDs, you compose selections with ordinary
shell tools (`sort -u`, `comm`, `grep`) and feed the result to `export.sh`,
`retrieve`, `analyze`, or `export`. See `examples/web/README.md`.

## More

See `docs/` for principles, architecture, the `.note` format, configuration and
the read contract; `examples/config.yaml` for an annotated config with a Markdown
export template.

## Contributing

Contributions are welcome — issues and pull requests both. Known limitations and
open problems worth tackling:

- **Only tested on the Supernote Manta.**
- **Links only resolve between `.note` files.** Other link kinds (web URLs, links to
  non-note files) are not handled yet.
- **Analysis prompts need fine-tuning.** The vision-LLM prompts could interpret notes
  more deeply — reconstructing tables, diagrams, etc. — and should emit Markdown that
  survives the `pandoc` conversion to org (and other formats) cleanly.
