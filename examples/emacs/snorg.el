;;; snorg.el --- Org/denote client for the snorg archive -*- lexical-binding: t; -*-

;; Author: Jakub Dlugosz
;; Keywords: outlines, convenience
;; Package-Requires: ((emacs "28.1") (denote "3.0") (org "9.6"))

;;; Commentary:

;; Emacs client for `snorg' (supernote-organizer).  It talks to the snorg
;; CLI (`list', `query', `retrieve', `export') and brings archived Supernote
;; notes into Emacs as denote org notes.  `retrieve' and `export' are
;; page-oriented (they take PAGEIDs), so the per-note helpers here first ask
;; `query note' for the note's pages.
;;
;; Features:
;;
;; - `snorg-import' -- pick an archived note by its `source' name and import
;;   it into a denote note (created fresh, or its generated subtree refreshed
;;   in place on re-import).  `snorg-import-all' imports every archived note.
;;
;; - Two org link types, both with `C-c C-l' completion: `snorg:' opens a page
;;   SVG from the archive, and `denote-snorg:IDENTIFIER::PAGEID' jumps to a
;;   denote note and moves point to the heading whose :SNORG_PAGEID: matches PAGEID.
;;
;; - `snorg-view' -- a dual-window review mode: the note buffer on the left,
;;   the current page SVG on the right; the left buffer folds to just the page
;;   under review.  `M-n'/`M-p' cycle pages, `o' opens the SVG in the system
;;   viewer (xdg-open), `M-P'/`M-N' step a git diff overlay of the current page
;;   against older/newer revisions, and `q' quits (restoring the folding).
;;   The page SVG is read from the heading's :SNORG_SVGP: property.
;;
;; - `snorg-analyze-edit' -- edit the transcription of the page heading at
;;   point (its :SNORG_PAGEID:) via the CLI's `analyze-edit', opening it in
;;   this Emacs through emacsclient (finish with `C-x #').  Edits survive
;;   re-analysis, and the note's subtree is refreshed in place.  Also bound
;;   to `e' in `snorg-view-mode'.
;;
;; - `snorg-command-map' -- an (unbound) prefix keymap gathering the interactive
;;   commands; bind it to a prefix key of your choice.
;;
;; Set `snorg-archive' and `snorg-config-files' before use.  The config must
;; define `export.template' (see examples/emacs/orgmode.yaml in the snorg repo).

;;; Code:

(require 'org)
(require 'denote)
(require 'json)
(require 'subr-x)
(require 'cl-lib)

;; `server' is loaded lazily by `snorg--emacsclient-editor' (only that command
;; needs it), so declare its function to keep the byte-compiler quiet.
(declare-function server-running-p "server")

;;;; Configuration

(defgroup snorg nil
  "Org/denote client for the snorg archive."
  :group 'org
  :prefix "snorg-")

(defvar snorg-executable "snorg"
  "Name or path of the snorg CLI binary.")

(defvar snorg-archive nil
  "Path to the snorg archive, passed to the CLI as `-a'.
Must be set before any command is used.")

(defvar snorg-config-files nil
  "List of snorg config files, each passed to the CLI as `-c'.
At least one must define `export.template' for `snorg-import' to work.")

(defvar snorg-denote-directory nil
  "Destination for imported notes.
A string is used directly.  A list of strings prompts for one on
creation.  When nil, `denote-directory' is used.")

(defvar snorg-generated-heading "Generated"
  "Headline of the export root heading.
On re-import the top-level heading with this text is replaced.
Keep in sync with the export template's root heading.")

(defvar snorg-default-keywords '("snorg")
  "Denote keywords added to every imported note.
Prepended to the note's own page keywords on `snorg-import'.")

;;;; CLI / process layer

(defun snorg--global-args ()
  "Return the global CLI args (archive and config flags)."
  (unless snorg-archive
    (user-error "`snorg-archive' is not set"))
  (append (list "-a" (expand-file-name snorg-archive))
          (mapcan (lambda (f) (list "-c" (expand-file-name f)))
                  snorg-config-files)))

(defun snorg--call (&rest args)
  "Run the snorg CLI with global flags followed by ARGS.
Return stdout as a string, or signal an error with the CLI output."
  (with-temp-buffer
    (let ((status (apply #'call-process snorg-executable nil t nil
                         (append (snorg--global-args) args))))
      (unless (eq status 0)
        (error "snorg %s failed (%s): %s"
               (string-join args " ") status
               (string-trim (buffer-string))))
      (buffer-string))))

(defun snorg-list ()
  "Return the archived FILE_IDs as a list of strings."
  (split-string (snorg--call "list") "\n" t))

(defun snorg--note-pageids (file-id)
  "Return the PAGEIDs of FILE-ID (placement order) via `query note'."
  (split-string (snorg--call "query" "note" file-id) "\n" t))

(defun snorg-retrieve (file-id)
  "Return the retrieve JSON for FILE-ID parsed as an alist.
`retrieve' takes PAGEIDs and groups them per note; passing one note's
pages yields a single-element array, whose sole NoteView is returned."
  (let ((json-object-type 'alist)
        (json-array-type 'list)
        (json-key-type 'symbol))
    (car (json-read-from-string
          (apply #'snorg--call "retrieve" (snorg--note-pageids file-id))))))

(defun snorg-export (file-id)
  "Return the exported org text for FILE-ID."
  (apply #'snorg--call "export" (snorg--note-pageids file-id)))

;;;; Note selection

(defvar snorg--retrieve-cache (make-hash-table :test 'equal)
  "Cache of FILE_ID -> retrieve alist for the current session.")

(defun snorg--retrieve-cached (file-id)
  "Return the retrieve alist for FILE-ID, caching the result."
  (or (gethash file-id snorg--retrieve-cache)
      (puthash file-id (snorg-retrieve file-id) snorg--retrieve-cache)))

(defun snorg-reset-cache ()
  "Clear the retrieve cache."
  (interactive)
  (clrhash snorg--retrieve-cache))

(defun snorg--source (view)
  "Return the `source' string of retrieve alist VIEW."
  (or (alist-get 'source view) ""))

(defun snorg-read-file-id ()
  "Prompt for an archived note by its `source' name; return its FILE_ID."
  (let* ((ids (snorg-list))
         (choices
          (mapcar (lambda (id)
                    (cons (format "%s  [%s]"
                                  (snorg--source (snorg--retrieve-cached id))
                                  id)
                          id))
                  ids))
         (key (completing-read "Note: " choices nil t)))
    (or (cdr (assoc key choices))
        (user-error "Unknown note: %s" key))))

;;;; Import

(defun snorg--denote-id (id)
  "Convert a snorg FILE_ID or PAGEID into a denote identifier.
Port of the CLI `denote' filter: strip a leading F/P, then format the
first 14 digits as YYYYMMDDTHHMMSS.  Return ID unchanged if too short."
  (let ((s (if (and (> (length id) 0) (memq (aref id 0) '(?F ?P)))
               (substring id 1)
             id)))
    (let ((n 0))
      (while (and (< n (length s)) (<= ?0 (aref s n) ?9))
        (setq n (1+ n)))
      (if (< n 14)
          id
        (concat (substring s 0 8) "T" (substring s 8 14))))))

(defun snorg--id-time (id)
  "Return an Emacs time value decoded from FILE_ID/PAGEID digits in ID."
  (let ((d (snorg--denote-id id)))
    ;; d is YYYYMMDDTHHMMSS
    (encode-time
     (string-to-number (substring d 13 15))  ; ss
     (string-to-number (substring d 11 13))  ; mm
     (string-to-number (substring d 9 11))   ; HH
     (string-to-number (substring d 6 8))    ; DD
     (string-to-number (substring d 4 6))    ; MM
     (string-to-number (substring d 0 4))))) ; YYYY

(defun snorg--title (view)
  "Return the note title from retrieve alist VIEW (source without .note)."
  (let ((src (snorg--source view)))
    (if (string-suffix-p ".note" src)
        (substring src 0 (- (length src) 5))
      src)))

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
Return nil to let denote pick its default."
  (cond
   ((stringp snorg-denote-directory) snorg-denote-directory)
   ((consp snorg-denote-directory)
    (completing-read "Denote directory: " snorg-denote-directory nil t))
   (t nil)))

(defun snorg--replace-generated (body)
  "Replace the `snorg-generated-heading' subtree in the current buffer with BODY.
Insert BODY at end of buffer when no such heading exists.  BODY is the
export text (a single top-level heading subtree)."
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
   (insert (string-trim-right body) "\n")))

(defun snorg--import (file-id)
  "Import archived note FILE-ID into a denote note and return its path.
Create it fresh, or refresh its generated subtree if it already exists.
Does not display the buffer."
  (let* ((view (snorg--retrieve-cached file-id))
         (id (snorg--denote-id file-id))
         (title (snorg--title view))
         (keywords (snorg--keywords view))
         (body (snorg-export file-id))
         (existing (denote-get-path-by-id id))
         (path
          (or existing
              ;; `denote' returns the new note's path directly.  Do not
              ;; re-derive it via `denote-get-path-by-id': the fresh note
              ;; lives in an unsaved buffer and is not yet on disk, so that
              ;; disk scan returns nil and the import fails on first run.
              (denote title keywords 'org
                      (snorg--destination-directory)
                      (snorg--id-time file-id)))))
    (unless path
      (error "Failed to locate or create denote note for %s" file-id))
    (with-current-buffer (find-file-noselect path)
      (snorg--replace-generated body)
      (save-buffer))
    (message "snorg: %s note %s" (if existing "updated" "created") title)
    path))

;;;###autoload
(defun snorg-import (file-id)
  "Import archived note FILE-ID into a denote note and display it.
Create it fresh, or refresh its generated subtree if it already exists."
  (interactive (list (snorg-read-file-id)))
  (pop-to-buffer (find-file-noselect (snorg--import file-id))))

;;;###autoload
(defun snorg-import-all ()
  "Import every archived note into a denote note.
Create each fresh, or refresh its generated subtree if it already exists.
The destination directory is resolved once for the whole batch, and per-note
errors are collected and reported at the end rather than aborting the run."
  (interactive)
  (let* ((ids (snorg-list))
         (total (length ids))
         ;; Resolve (and possibly prompt for) the destination once, so the
         ;; batch does not prompt per note.
         (snorg-denote-directory (snorg--destination-directory))
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

;;;; Analyze-edit (transcription editing)

(defvar snorg-analyze-edit-editor nil
  "Editor command line passed to `snorg analyze-edit' as $VISUAL/$EDITOR.
When nil, an `emacsclient' invocation for the current Emacs server is
derived automatically, so the transcription opens in this Emacs (finish
with \\[server-edit], `C-x #').  Set this to override, e.g. for a
terminal Emacs, or a TCP/named server emacsclient cannot reach by default.")

(defvar snorg-analyze-edit-refresh t
  "When non-nil, re-import the owning note after a content-changing edit,
so the new transcription appears in the org buffer immediately.")

(defun snorg--page-id-at-point ()
  "Return the PAGEID of the page heading at point, or nil.
Read from the `SNORG_PAGEID' property set by the export template, the
same page identity `snorg-view' navigates by."
  (let ((id (org-entry-get nil "SNORG_PAGEID")))
    (and id (not (string-empty-p id)) id)))

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

(defun snorg--analyze-edit-refresh (pageid origin view-p)
  "Refresh the note owning PAGEID after an edit and restore the view.
ORIGIN is the buffer point was in when the edit started; VIEW-P says
whether `snorg-view-mode' was active there.  Reuses `snorg--import' to
regenerate the note's subtree from the freshly written transcription."
  (snorg-reset-cache)
  (let* ((json-object-type 'alist)
         (json-array-type 'list)
         (json-key-type 'symbol)
         (view (car (json-read-from-string (snorg--call "retrieve" pageid))))
         (file-id (alist-get 'file_id view)))
    (when file-id
      (snorg--import file-id)
      (when (buffer-live-p origin)
        (with-current-buffer origin
          (let ((pos (org-find-property "SNORG_PAGEID" pageid)))
            (when pos
              (goto-char pos)
              (if view-p
                  (snorg--show-page)
                (org-fold-show-entry)))))))))

;;;###autoload
(defun snorg-analyze-edit ()
  "Edit the transcription of the page heading at point.
Grab the page's PAGEID (its :SNORG_PAGEID: property, as `snorg-view'
does) and run `snorg analyze-edit', opening the transcription in this
Emacs via emacsclient; finish editing with \\[server-edit] (`C-x #').
The edit is stored so it survives re-analysis, and the note's org subtree
is refreshed in place (see `snorg-analyze-edit-refresh').  Works on a
never-analyzed page too: the buffer opens empty and the text you save
becomes a hand transcription."
  (interactive)
  (let ((pageid (snorg--page-id-at-point)))
    (unless pageid
      (user-error "Point is not on a heading with a :SNORG_PAGEID: property"))
    (let* ((editor (or snorg-analyze-edit-editor (snorg--emacsclient-editor)))
           (origin (current-buffer))
           (view-p (bound-and-true-p snorg-view-mode))
           ;; The CLI opens the temp .md through $VISUAL/$EDITOR; point both
           ;; at emacsclient so the edit lands in this Emacs.  The process must
           ;; be async: a blocking call would deadlock (the CLI waits on
           ;; emacsclient, which waits on this Emacs).
           (process-environment
            (append (list (concat "VISUAL=" editor)
                          (concat "EDITOR=" editor))
                    process-environment))
           (proc (make-process
                  :name "snorg-analyze-edit"
                  :buffer (generate-new-buffer " *snorg-analyze-edit*")
                  :noquery t
                  :command (append (list snorg-executable)
                                   (snorg--global-args)
                                   (list "analyze-edit" pageid)))))
      (set-process-sentinel
       proc
       (lambda (p _event)
         (when (memq (process-status p) '(exit signal))
           (let ((out (with-current-buffer (process-buffer p)
                        (string-trim (buffer-string)))))
             (kill-buffer (process-buffer p))
             (if (not (eq (process-exit-status p) 0))
                 (message "snorg analyze-edit failed: %s"
                          (if (string-empty-p out) "(no output)" out))
               (message "snorg: %s" out)
               (when (and snorg-analyze-edit-refresh
                          (string-match "\\(edited\\|reverted\\)\\'" out))
                 (snorg--analyze-edit-refresh pageid origin view-p)))))))
      (message "snorg: editing %s -- finish with C-x #" pageid))))

;;;; Org link types

(defun snorg--follow-svg (path _)
  "Open the archive-relative SVG PATH via `snorg-archive'."
  (unless snorg-archive
    (user-error "`snorg-archive' is not set"))
  (find-file (expand-file-name path (expand-file-name snorg-archive))))

(defun snorg--follow-denote-page (path _)
  "Follow a `denote-snorg:' link PATH of the form IDENTIFIER::PAGEID.
Open the denote note and move point to the heading whose :SNORG_PAGEID: matches."
  (let* ((parts (split-string path "::"))
         (id (car parts))
         (pageid (cadr parts))
         (file (denote-get-path-by-id id)))
    (unless file
      (user-error "No denote note with identifier %s" id))
    (find-file file)
    (widen)
    (goto-char (point-min))
    (if (and pageid (org-find-property "SNORG_PAGEID" pageid))
        (progn
          (goto-char (org-find-property "SNORG_PAGEID" pageid))
          (org-fold-show-entry))
      (when pageid
        (message "snorg: page %s not found in %s" pageid id)))))

(defun snorg--read-page (view prompt key)
  "Prompt with PROMPT for a page of retrieve alist VIEW; return its KEY value."
  (let* ((choices
          (mapcar (lambda (page)
                    (cons (format "page %s" (alist-get 'number page))
                          (alist-get key page)))
                  (alist-get 'pages view)))
         (sel (completing-read prompt choices nil t)))
    (or (cdr (assoc sel choices))
        (user-error "Unknown page: %s" sel))))

(defun snorg--complete-svg ()
  "Completion for `snorg:' links: pick a note, then a page SVG."
  (let ((view (snorg--retrieve-cached (snorg-read-file-id))))
    (concat "snorg:" (snorg--read-page view "Page: " 'svg))))

(defun snorg--complete-denote-page ()
  "Completion for `denote-snorg:' links: pick a note, then a page."
  (let* ((file-id (snorg-read-file-id))
         (view (snorg--retrieve-cached file-id)))
    (concat "denote-snorg:" (snorg--denote-id file-id)
            "::" (snorg--read-page view "Page: " 'page_id))))

(org-link-set-parameters "snorg"
                         :follow #'snorg--follow-svg
                         :complete #'snorg--complete-svg)
(org-link-set-parameters "denote-snorg"
                         :follow #'snorg--follow-denote-page
                         :complete #'snorg--complete-denote-page)

;;;; Interactive review mode

(defvar-local snorg-view--window nil
  "Window showing the SVG in `snorg-view-mode'.")

(defvar-local snorg-view--saved-folds nil
  "Snapshot of the buffer's fold state saved on entering `snorg-view-mode'.
Restored on quit so review folding does not clobber the user's outline.")

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
    (define-key map (kbd "M-n") #'snorg-view-next)
    (define-key map (kbd "M-p") #'snorg-view-prev)
    (define-key map (kbd "o")   #'snorg-view-open-external)
    (define-key map (kbd "e")   #'snorg-analyze-edit)
    (define-key map (kbd "M-P") #'snorg-view-diff-older)
    (define-key map (kbd "M-N") #'snorg-view-diff-newer)
    (define-key map (kbd "q")   #'snorg-view-quit)
    map)
  "Keymap for `snorg-view-mode'.")

(define-minor-mode snorg-view-mode
  "Minor mode for the snorg dual-window page review."
  :lighter " SnView"
  :keymap snorg-view-mode-map)

(defun snorg--page-svg-at-point ()
  "Return the absolute SVG path of the page heading at point, or nil.
Read from the `SNORG_SVGP' property (set by the export template) rather
than scanning the body for a link, which body edits could remove."
  (let ((rel (org-entry-get nil "SNORG_SVGP")))
    (when (and rel (not (string-empty-p rel)))
      (expand-file-name rel (expand-file-name snorg-archive)))))

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

;;;###autoload
(defun snorg-view ()
  "Open a dual-window review for the page heading at point.
The note stays on the left; the page SVG shows on the right.  Use
`M-n'/`M-p' to move between pages, `o' to open the SVG in the system
viewer, `e' to edit the current page's transcription (`snorg-analyze-edit'),
`M-P'/`M-N' to step a git diff overlay against older/newer revisions, and
`q' to quit."
  (interactive)
  (unless (org-entry-get nil "SNORG_SVGP")
    (user-error "Point is not on a heading with a :SNORG_SVGP: property"))
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
  "Leave `snorg-view-mode' and restore a single window."
  (interactive)
  (snorg-view-mode -1)
  (when (window-live-p snorg-view--window)
    (delete-window snorg-view--window))
  (setq snorg-view--window nil)
  (when snorg-view--saved-folds
    (org-fold-show-all)
    (org-fold-core-regions snorg-view--saved-folds :override t :clean-markers t)
    (setq snorg-view--saved-folds nil)))

;;;; Open externally

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
  (let ((out nil))
    (dolist (d (snorg-view--path-data svg))
      (dolist (s (snorg-view--split-strokes d))
        (push s out)))
    (nreverse out)))

(defun snorg-view--stroke-set (strokes)
  "Return a hash table with each string in STROKES as a key."
  (let ((h (make-hash-table :test 'equal)))
    (dolist (s strokes) (puthash s t h))
    h))

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
         (cur-set (snorg-view--stroke-set cur-strokes))
         (old-set (snorg-view--stroke-set old-strokes))
         (added (cl-remove-if (lambda (s) (gethash s old-set)) cur-strokes))
         (removed (cl-remove-if (lambda (s) (gethash s cur-set)) old-strokes))
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

(defun snorg-view--restore-svg ()
  "Put the plain page SVG file back in the side window."
  (when (and snorg-view--svg (file-exists-p snorg-view--svg))
    (set-window-buffer snorg-view--window
                       (find-file-noselect snorg-view--svg))))

(defun snorg-view--refresh-diff ()
  "Render the overlay for the current `snorg-view--diff-depth' (0 = plain)."
  (if (<= snorg-view--diff-depth 0)
      (snorg-view--restore-svg)
    (let* ((file snorg-view--svg)
           (revs (snorg-view--revisions file))
           (rev (nth snorg-view--diff-depth revs)))
      (snorg-view--display-overlay
       (snorg-view--build-overlay file rev)
       (format " diff %d/%d  %s"
               snorg-view--diff-depth (1- (length revs))
               (snorg-view--rev-label file rev))))))

(defun snorg-view-diff-older ()
  "Compare the current SVG against an older git revision (deeper each call)."
  (interactive)
  (unless (and snorg-view--svg (file-exists-p snorg-view--svg))
    (user-error "snorg: no SVG to diff"))
  (if (not (snorg-view--git-tracked-p snorg-view--svg))
      (message "snorg: %s is not under version control"
               (file-name-nondirectory snorg-view--svg))
    (let ((revs (snorg-view--revisions snorg-view--svg)))
      (if (< (length revs) 2)
          (message "snorg: no earlier version to compare")
        (if (>= snorg-view--diff-depth (1- (length revs)))
            (message "snorg: already at the oldest version")
          (setq snorg-view--diff-depth (1+ snorg-view--diff-depth))
          (snorg-view--refresh-diff))))))

(defun snorg-view-diff-newer ()
  "Step the diff comparison back toward the current version."
  (interactive)
  (if (<= snorg-view--diff-depth 0)
      (message "snorg: already at the current version")
    (setq snorg-view--diff-depth (1- snorg-view--diff-depth))
    (snorg-view--refresh-diff)))

;;;; Command keymap

;;;###autoload (autoload 'snorg-command-map "snorg" nil t 'keymap)
(defvar snorg-command-map
  (let ((map (make-sparse-keymap)))
    (define-key map "i" #'snorg-import)
    (define-key map "I" #'snorg-import-all)
    (define-key map "v" #'snorg-view)
    (define-key map "e" #'snorg-analyze-edit)
    (define-key map "r" #'snorg-reset-cache)
    map)
  "Prefix keymap gathering the interactive snorg commands.
It is left unbound; bind it to a prefix key of your choice, e.g.
  (define-key global-map (kbd \"C-c n\") \\='snorg-command-map)")
(fset 'snorg-command-map snorg-command-map)

(provide 'snorg)
;;; snorg.el ends here
