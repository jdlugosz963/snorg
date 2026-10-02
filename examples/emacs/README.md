# snorg.el — Emacs org client

`snorg.el` brings archived Supernote notes into Emacs. It shells out to the
`snorg` CLI (`list` / `query` / `retrieve` / `export` — the page-oriented
commands get their PAGEIDs from `query note=<FILE_ID>`) and imports notes as org files.

Where imported notes live is a pluggable backend: `snorg.el` holds the generic
interface (`snorg-backend-find` / `snorg-backend-create`, dispatched on
`snorg-backend`), and a backend file implements it per note-taking package. Core
hands the backend the **raw snorg FILE_ID**; the backend is the only place that
translates it to and from its own note id, both directions:

- `snorg-denote.el` for [denote](https://protesilaos.com/emacs/denote) — the
  denote identifier (`YYYYMMDDTHHMMSS`) is derived from the FILE_ID, so a note
  maps to a stable denote id every time.
- `snorg-org-roam.el` for [org-roam](https://www.orgroam.com/) — an ordinary
  org-roam node with a **native** `:ID:` (a UUID) and native
  `<timestamp>-slug.org` filename; the snorg identity rides along as a
  `snorg:FILE_ID` `:ROAM_REFS:` ref, which is what re-import resolves by
  (`org-roam-node-from-ref`).

Requiring a backend selects it when none is set yet.

## Install

Put the files on your `load-path`, require `snorg` plus one backend
(`snorg-denote` needs `denote`, `snorg-org-roam` needs `org-roam`), then:

```elisp
(require 'snorg)
(require 'snorg-denote)   ; or (require 'snorg-org-roam)
(setq snorg-archive "~/notes/sn/arch"
      snorg-config-files '("~/work/snorg/examples/emacs/orgmode.yaml"))
```

`snorg-archive` is **required**. Every CLI call passes it as `-a`, adds the
archive's own `config.yaml` as `-c` and suppresses the XDG user config with
`--no-user-config` — so the client always targets exactly that archive and never
silently inherits `archive:` from somewhere else. `snorg-config-files` layers
extra configs *on top* of the archive's own.

Config variables (all plain `defvar`s you may override):

| Variable                  | Meaning                                                                                               |
|---------------------------|-------------------------------------------------------------------------------------------------------|
| `snorg-executable`        | CLI binary name/path. `nil` (the default) resolves the **newest** executable `snorg` between `default-directory` and the project root, falling back to `PATH`. |
| `snorg-archive`           | Archive path. Required; passed as `-a` together with `<archive>/config.yaml` and `--no-user-config`.  |
| `snorg-config-files`      | Extra `-c` config files, layered over the archive's own; one layer must define `export.template`.     |
| `snorg-backend`           | Active backend symbol (`denote` / `org-roam`); set by the first backend required.                     |
| `snorg-import-directory`  | Import destination: a string is used directly, a list prompts for one, `nil` uses the backend default. |
| `snorg-generated-heading` | Root heading text replaced on re-import (default `"Generated"`; keep in sync with the template).      |

## Commands

- `M-x snorg-import` — pick an archived note by its `source` name and import it.
  A new backend note is created (the backend derives its own note id from the
  snorg FILE_ID, so re-import and cross-note links resolve); title = `source`,
  tags = page keywords. Re-importing an existing note replaces only its
  generated subtree, leaving your own edits intact.
- `M-x snorg-import-all` — import (or re-import) every archived note in one go;
  the destination is prompted once, and per-note failures are reported at the
  end without aborting the run.
- `M-x snorg-view` — split into two windows: note on the left, page SVG on the
  right. Works from anywhere in the note: the page heading is the one at point
  (a heading with a `:SNORG_SVGP:` property), else the nearest ancestor
  carrying one, else the first page heading in the file. The left buffer goes
  read-only and folds down to just the page under review — the mode is
  strictly a snorg interface, plain keys are review commands instead of
  self-insert, and a header line summarizes them:
  - `n`/`p` cycle pages, refolding to follow the SVG;
  - `e` edits the page's transcription (`snorg-analyze-edit`), `a`
    (re-)transcribes it (`snorg-analyze`); a prefix argument (`C-u a`) forces
    re-transcription of an unchanged page;
  - `o` opens the current SVG in the system viewer (`xdg-open`);
  - when the archive is a git repo, `P`/`N` step a diff overlay of the
    current page against progressively older/newer revisions (strokes added
    since the compared revision are green, removed strokes red); `N` back to
    depth 0 restores the plain SVG, and switching pages resets it; a numeric
    prefix (e.g. `C-3 P`) steps several revisions at once;
  - `h` (or `?`) shows the full key list in the echo area;
  - `q` quits, restoring the folding, the window layout and point from before
    entry.
- `M-x snorg-analyze-edit` — with point on a page heading, edit its
  transcription via the CLI's `analyze-edit`; opens in this Emacs through
  emacsclient (finish with `C-x #`). Edits survive re-analysis, and the note's
  generated subtree refreshes in place.
- `M-x snorg-analyze` — with point on a page heading, (re-)transcribe it via
  the CLI's `analyze` (asks first — it may spend an LLM call; prefix argument
  forces re-transcription of an unchanged page), then refresh the subtree.
- `M-x snorg-query` — browse the archive by query. Prompts for a snorg query
  expression (default `all` — e.g. `starred AND tag:work`, `mtime:today`,
  `content~regexp`) and lists every matching **page** in `*snorg-query*`, one
  row per page: note, page number, star, analyzed headings, device keywords and
  snorg tags. The PAGEID is the row id rather than a column, so the table stays
  readable while every command still knows which page it is on. Keys:
  - `RET` opens the page overview (below);
  - `e` visits the page SVG in Emacs, `E` hands it to the system viewer;
  - `n`/`p` move by row, `g` re-runs the query, `q` buries the list;
  - `h` (or `?`) shows the key list; the `Note`, `Page` (numerically) and
    `Headings` columns sort on click.
- `M-x snorg-show-page` — the page overview, `*snorg-page*`: everything snorg
  knows about one page in plain text — the owning note, the page's position and
  star, its snorg tags (effective) and the note's own, its device keywords, its
  titles (`(unanalyzed)` until `analyze` has run), its links, and its
  transcription — per region on a templated page — plus any generated fields.
  `e`/`E` open the SVG as in the list, `g` re-fetches, `q` buries.
- `M-x snorg-reset-cache` — drop every per-session cache: `retrieve` results,
  page lookups, page counts and the resolved CLI binary. Run it after
  rebuilding the CLI so `snorg-executable`'s search picks up the new binary.

## Keybindings

`snorg-command-map` is a prefix keymap gathering the interactive commands:

| Key | Command              |
|-----|----------------------|
| `i` | `snorg-import`       |
| `I` | `snorg-import-all`   |
| `v` | `snorg-view`         |
| `e` | `snorg-analyze-edit` |
| `a` | `snorg-analyze`      |
| `q` | `snorg-query`        |
| `r` | `snorg-reset-cache`  |

It is left unbound — pick a prefix key yourself:

```elisp
(define-key global-map (kbd "C-c n") 'snorg-command-map)
```

## Org links

- `[[snorg:PAGEID][page N]]` — open that page's SVG from the archive.
- `[[snorg-note:PAGEID]]` — jump to the backend note owning `PAGEID` and move
  point to the heading whose `:SNORG_PAGEID:` matches.
- `[[snorg-note:FILE_ID]]` — the same, for a link that targets a note rather
  than one of its pages: open the note, no page jump.

Both are defined by `snorg.el` and carry one bare snorg id. A PAGEID is unique
across an archive, so `retrieve` (cached) resolves the owning note itself — no
`FILE_ID` in the link. `snorg-note:` tells the two apart by the device's own id
prefix (`P…` page, `F…` note), so a note-scoped link stays a single id too. That
also keeps them backend-agnostic: the resolved `FILE_ID` is a raw snorg id, which
whichever backend is active translates to its own note. The shipped export
template (`examples/emacs/orgmode.yaml`) emits `snorg:` links, stores the SVG path
in each page's `:SNORG_SVGP:` property, and uses `snorg-note:` for cross-note
links (no per-backend prefix to swap); a link out of the archive is not a
`snorg-note:` link — a web link exports as its plain URL, a file link as its name
and device path. Both link types support `C-c C-l` (`org-insert-link`) completion:
pick an archived note, then one of its pages.
