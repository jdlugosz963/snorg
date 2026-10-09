# snorg.el — Emacs org client

`snorg.el` brings archived Supernote notes into Emacs. It shells out to the
`snorg` CLI (`list` / `query` / `retrieve` / `export`, plus `analyze` /
`analyze-edit` from the review mode) and imports notes as org files.

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
| `snorg-executable`        | CLI binary name/path (default `"snorg"`, resolved on `PATH`).                                         |
| `snorg-archive`           | Archive path. Required; passed as `-a` together with `<archive>/config.yaml` and `--no-user-config`.  |
| `snorg-config-files`      | Extra `-c` config files, layered over the archive's own; one layer must define `export.template`.     |
| `snorg-backend`           | Active backend symbol (`denote` / `org-roam`); set by the first backend required.                     |
| `snorg-import-directory`  | Import destination: a string is used directly, a list prompts for one, `nil` uses the backend default. |
| `snorg-generated-heading` | Root heading text replaced on re-import (default `"Generated"`; keep in sync with the template).      |
| `snorg-default-keywords`  | Keywords prepended to every imported note's page keywords (default `("snorg")`).                      |
| `snorg-analyze-edit-editor` | `$VISUAL`/`$EDITOR` for `analyze-edit`; `nil` derives an `emacsclient` for this Emacs.             |

## Commands

- `M-x snorg-import` — pick an archived note by its `source` name and import it.
  A new backend note is created (the backend derives its own note id from the
  snorg FILE_ID, so re-import and cross-note links resolve); title = `source`,
  tags = `snorg-default-keywords` + page keywords. Re-importing an existing note replaces only its
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

## Org links

- `[[snorg:PAGEID][page N]]` — open that page's SVG from the archive.
- `[[snorg-note:PAGEID]]` — jump to the backend note owning `PAGEID` and move
  point to the heading whose `:SNORG_PAGEID:` matches.

Both are defined by `snorg.el` and carry exactly one PAGEID, never a `FILE_ID`.
snorg guarantees a PAGEID is unique across an archive, so `retrieve` resolves
the owning note itself. That also keeps them backend-agnostic: the resolved `FILE_ID` is a raw snorg id, which
whichever backend is active translates to its own note. The shipped export
template (`examples/emacs/orgmode.yaml`) emits `snorg:` links, stores the SVG path
in each page's `:SNORG_SVGP:` property, and uses `snorg-note:` for cross-note
links (no per-backend prefix to swap); a note link without a target page, or a link out of the
archive, is not a `snorg-note:` link — a web link exports as its plain URL, a file link as a
`[[denote:ID][label]]` link when its name follows denote's file-naming scheme
(`20260915T001940--solaris__calibre.epub`; id from the `denote` export filter),
else as its plain label — the label being the handwritten `analysis.name`, falling
back to the file name. Both link types support `C-c C-l` (`org-insert-link`) completion:
pick an archived note, then one of its pages.
