# snorg.el — Emacs org/denote client

`snorg.el` brings archived Supernote notes into Emacs. It shells out to the
`snorg` CLI (`list` / `query` / `retrieve` / `export` — the page-oriented
commands get their PAGEIDs from `query note`) and imports notes as
[denote](https://protesilaos.com/emacs/denote) org files.

## Install

Put `snorg.el` on your `load-path` (`denote` and `org` required), then:

```elisp
(require 'snorg)
(setq snorg-archive "~/notes/sn/arch"
      snorg-config-files '("~/work/snorg/examples/emacs/orgmode.yaml"))
```

Config variables (all plain `defvar`s you may override):

| Variable                  | Meaning                                                                                               |
|---------------------------|-------------------------------------------------------------------------------------------------------|
| `snorg-executable`        | CLI binary name/path (default `"snorg"`).                                                             |
| `snorg-archive`           | Archive path, passed as `-a`. **Required.**                                                           |
| `snorg-config-files`      | List of `-c` config files; one must define `export.template`.                                         |
| `snorg-denote-directory`  | Import destination: a string is used directly, a list prompts for one, `nil` uses `denote-directory`. |
| `snorg-generated-heading` | Root heading text replaced on re-import (default `"Generated"`; keep in sync with the template).      |

## Commands

- `M-x snorg-import` — pick an archived note by its `source` name and import it.
  A new denote note is created (identifier derived from the FILE_ID, so
  cross-note `denote:` links resolve); title = `source`, tags = page keywords.
  Re-importing an existing note replaces only its generated subtree, leaving
  your own edits intact.
- `M-x snorg-import-all` — import (or re-import) every archived note in one go;
  the destination is prompted once, and per-note failures are reported at the
  end without aborting the run.
- `M-x snorg-view` — with point on a page heading (one with a `:SNORG_SVGP:`
  property), split into two windows: note on the left, page SVG on the right.
  The left buffer folds down to just the page under review; `M-n`/`M-p` cycle
  pages (refolding to follow the SVG) and `q` quits, restoring the prior folding.
  The SVG is read from the heading's `:SNORG_SVGP:` property. In the review
  window `o` opens the current SVG in the system viewer (`xdg-open`), and — when
  the archive is a git repo — `M-P`/`M-N` step a diff overlay of the current page
  against progressively older/newer revisions (strokes added since the compared
  revision are green, removed strokes red); `M-N` back to depth 0 restores the
  plain SVG, and switching pages resets it.
- `M-x snorg-reset-cache` — drop the per-session `retrieve` cache.

## Keybindings

`snorg-command-map` is a prefix keymap gathering the interactive commands:

| Key | Command             |
|-----|---------------------|
| `i` | `snorg-import`      |
| `I` | `snorg-import-all`  |
| `v` | `snorg-view`        |
| `r` | `snorg-reset-cache` |

It is left unbound — pick a prefix key yourself:

```elisp
(define-key global-map (kbd "C-c n") 'snorg-command-map)
```

## Org links

- `[[snorg:FILEID/PAGEID.svg][page N]]` — open a page SVG from the archive.
- `[[denote-snorg:IDENTIFIER::PAGEID]]` — jump to a denote note and move point
  to the heading whose `:SNORG_PAGEID:` matches `PAGEID`.

These are emitted by the shipped export template (`examples/emacs/orgmode.yaml`),
which also stores the SVG path in each page's `:SNORG_SVGP:` property. Both link
types support `C-c C-l` (`org-insert-link`) completion: pick an archived note,
then one of its pages.

## Cross-note query exports

`orgmode-query.yaml` is a second template for ad-hoc query results spanning
notes: every selected page — whichever note it comes from — sits flat at one
level under a single root heading, with the same `:SNORG_PAGEID:`/`:SNORG_SVGP:`
properties, so `snorg-view` cycles straight through the whole result set:

```sh
snorg -a <archive> query starred | \
  snorg -c examples/emacs/orgmode-query.yaml -a <archive> export > starred.org
```
