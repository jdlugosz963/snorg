# snorg

Supernote organizer: ingests Supernote `.note` files into a plaintext,
machine-readable, VCS-friendly archive for later retrieval, analysis and export.

## Quick start

Prerequisites: Go (to install), plus `pandoc` if you use the `pandoc` export filter.

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
snorg query note=<FILE_ID> | snorg retrieve

# Optional AI pass: transcribe pages with a vision LLM (needs a provider config).
snorg query all | snorg -c config.yaml analyze      # skips unchanged pages

# Fix a transcription by hand — or write one without any LLM. Edits survive re-analysis.
snorg analyze-edit <PAGEID>                          # opens $VISUAL/$EDITOR

# Export through a template (export.template in a config; see docs/config.md).
snorg query note=<FILE_ID> | snorg -c export.yaml export
```

## Usage

```
NAME:
   snorg - supernote-organizer: ingest .note files into a plaintext archive

USAGE:
   snorg [-a <archive-path>] [-c config.yaml ...] [--no-user-config] [-v] <command> [command flags] [args]

COMMANDS:
   ingest        register a .note file (or all *.note under a dir) into the archive
   list          inspect an archive: bare = FILE_IDs; subcommands `keywords`/`tags` enumerate labels
   retrieve      print the assembled pages as a JSON object {archive, notes} (no PAGEIDs = read them from stdin)
   query         print PAGEIDs of matching pages, one per line (pipe into retrieve/analyze/export); -l/--long annotates them (browse-only, not pipe-safe)
   tag           add (or -r remove) a snorg-managed tag on pages, or with -n on whole notes (no ids = read them from stdin)
   analyze       vision-LLM analysis of pages; unchanged pages are skipped (no PAGEIDs = read them from stdin)
   analyze-edit  edit a page's transcription and title/link names in $VISUAL/$EDITOR (edits survive re-analysis)
   export        render the retrieved pages through the config's pongo2 template (no PAGEIDs = read them from stdin)
   serve         serve a local HTML viewer for the pages (no PAGEIDs and no pipe = the whole archive)
   migrate       upgrade note.json/page JSON to the current schema version (no PAGEIDs and no pipe = whole archive)
   help, h       Shows a list of commands or help for one command

GLOBAL OPTIONS:
   --archive PATH, -a PATH                            archive root PATH (holds the FILE_ID sub-dirs); optional if the user config sets archive:
   --config FILE, -c FILE [ --config FILE, -c FILE ]  config YAML FILE (repeatable; later files override earlier ones)
   --no-user-config                                   ignore the XDG user config (~/.config/snorg/config.yaml)
   --verbose, -v                                      describe what changed per item instead of drawing a progress bar
   --help, -h                                         show help
```

## Configuration

snorg loads config from two places, lowest priority first:

- the **user config**, `$XDG_CONFIG_HOME/snorg/config.yaml` (usually
  `~/.config/snorg/config.yaml`), loaded automatically; skip it with the global
  `--no-user-config`.
- `-c`/`--config FILE` (global, repeatable). These override the user config key by
  key, and a later `-c` file overrides an earlier one.

A config can `include:` other configs (paths relative to the including file). One
handy setup: a one-line user config that includes the archive's own config, which
sets `archive:` and everything else, so plain `snorg <command>` needs neither `-a`
nor `-c`:

```yaml
# ~/.config/snorg/config.yaml
include: [~/notes/sn/config.yaml]
```

```yaml
# ~/notes/sn/config.yaml
archive: .   # relative to this file
```

`-c` is useful for switching setups per run. For example, keep each export template
in its own config file and pass the one you want:

```sh
snorg query note=<FILE_ID> | snorg -c ~/snorg/org.yaml export
```

Full reference: [`docs/config.md`](docs/config.md).

## Browse and pick pages with fzf

`query -l` prints one page per line — PAGEID first, then note name, page number, a
`*` for starred, analyzed headings, `#keywords` and `@tags` — so a fuzzy finder can
filter on any of it. Pick pages, keep the PAGEID with `cut -f1`, and open just those in
the flat viewer:

```sh
snorg query all -l | fzf -m | cut -f1 | snorg serve -f
```

Or pick a whole note by name and pipe it into any consumer — here `retrieve`:

```sh
snorg query "note=$(snorg list -l | fzf | cut -f1)" | snorg retrieve
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

## Use it as a Go library

Everything the CLI does is available from Go — one `Client` over an archive plus its
config:

```go
import snorg "github.com/jdlugosz963/snorg/pkg/snorg"
```

[`docs/library.md`](docs/library.md) is the guide. For the mechanical reference —
every exported identifier and its signature — see [`docs/api.md`](docs/api.md),
generated from the source by gomarkdoc.

## Contributing

Contributions are welcome — issues and pull requests both. Known limitations and
open problems worth tackling:

- **Only tested on the Supernote Manta.**
- **A note keeps stale page links after a page moves out of it.** When a page is moved
  to another note on the device, snorg moves it within the archive too, but the old
  note's remaining page SVGs still carry navigation/link targets pointing at it until
  that note is itself re-ingested.
- **Analysis prompts need fine-tuning.** The vision-LLM prompts could interpret notes
  more deeply — reconstructing tables, diagrams, etc. — and should emit Markdown that
  survives the `pandoc` conversion to org (and other formats) cleanly.
