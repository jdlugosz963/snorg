;;; snorg.el --- Org client for the snorg archive -*- lexical-binding: t; -*-

;; Author: Jakub Dlugosz
;; Keywords: outlines, convenience
;; Package-Requires: ((emacs "28.1") (org "9.6"))

;;; Commentary:

;; Emacs client for `snorg' (supernote-organizer).  It shells out to the
;; snorg CLI and brings archived Supernote notes into Emacs as org notes.
;;
;; Where imported notes live is a pluggable *backend*: this file holds the
;; generic interface (`snorg-backend-find'/`snorg-backend-create', dispatched
;; on `snorg-backend'), and a backend file implements it for a note-taking
;; package -- (require 'snorg-denote) for denote, (require 'snorg-org-roam)
;; for org-roam.  Loading a backend selects it when none is set yet.  The
;; identity core hands a backend is the raw snorg FILE_ID; each backend owns
;; the translation to and from its own note id (both directions).
;;
;; Features:
;;
;; - `snorg-import' -- pick an archived note by name and import it into a
;;   backend note (created fresh, or its generated subtree refreshed in place
;;   on re-import).  `snorg-import-all' imports every archived note.
;;
;; - Org link types with `C-c C-l' completion, each carrying one bare PAGEID
;;   (unique across the archive, so it resolves its owning note):
;;   `snorg:PAGEID' opens that page's SVG; `snorg-note:PAGEID' jumps to the
;;   backend note owning the page and to its heading.
;;
;; - `snorg-view' -- a dual-window review: the note on the left, folded to
;;   the page under review, its SVG on the right.  The buffer goes read-only
;;   and plain keys drive the review: `n'/`p' pages, `e' edit the
;;   transcription (`snorg-analyze-edit'), `a' re-transcribe it
;;   (`snorg-analyze'), `o' system viewer, `P'/`N' git stroke-diff overlay,
;;   `h' help, `q' quit (restoring folds, windows and point).
;;
;; Set `snorg-archive' before use: every CLI call passes it as `-a', adds the
;; archive's own config.yaml as `-c' and suppresses the XDG user config, so the
;; client targets exactly that archive.  `snorg-config-files' layers extra
;; configs on top; one of the layers must define `export.template' for
;; `snorg-import' (see examples/emacs/orgmode.yaml in the snorg repo).

;;; Code:

(require 'org)
(require 'json)
(require 'subr-x)
(require 'cl-lib)

;; `server' is loaded lazily by `snorg--emacsclient-editor' (only that command
;; needs it), so declare its function to keep the byte-compiler quiet.
(declare-function server-running-p "server")

;;;; Configuration

(defgroup snorg nil
  "Org client for the snorg archive."
  :group 'org
  :prefix "snorg-")

(defvar snorg-executable "snorg"
  "Name or path of the snorg CLI binary.")

(defvar snorg-archive nil
  "Path to the snorg archive.  Required.
Every CLI call passes it as `-a', together with the archive's own
`config.yaml' as `-c' and `--no-user-config' -- so the client targets
exactly this archive and never silently inherits the XDG user config.")

(defvar snorg-config-files nil
  "Extra snorg config files, each passed to the CLI as `-c'.
They come *after* the archive's own `config.yaml', so they win the
CLI's later-wins merge.  One of the layers must define `export.template'
for `snorg-import' to work; the archive config usually does.")

(defvar snorg-import-directory nil
  "Destination for imported notes.
A string is used directly.  A list of strings prompts for one on
creation.  When nil, the backend's default directory is used.")

(defvar snorg-generated-heading "Generated"
  "Headline of the export root heading.
On re-import the top-level heading with this text is replaced.
Keep in sync with the export template's root heading.")

(defvar snorg-default-keywords '("snorg")
  "Keywords added to every imported note.
Prepended to the note's own page keywords on `snorg-import'.")

;;;; CLI / process layer

(defun snorg--global-args ()
  "Return the global CLI args: archive, its config, and `--no-user-config'.
`snorg-archive' is required; the archive's own `config.yaml' is passed
explicitly (the CLI never auto-loads it) and the XDG user config is
suppressed, so a call targets exactly this archive.  `snorg-config-files'
follow, overriding via the CLI's later-wins merge."
  (unless snorg-archive
    (user-error "snorg: set `snorg-archive' to the archive path"))
  (let ((archive (expand-file-name snorg-archive)))
    (append (list "-a" archive
                  "-c" (expand-file-name "config.yaml" archive)
                  "--no-user-config")
            (mapcan (lambda (f) (list "-c" (expand-file-name f)))
                    snorg-config-files))))

(defun snorg--archive-file (rel)
  "Return the absolute path of archive-relative path REL."
  (expand-file-name rel (expand-file-name snorg-archive)))

(defun snorg--call (&rest args)
  "Run the snorg CLI with global flags followed by ARGS.
Return stdout as a string, or signal an error carrying stderr.  Stderr
is kept apart so it never pollutes the parsed output.  The nil INFILE
is /dev/null, a character device, so the CLI sees no piped stdin."
  (let ((err-file (make-temp-file "snorg-err")))
    (unwind-protect
        (with-temp-buffer
          (let ((status (apply #'call-process snorg-executable nil
                               (list t err-file) nil
                               (append (snorg--global-args) args))))
            (unless (eq status 0)
              (error "snorg %s failed (%s): %s"
                     (string-join args " ") status
                     (with-temp-buffer
                       (insert-file-contents err-file)
                       (string-trim (buffer-string)))))
            (buffer-string)))
      (delete-file err-file))))

(defun snorg--lines (&rest args)
  "Run the snorg CLI with ARGS; return its stdout as a list of lines."
  (split-string (apply #'snorg--call args) "\n" t))

(defun snorg--run-async (name args env on-success)
  "Run the snorg CLI with ARGS asynchronously, as process NAME.
ENV is prepended to `process-environment'.  On exit, echo the CLI's
output and call ON-SUCCESS with no arguments when it exited 0.  A pipe
rather than a pty keeps the CLI's progress output free of escapes."
  (let* ((process-environment (append env process-environment))
         (proc (make-process
                :name name
                :buffer (generate-new-buffer (format " *%s*" name))
                :noquery t
                :connection-type 'pipe
                :command (append (list snorg-executable)
                                 (snorg--global-args) args))))
    (set-process-sentinel
     proc
     (lambda (p _event)
       (when (memq (process-status p) '(exit signal))
         (let ((out (with-current-buffer (process-buffer p)
                      (string-trim (buffer-string)))))
           (kill-buffer (process-buffer p))
           (if (not (eq (process-exit-status p) 0))
               (message "%s failed: %s" name
                        (if (string-empty-p out) "(no output)" out))
             (message "snorg: %s" out)
             (funcall on-success))))))))

;;;; Archive access

(defun snorg--notes ()
  "Return the archived notes as an alist of (FILE_ID . NAME).
NAME is the note's source name without `.note', from `list -l'."
  (mapcar (lambda (line)
            (let ((col (split-string line "\t")))
              (cons (car col) (or (cadr col) (car col)))))
          (snorg--lines "list" "-l")))

(defun snorg--note-pageids (file-id)
  "Return the PAGEIDs of FILE-ID (placement order) via `query note=FILE_ID'."
  (snorg--lines "query" (concat "note=" file-id)))

(defun snorg--retrieve (pageids)
  "Return the retrieve NoteViews (alists) for PAGEIDS."
  (let ((json-object-type 'alist)
        (json-array-type 'list)
        (json-key-type 'symbol))
    (alist-get 'notes (json-read-from-string
                       (apply #'snorg--call "retrieve" pageids)))))

(defun snorg--page-view (pageid)
  "Return (NOTE-VIEW . PAGE-VIEW) for PAGEID.
`retrieve' groups the pages it is given per owning note, so a single
PAGEID comes back as one note holding one page."
  (let* ((note (car (snorg--retrieve (list pageid))))
         (page (car (alist-get 'pages note))))
    (unless page
      (error "snorg: no page %s in the archive" pageid))
    (cons note page)))

(defun snorg-read-file-id ()
  "Prompt for an archived note by name; return its FILE_ID."
  (let* ((choices (mapcar (lambda (n) (cons (format "%s  [%s]" (cdr n) (car n))
                                            (car n)))
                          (snorg--notes)))
         (key (completing-read "Note: " choices nil t)))
    (or (cdr (assoc key choices))
        (user-error "Unknown note: %s" key))))

;;;; Backend interface

(defvar snorg-backend nil
  "Symbol naming the note backend used for import and note links.
A backend file ((require \\='snorg-denote), (require \\='snorg-org-roam))
implements the `snorg-backend-*' generics for its symbol and sets this
variable when it is still nil; set it explicitly to choose between
several loaded backends.")

(defun snorg--backend ()
  "Return the active backend symbol, or signal a `user-error'."
  (or snorg-backend
      (user-error
       "No snorg backend; (require 'snorg-denote) or (require 'snorg-org-roam)")))

;; The identity snorg hands a backend is the raw snorg FILE_ID (as the CLI
;; emits it) -- the generic, format-neutral id.  Each backend is the *only*
;; place that translates it to and from its own note id (denote's
;; YYYYMMDDTHHMMSS, an org-roam ROAM_REF, ...); core never speaks a backend's
;; dialect.

(cl-defgeneric snorg-backend-find (backend file-id)
  "Return the path of BACKEND's note for snorg FILE-ID, or nil.
FILE-ID is the raw snorg FILE_ID; the backend maps it to its own note id.")

(cl-defgeneric snorg-backend-create (backend file-id title keywords directory)
  "Create a BACKEND note for snorg FILE-ID, with TITLE and KEYWORDS.
FILE-ID is the raw snorg FILE_ID; the backend mints its own note id from
it and records enough to map back on the next `snorg-backend-find'.
DIRECTORY is the resolved `snorg-import-directory' (nil means the
backend's default).  Return the new note's path; the note may still
live in an unsaved buffer rather than on disk.")

;;;; Import

(defun snorg--title (view)
  "Return the note title from retrieve alist VIEW (source without .note)."
  (string-remove-suffix ".note" (or (alist-get 'source view) "")))

(defun snorg--keywords (view)
  "Return the note keywords for retrieve alist VIEW.
`snorg-default-keywords' first, then the union of all page keywords."
  (let ((tags (reverse snorg-default-keywords)))
    (dolist (page (alist-get 'pages view))
      (dolist (kw (alist-get 'keywords page))
        (let ((text (alist-get 'text kw)))
          (when (and text (not (member text tags)))
            (push text tags)))))
    (nreverse tags)))

(defun snorg--destination-directory ()
  "Resolve the destination directory for a new imported note.
Return nil to let the backend pick its default."
  (cond
   ((stringp snorg-import-directory) snorg-import-directory)
   ((consp snorg-import-directory)
    (completing-read "Import directory: " snorg-import-directory nil t))
   (t nil)))

(defun snorg--replace-generated (body)
  "Replace the `snorg-generated-heading' subtree in the current buffer with BODY.
Insert BODY at end of buffer when no such heading exists.  BODY is the
export text (a single top-level heading subtree).  Writes through
read-only: `snorg-view-mode' keeps the note buffer read-only, and the
analyze/edit refresh replaces the subtree while the review is open."
  (let ((inhibit-read-only t))
    (org-with-wide-buffer
     (goto-char (point-min))
     (if (re-search-forward
          (format "^\\*[ \t]+%s[ \t]*$" (regexp-quote snorg-generated-heading))
          nil t)
         (progn
           (org-back-to-heading t)
           (delete-region (point) (org-end-of-subtree t t)))
       (goto-char (point-max))
       (unless (bolp) (insert "\n")))
     (insert (string-trim-right body) "\n"))))

(defun snorg--import (file-id)
  "Import archived note FILE-ID into a backend note and return its path.
Create it fresh, or refresh its generated subtree if it already exists.
Does not display the buffer."
  (let* ((backend (snorg--backend))
         (pageids (snorg--note-pageids file-id))
         (view (car (snorg--retrieve pageids)))
         (title (snorg--title view))
         (body (apply #'snorg--call "export" pageids))
         (existing (snorg-backend-find backend file-id))
         (path
          (or existing
              (snorg-backend-create backend file-id title (snorg--keywords view)
                                    (snorg--destination-directory)))))
    (unless path
      (error "Failed to locate or create a note for %s" file-id))
    (with-current-buffer (find-file-noselect path)
      (snorg--replace-generated body)
      (save-buffer))
    (message "snorg: %s note %s" (if existing "updated" "created") title)
    path))

;;;###autoload
(defun snorg-import (file-id)
  "Import archived note FILE-ID into a backend note and display it.
Create it fresh, or refresh its generated subtree if it already exists."
  (interactive (list (snorg-read-file-id)))
  (pop-to-buffer (find-file-noselect (snorg--import file-id))))

;;;###autoload
(defun snorg-import-all ()
  "Import every archived note into a backend note.
Create each fresh, or refresh its generated subtree if it already exists.
The destination directory is resolved once for the whole batch, and per-note
errors are collected and reported at the end rather than aborting the run."
  (interactive)
  (let* ((ids (mapcar #'car (snorg--notes)))
         (total (length ids))
         ;; Resolve (and possibly prompt for) the destination once, so the
         ;; batch does not prompt per note.
         (snorg-import-directory (snorg--destination-directory))
         (n 0)
         (failures nil))
    (dolist (id ids)
      (setq n (1+ n))
      (message "snorg: importing %d/%d %s" n total id)
      (condition-case err
          (snorg--import id)
        (error (push (cons id (error-message-string err)) failures))))
    (if failures
        (message "snorg: imported %d/%d note(s); %d failed: %s"
                 (- total (length failures)) total (length failures)
                 (mapconcat #'car (reverse failures) ", "))
      (message "snorg: imported %d note(s)" total))))

;;;; Transcription: edit and (re-)analyze

(defvar snorg-analyze-edit-editor nil
  "Editor command line passed to `snorg analyze-edit' as $VISUAL/$EDITOR.
When nil, an `emacsclient' invocation for the current Emacs server is
derived automatically, so the transcription opens in this Emacs (finish
with \\[server-edit], `C-x #').  Set this to override, e.g. for a
terminal Emacs, or a TCP/named server emacsclient cannot reach by default.")

(defun snorg--page-id-at-point ()
  "Return the PAGEID of the page heading at point, or signal a `user-error'.
Read from the `SNORG_PAGEID' property set by the export template."
  (let ((id (org-entry-get nil "SNORG_PAGEID")))
    (if (and id (not (string-empty-p id)))
        id
      (user-error "Point is not on a heading with a :SNORG_PAGEID: property"))))

(defun snorg--emacsclient-editor ()
  "Return an `emacsclient' command line for the running Emacs server.
Start the server when needed, and append `-s SERVER-NAME' for a
non-default server name so emacsclient reaches this Emacs."
  (require 'server)
  (unless (server-running-p)
    (server-start))
  (if (and (boundp 'server-name) server-name
           (not (equal server-name "server")))
      (concat "emacsclient -s " (shell-quote-argument server-name))
    "emacsclient"))

(defun snorg--refresh-page (pageid origin view-p)
  "Re-import the note owning PAGEID and return point to that page.
ORIGIN is the buffer point was in when the command started; VIEW-P says
whether `snorg-view-mode' was active there."
  (snorg--import (alist-get 'file_id (car (snorg--page-view pageid))))
  (when (buffer-live-p origin)
    (with-current-buffer origin
      (let ((pos (org-find-property "SNORG_PAGEID" pageid)))
        (when pos
          (goto-char pos)
          (if view-p
              (snorg--show-page)
            (org-fold-show-entry)))))))

(defun snorg--run-page-command (name args env pageid)
  "Run CLI ARGS as process NAME with ENV, then refresh PAGEID's note.
The refresh restores point (and the review, when active) in the
current buffer."
  (let ((origin (current-buffer))
        (view-p (bound-and-true-p snorg-view-mode)))
    (snorg--run-async name args env
                      (lambda () (snorg--refresh-page pageid origin view-p)))))

;;;###autoload
(defun snorg-analyze-edit ()
  "Edit the transcription of the page heading at point.
Run `snorg analyze-edit' on the heading's :SNORG_PAGEID:, opening the
transcription in this Emacs via emacsclient; finish editing with
\\[server-edit] (`C-x #').  The edit survives re-analysis, and the note's
org subtree is refreshed in place.  Works on a never-analyzed page too:
the buffer opens empty and the text you save becomes a hand transcription."
  (interactive)
  (let ((pageid (snorg--page-id-at-point))
        (editor (or snorg-analyze-edit-editor (snorg--emacsclient-editor))))
    ;; Must be async: a blocking call would deadlock (the CLI waits on
    ;; emacsclient, which waits on this Emacs).
    (snorg--run-page-command "snorg-analyze-edit" (list "analyze-edit" pageid)
                             (list (concat "VISUAL=" editor)
                                   (concat "EDITOR=" editor))
                             pageid)
    (message "snorg: editing %s -- finish with C-x #" pageid)))

;;;###autoload
(defun snorg-analyze (&optional force)
  "(Re-)transcribe the page heading at point via the CLI `analyze'.
Confirm first, since analysis may spend an LLM call.  With a prefix
argument FORCE, pass `--force' to re-transcribe even an unchanged page.
Runs asynchronously; on success the note's org subtree is refreshed."
  (interactive "P")
  (let ((pageid (snorg--page-id-at-point)))
    (unless (yes-or-no-p (format "Analyze page %s? " pageid))
      (user-error "Aborted"))
    (snorg--run-page-command "snorg-analyze"
                             (append '("analyze") (and force '("--force"))
                                     (list pageid))
                             nil pageid)
    (message "snorg: analyzing %s..." pageid)))

;;;; Org link types

(defun snorg--page-svg (pageid)
  "Return the absolute SVG path of PAGEID."
  (let ((rel (alist-get 'svg (cdr (snorg--page-view pageid)))))
    (unless rel
      (user-error "snorg: page %s has no SVG" pageid))
    (snorg--archive-file rel)))

(defun snorg--follow-svg (pageid _)
  "Open the SVG of PAGEID from the archive.
The link carries the bare PAGEID -- it is unique across the archive, so
`retrieve' resolves both the owning note and the SVG path."
  (let ((svg (snorg--page-svg pageid)))
    (unless (file-exists-p svg)
      (user-error "snorg: missing SVG %s" svg))
    (find-file svg)))

(defun snorg--follow-note-page (pageid _)
  "Follow a `snorg-note:' link for PAGEID.
A PAGEID is unique across the archive, so `retrieve' resolves its owning
FILE_ID; the active backend maps that to its own note, which is opened
with point on the heading whose :SNORG_PAGEID: matches."
  (let* ((file-id (alist-get 'file_id (car (snorg--page-view pageid))))
         (backend (snorg--backend))
         (file (snorg-backend-find backend file-id)))
    (unless file
      (user-error "No %s note for snorg %s" backend file-id))
    (find-file file)
    (widen)
    (let ((pos (org-find-property "SNORG_PAGEID" pageid)))
      (if pos
          (progn
            (goto-char pos)
            (org-fold-show-entry))
        (goto-char (point-min))
        (message "snorg: page %s not found in %s" pageid file-id)))))

(defun snorg--complete-pageid ()
  "Prompt for an archived note, then one of its pages; return the PAGEID.
Pages are listed in placement order, so position is the page number."
  (let* ((n 0)
         (choices (mapcar (lambda (pid) (cons (format "page %d" (cl-incf n)) pid))
                          (snorg--note-pageids (snorg-read-file-id))))
         (sel (completing-read "Page: " choices nil t)))
    (or (cdr (assoc sel choices))
        (user-error "Unknown page: %s" sel))))

(org-link-set-parameters
 "snorg"
 :follow #'snorg--follow-svg
 :complete (lambda (&optional _) (concat "snorg:" (snorg--complete-pageid))))

(org-link-set-parameters
 "snorg-note"
 :follow #'snorg--follow-note-page
 :complete (lambda (&optional _) (concat "snorg-note:" (snorg--complete-pageid))))

;;;; Interactive review mode

(defvar-local snorg-view--window nil
  "Window showing the SVG in `snorg-view-mode'.")

(defvar-local snorg-view--saved-folds nil
  "Snapshot of the buffer's fold state saved on entering `snorg-view-mode'.
Restored on quit so review folding does not clobber the user's outline.")

(defvar-local snorg-view--return-config nil
  "Window configuration from before entering `snorg-view-mode'.
Restored on quit.")

(defvar-local snorg-view--return-point nil
  "Marker at point from before entering `snorg-view-mode'.
Restored on quit.")

(defvar-local snorg-view--was-read-only nil
  "`buffer-read-only' value from before entering `snorg-view-mode'.")

(defvar-local snorg-view--saved-header nil
  "`header-line-format' value from before entering `snorg-view-mode'.")

(defvar-local snorg-view--svg nil
  "Absolute path of the page SVG currently under review.")

(defvar-local snorg-view--diff-depth 0
  "How many git revisions back the diff overlay compares against.
0 means the plain current SVG is shown, with no overlay.")

(defvar-local snorg-view--revisions nil
  "Cached commit hashes touching `snorg-view--svg', newest-first.
Computed lazily and cleared when the reviewed page changes.")

(defvar snorg-view-diff-added-color "#0a8f0a"
  "Fill color for strokes added since the compared revision (green).")

(defvar snorg-view-diff-removed-color "#d01010"
  "Fill color for strokes removed since the compared revision (red).")

(defvar snorg-view-mode-map
  (let ((map (make-sparse-keymap)))
    (suppress-keymap map t)
    (define-key map (kbd "n")   #'snorg-view-next)
    (define-key map (kbd "p")   #'snorg-view-prev)
    (define-key map (kbd "o")   #'snorg-view-open-external)
    (define-key map (kbd "e")   #'snorg-analyze-edit)
    (define-key map (kbd "a")   #'snorg-analyze)
    (define-key map (kbd "P")   #'snorg-view-diff-older)
    (define-key map (kbd "N")   #'snorg-view-diff-newer)
    (define-key map (kbd "h")   #'snorg-view-help)
    (define-key map (kbd "?")   #'snorg-view-help)
    (define-key map (kbd "q")   #'snorg-view-quit)
    map)
  "Keymap for `snorg-view-mode'.
Printable keys are suppressed (`suppress-keymap'): the review is
strictly a snorg interaction mode, nothing self-inserts.")

(defvar snorg-view-header-line
  "snorg-view:  n/p page   e edit   a analyze   o open   P/N diff   h help   q quit"
  "Header line shown in the note buffer while `snorg-view-mode' is on.")

(define-minor-mode snorg-view-mode
  "Minor mode for the snorg dual-window page review.
Strict interaction mode: while it is on the buffer is read-only, a
header line summarizes the keys, and printable keys run review commands
instead of self-inserting (`h' lists them all)."
  :lighter " SnView"
  :keymap snorg-view-mode-map
  (if snorg-view-mode
      (setq snorg-view--was-read-only buffer-read-only
            buffer-read-only t
            snorg-view--saved-header header-line-format
            header-line-format snorg-view-header-line)
    (setq buffer-read-only snorg-view--was-read-only
          header-line-format snorg-view--saved-header
          snorg-view--was-read-only nil
          snorg-view--saved-header nil)))

(defun snorg--page-svg-at-point ()
  "Return the absolute SVG path of the page heading at point, or nil.
Read from the `SNORG_SVGP' property (set by the export template) rather
than scanning the body for a link, which body edits could remove."
  (let ((rel (org-entry-get nil "SNORG_SVGP")))
    (when (and rel (not (string-empty-p rel)))
      (snorg--archive-file rel))))

(defun snorg--goto-page-heading (direction)
  "Move point to the next page heading in DIRECTION (1 or -1).
A page heading is one carrying a :SNORG_SVGP: property.
Return non-nil on success, leaving point unchanged on failure."
  (let ((start (point)) (found nil))
    (while (and (not found)
                (if (> direction 0)
                    (outline-next-heading)
                  (outline-previous-heading)))
      (when (org-entry-get nil "SNORG_SVGP")
        (setq found t)))
    (unless found (goto-char start))
    found))

(defun snorg-view--focus ()
  "Fold the note buffer down to the page subtree at point.
Collapse every page to its heading, then reveal only the current one."
  (let ((heading (save-excursion (org-back-to-heading t) (point))))
    (org-fold-hide-sublevels 2)
    (goto-char heading)
    (org-fold-show-subtree)))

(defun snorg--show-page ()
  "Display the SVG of the page at point in the side window.
Reset the diff overlay state so page navigation drops back to the plain SVG."
  (snorg-view--focus)
  (let ((svg (snorg--page-svg-at-point)))
    (setq snorg-view--svg svg
          snorg-view--diff-depth 0
          snorg-view--revisions nil)
    (cond
     ((not svg) (message "snorg: no SVG for this page"))
     ((not (file-exists-p svg)) (message "snorg: missing SVG %s" svg))
     (t (set-window-buffer snorg-view--window (find-file-noselect svg))))))

(defun snorg-view--locate-page ()
  "Move point to the page heading `snorg-view' should start on.
Try the heading at point, then its ancestors up to the top level, then
the first heading in the buffer with a :SNORG_SVGP: property.  Signal a
`user-error' when the buffer has none."
  (let ((pos (or (save-excursion
                   (when (ignore-errors (org-back-to-heading t) t)
                     (catch 'found
                       (while t
                         (when (org-entry-get nil "SNORG_SVGP")
                           (throw 'found (point)))
                         (unless (org-up-heading-safe)
                           (throw 'found nil))))))
                 (org-find-property "SNORG_SVGP"))))
    (unless pos
      (user-error "No heading with a :SNORG_SVGP: property in this buffer"))
    (goto-char pos)))

;;;###autoload
(defun snorg-view ()
  "Open a dual-window review of the page around point.
The page heading is found from anywhere in the note: the heading at
point, else its nearest ancestor carrying :SNORG_SVGP:, else the first
page heading in the file.  The note stays on the left, folded down to
the page under review; its SVG shows on the right.  The buffer goes
read-only and plain keys drive the review -- `h' lists them; `q' quits,
restoring the folding, the window layout and point from before entry."
  (interactive)
  (setq snorg-view--return-config (current-window-configuration)
        snorg-view--return-point (point-marker))
  (snorg-view--locate-page)
  (setq snorg-view--saved-folds
        (org-fold-core-get-regions :with-markers t))
  (delete-other-windows)
  (snorg-view-mode 1)
  (setq snorg-view--window (split-window-right))
  (snorg--show-page))

(defun snorg-view-next ()
  "Move to the next page heading and refresh the SVG."
  (interactive)
  (if (snorg--goto-page-heading 1)
      (snorg--show-page)
    (message "snorg: no next page")))

(defun snorg-view-prev ()
  "Move to the previous page heading and refresh the SVG."
  (interactive)
  (if (snorg--goto-page-heading -1)
      (snorg--show-page)
    (message "snorg: no previous page")))

(defun snorg-view-quit ()
  "Leave `snorg-view-mode', restoring folds, windows and point from entry."
  (interactive)
  (snorg-view-mode -1)
  (setq snorg-view--window nil)
  (when snorg-view--saved-folds
    (org-fold-show-all)
    (org-fold-core-regions snorg-view--saved-folds :override t :clean-markers t)
    (setq snorg-view--saved-folds nil))
  ;; Grab the return state before the window configuration swaps buffers
  ;; around: the vars are buffer-local to this note buffer.
  (let ((config snorg-view--return-config)
        (pos snorg-view--return-point))
    (setq snorg-view--return-config nil
          snorg-view--return-point nil)
    (when config
      (set-window-configuration config))
    (when pos
      (when (eq (current-buffer) (marker-buffer pos))
        (goto-char pos))
      (set-marker pos nil))))

(defun snorg-view-help ()
  "Show the `snorg-view-mode' keys in the echo area."
  (interactive)
  (message "%s"
           (concat
            "snorg-view keys:\n"
            "  n          next page          p          previous page\n"
            "  e          edit the page's transcription (snorg-analyze-edit)\n"
            "  a          (re-)transcribe the page via the LLM (snorg-analyze);\n"
            "             a prefix arg (C-u a) forces re-transcription (--force)\n"
            "  o          open the page SVG in the system viewer\n"
            "  P / N      diff overlay against an older / newer git revision\n"
            "             (a numeric prefix, e.g. C-3 P, steps that many\n"
            "             revisions at once)\n"
            "  h / ?      this help\n"
            "  q          quit, restoring windows, point and folding")))

(defun snorg-view-open-external ()
  "Open the SVG under review in the system viewer via `xdg-open'.
Runs asynchronously so Emacs is not blocked."
  (interactive)
  (unless (and snorg-view--svg (file-exists-p snorg-view--svg))
    (user-error "snorg: no SVG to open"))
  (if (fboundp 'browse-url-xdg-open)
      (browse-url-xdg-open snorg-view--svg)
    (start-process "snorg-xdg-open" nil "xdg-open" snorg-view--svg)))

;;;; Version diff overlay

(defun snorg-view--git (file &rest args)
  "Run git with ARGS in FILE's directory; return stdout, or nil on failure."
  (let ((default-directory (file-name-directory file)))
    (with-temp-buffer
      (when (eq 0 (apply #'call-process "git" nil t nil args))
        (buffer-string)))))

(defun snorg-view--git-tracked-p (file)
  "Return non-nil when FILE is tracked in a git repository."
  (and (snorg-view--git file "ls-files" "--error-unmatch" "--"
                        (file-name-nondirectory file))
       t))

(defun snorg-view--revisions (file)
  "Return commit hashes touching FILE, newest-first, memoized per page."
  (or snorg-view--revisions
      (setq snorg-view--revisions
            (let ((out (snorg-view--git file "log" "--pretty=%h" "--"
                                        (file-name-nondirectory file))))
              (and out (split-string out "\n" t))))))

(defun snorg-view--rev-label (file rev)
  "Return a one-line `hash date subject' label for REV of FILE."
  (string-trim
   (or (snorg-view--git file "show" "-s" "--date=short"
                        "--format=%h %ad %s" rev)
       rev)))

(defun snorg-view--svg-at-rev (file rev)
  "Return the contents of FILE at git revision REV, or nil."
  (snorg-view--git file "show"
                   (concat rev ":./" (file-name-nondirectory file))))

(defun snorg-view--path-data (svg)
  "Return the normalized `d' string of every <path> in SVG.
Element boundaries are found with plain `search-forward' rather than a
newline-spanning regexp: `\\(?:.\\|\\n\\)*?' recurses in Emacs's regexp
engine and overflows its stack on large SVGs (a page is one 300KB+ path).
The `d' data has no quotes, so `[^\"]*' (which matches newlines) captures
the whole reflowed attribute cheaply."
  (let ((data nil))
    (with-temp-buffer
      (insert svg)
      (goto-char (point-min))
      (let ((case-fold-search nil))
        (while (search-forward "<path" nil t)
          (let ((beg (match-beginning 0))
                (end (search-forward "/>" nil t)))
            (when end
              (let ((elem (buffer-substring-no-properties beg end)))
                (when (string-match "\\bd=\"\\([^\"]*\\)\"" elem)
                  (push (replace-regexp-in-string
                         "[ \t\n]+" " " (string-trim (match-string 1 elem)))
                        data))))))))
    (nreverse data)))

(defun snorg-view--split-strokes (d)
  "Split a normalized path `d' string into per-stroke subpaths.
Supernote renders a whole page as one aggregate <path> per pen shade whose
`d' concatenates every stroke as a subpath, so the stroke — a run from one
moveto (M/m) to the next — is the diff unit, not the <path> element."
  (let ((strokes nil) (cur nil))
    (dolist (tok (split-string d " " t))
      (when (and cur (or (string= tok "M") (string= tok "m")))
        (push (string-join (nreverse cur) " ") strokes)
        (setq cur nil))
      (push tok cur))
    (when cur (push (string-join (nreverse cur) " ") strokes))
    (nreverse strokes)))

(defun snorg-view--strokes (svg)
  "Return every stroke (subpath) in SVG as a list of normalized `d' strings.
This is the stroke identity: immune to `formatSVG' indentation and to
fill/background restyling (which never touch `d')."
  (mapcan #'snorg-view--split-strokes (snorg-view--path-data svg)))

(defun snorg-view--stroke-difference (a b)
  "Return the strokes of A absent from B, in A's order.
Hash-based: a page holds thousands of strokes, too many for a list scan."
  (let ((seen (make-hash-table :test 'equal)))
    (dolist (s b) (puthash s t seen))
    (cl-remove-if (lambda (s) (gethash s seen)) a)))

(defun snorg-view--stroke-path (d color &optional opacity)
  "Return a filled <path> element drawing stroke geometry D in COLOR."
  (format "<path d=\"%s\" fill=\"%s\"%s />"
          d color
          (if opacity (format " fill-opacity=\"%s\"" opacity) "")))

(defun snorg-view--build-overlay (file rev)
  "Return an overlay SVG string comparing FILE (working tree) against REV.
Strokes present only in the current version are drawn green (added), strokes
present only in REV are drawn red (removed) and appended before </svg>;
unchanged strokes come through from the current SVG unaltered."
  (let* ((cur (with-temp-buffer
                (insert-file-contents file) (buffer-string)))
         (old (or (snorg-view--svg-at-rev file rev) ""))
         (cur-strokes (snorg-view--strokes cur))
         (old-strokes (snorg-view--strokes old))
         (added (snorg-view--stroke-difference cur-strokes old-strokes))
         (removed (snorg-view--stroke-difference old-strokes cur-strokes))
         (extra (concat
                 "\n"
                 (mapconcat
                  (lambda (s)
                    (snorg-view--stroke-path
                     s snorg-view-diff-removed-color "0.7"))
                  removed "\n")
                 "\n"
                 (mapconcat
                  (lambda (s)
                    (snorg-view--stroke-path s snorg-view-diff-added-color))
                  added "\n")
                 "\n")))
    (if (string-match "</svg>" cur)
        (replace-match (concat extra "</svg>") t t cur)
      (concat cur extra))))

(defun snorg-view--display-overlay (svg label)
  "Show overlay SVG string in the side window, titled LABEL."
  (let ((buf (get-buffer-create "*snorg-diff*")))
    (with-current-buffer buf
      (let ((inhibit-read-only t))
        (fundamental-mode)
        (erase-buffer)
        (insert svg)
        (image-mode))
      (setq header-line-format label))
    (set-window-buffer snorg-view--window buf)))

(defun snorg-view--refresh-diff ()
  "Render the overlay for the current `snorg-view--diff-depth' (0 = plain)."
  (if (<= snorg-view--diff-depth 0)
      (when (and snorg-view--svg (file-exists-p snorg-view--svg))
        (set-window-buffer snorg-view--window
                           (find-file-noselect snorg-view--svg)))
    (let* ((file snorg-view--svg)
           (revs (snorg-view--revisions file))
           (rev (nth snorg-view--diff-depth revs)))
      (snorg-view--display-overlay
       (snorg-view--build-overlay file rev)
       (format " diff %d/%d  %s"
               snorg-view--diff-depth (1- (length revs))
               (snorg-view--rev-label file rev))))))

(defun snorg-view-diff-older (&optional count)
  "Compare the current SVG against an older git revision (deeper each call).
With a numeric prefix argument COUNT, step that many revisions deeper at
once (clamped to the oldest revision)."
  (interactive "p")
  (setq count (max 1 (or count 1)))
  (unless (and snorg-view--svg (file-exists-p snorg-view--svg))
    (user-error "snorg: no SVG to diff"))
  (if (not (snorg-view--git-tracked-p snorg-view--svg))
      (message "snorg: %s is not under version control"
               (file-name-nondirectory snorg-view--svg))
    (let* ((revs (snorg-view--revisions snorg-view--svg))
           (max-depth (1- (length revs))))
      (cond
       ((< (length revs) 2)
        (message "snorg: no earlier version to compare"))
       ((>= snorg-view--diff-depth max-depth)
        (message "snorg: already at the oldest version"))
       (t
        (setq snorg-view--diff-depth (min max-depth (+ snorg-view--diff-depth count)))
        (snorg-view--refresh-diff))))))

(defun snorg-view-diff-newer (&optional count)
  "Step the diff comparison back toward the current version.
With a numeric prefix argument COUNT, step that many revisions back at
once (clamped to the current version)."
  (interactive "p")
  (setq count (max 1 (or count 1)))
  (if (<= snorg-view--diff-depth 0)
      (message "snorg: already at the current version")
    (setq snorg-view--diff-depth (max 0 (- snorg-view--diff-depth count)))
    (snorg-view--refresh-diff)))

(provide 'snorg)
;;; snorg.el ends here
