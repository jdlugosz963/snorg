# snorg

Supernote organizer: ingests Supernote `.note` files into a plaintext,
machine-readable, VCS-friendly archive for later retrieval and export.

The `.note` binary format is handled by shelling out to
[`supernote-tool`](https://github.com/jya-dev/supernote-tool) (must be on `PATH`).
Go, stdlib only.

## Install

```sh
make install   # build + install binary + shell completions (fish)
```

## Usage

```
snorg ingest   <file-or-dir>     # register a .note file
snorg list                       # list FILE_IDs
snorg retrieve <FILE_ID>         # note as JSON
snorg query    <filter> [arg]    # filter pages
snorg export   <FILE_ID>         # template export
```

## Shell completions

| Shell | Install | Reload |
|---|---|---|
| fish | `make install-completions-fish` (or `make install`) | `exec fish` |
| bash | `sudo make install-completions-bash` | `exec bash` |
| zsh  | `sudo make install-completions-zsh` | `exec zsh` |

For **Emacs** (`M-x shell`): install bash completions, then `M-x kill-shell` and `M-x shell`.

Archive layout: `<archive>/<FILE_ID>/{note.json,<PAGEID>.json,<PAGEID>.svg}`.

See `docs/` for principles, architecture, the `.note` format, and the read contract.
