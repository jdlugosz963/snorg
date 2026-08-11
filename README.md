# snorg

Supernote organizer: ingests Supernote `.note` files into a plaintext,
machine-readable, VCS-friendly archive for later retrieval, analysis and export.

## Quick start

Prerequisites: none, except `pandoc` if you use the `org` or `html` export filters.

Install the CLI (puts `snorg` on your `PATH` via `$GOBIN`/`$GOPATH/bin`):

```sh
go install github.com/jdlugosz963/snorg/cmd/snorg@latest
# or, from a clone:  go build -o snorg ./cmd/snorg
```

snorg targets one archive. Pass `-a <path>` to every command, or set
`archive: <path>` in `~/.config/snorg/config.yaml` once and omit `-a` — the examples
below assume the latter.

```sh
snorg ingest ~/supernote/Note/idea.note   # register one note (a directory works too)
snorg list                                # FILE_IDs, one per line
snorg serve                               # browse the whole archive at http://127.0.0.1:8080
```

```sh
# Assemble notes as JSON for your own tooling.
snorg query note <FILE_ID> | snorg retrieve

# Optional AI pass: transcribe pages with a vision LLM (needs a provider config).
snorg query all | snorg -c config.yaml analyze      # skips unchanged pages

# Fix a transcription by hand — or write one without any LLM. Edits survive re-analysis.
snorg analyze-edit <PAGEID>                          # opens $VISUAL/$EDITOR

# Export through a template (see examples/config.yaml).
snorg query note <FILE_ID> | snorg -c examples/config.yaml export
```

## Usage

```
snorg ingest <file.note|dir>   register note(s) into the archive
snorg list [-l]                FILE_IDs (-l appends the note name)
snorg query <filter> [arg]     PAGEIDs of matching pages; filters: all, note <FILE_ID>,
                               starred, keyword <re>, content <re>, date <spec>,
                               unanalyzed, and a `not <filter>` prefix. -l = annotated
                               browse form (tab-separated, PAGEID first)
snorg retrieve [PAGEID...]     assemble pages into JSON, grouped per note
snorg serve [-f] [PAGEID...]   built-in HTTP viewer (whole archive if no PAGEIDs; -f = flat)
snorg analyze [PAGEID...]      vision-LLM transcription (skips unchanged pages)
snorg analyze-edit <PAGEID>    edit a transcription in $VISUAL/$EDITOR
snorg export [PAGEID...]       render pages through a pongo2/Jinja2 template
```

`retrieve`, `serve`, `analyze` and `export` take PAGEIDs as arguments **or** stdin
lines, so `query` pipes into any of them. Because every step is just lines of PAGEIDs,
you compose selections with ordinary shell tools (`sort -u`, `comm`, `grep`, `fzf`).
`query` itself reads PAGEIDs from stdin when piped, so filters intersect:
`snorg query keyword foo | snorg query date today` == foo ∩ today.

Archive layout: `<archive>/<FILE_ID>/{note.json,<PAGEID>.json,<PAGEID>.md[.diff],<PAGEID>.svg,backgrounds/}`.

## Browse and pick pages with fzf

`query -l` prints one page per line — PAGEID first, then note name, page number, a
`*` for starred and `#tags` for keywords — so a fuzzy finder can filter on any of it.
Pick pages, keep the PAGEID with `cut -f1`, and open just those in the flat viewer:

```sh
snorg query all -l | fzf -m | cut -f1 | snorg serve -f
```

Or pick a whole note by name and pipe it into any consumer — here `retrieve`:

```sh
snorg query note $(snorg list -l | fzf | cut -f1) | snorg retrieve
```

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

## Contributing

Contributions are welcome — issues and pull requests both. Known limitations and
open problems worth tackling:

- **Only tested on the Supernote Manta.**
- **Links only resolve between `.note` files.** Other link kinds (web URLs, links to
  non-note files) are not handled yet.
- **Analysis prompts need fine-tuning.** The vision-LLM prompts could interpret notes
  more deeply — reconstructing tables, diagrams, etc. — and should emit Markdown that
  survives the `pandoc` conversion to org (and other formats) cleanly.
