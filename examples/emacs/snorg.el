;;; snorg.el --- Org client for the snorg archive -*- lexical-binding: t; -*-

;; Author: Jakub Dlugosz
;; Keywords: outlines, convenience
;; Package-Requires: ((emacs "28.1") (org "9.6"))

;;; Commentary:

;; Emacs client for `snorg' (supernote-organizer).  It talks to the snorg
;; CLI (`list', `query', `retrieve', `export') and brings archived Supernote
;; notes into Emacs as org notes.  `retrieve' and `export' are
;; page-oriented (they take PAGEIDs), so the per-note helpers here first ask
;; `query note=FILE_ID' for the note's pages.
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
;; - `snorg-import' -- pick an archived note by its `source' name and import
;;   it into a backend note (created fresh, or its generated subtree refreshed
;;   in place on re-import).  `snorg-import-all' imports every archived note.
;;
;; - Org link types with `C-c C-l' completion (both defined here), each
;;   carrying one bare snorg id -- a PAGEID is unique across the archive, so the
;;   owning note is resolved on the fly: `snorg:PAGEID' opens that page's SVG
;;   from the archive; `snorg-note:PAGEID' resolves its FILE_ID through the
;;   active backend, jumps to that note and moves point to the heading whose
;;   :SNORG_PAGEID: matches.  `snorg-note:' also takes a bare FILE_ID, for a
;;   link that targets a note rather than one of its pages.
;;
;; - `snorg-view' -- a dual-window review mode: the note buffer on the left,
;;   the current page SVG on the right; the left buffer folds to just the page
;;   under review.  Entry finds the page from anywhere in the note: the
;;   heading at point, else the nearest ancestor with :SNORG_SVGP:, else the
;;   first page heading in the file.  The mode is strict -- the buffer goes
;;   read-only and printable keys are review commands, not self-insert:
;;   `n'/`p' cycle pages, `o' opens the SVG in the system viewer (xdg-open),
;;   `P'/`N' step a git diff overlay of the current page against older/newer
;;   revisions (a numeric prefix steps several at once),
;;   `h' lists the keys, and `q' quits, restoring the
;;   folding, the window layout and point from before entry.  A header line
;;   summarizes the keys.  The page SVG is read from the heading's
;;   :SNORG_SVGP: property.
;;
;; - `snorg-analyze-edit' -- edit the transcription of the page heading at
;;   point (its :SNORG_PAGEID:) via the CLI's `analyze-edit', opening it in
;;   this Emacs through emacsclient (finish with `C-x #').  Edits survive
;;   re-analysis, and the note's subtree is refreshed in place.  Also bound
;;   to `e' in `snorg-view-mode'.
;;
;; - `snorg-analyze' -- (re-)transcribe the page heading at point (its
;;   :SNORG_PAGEID:) via the CLI's `analyze', after a `yes-or-no-p' guard
;;   (it may spend an LLM call).  Runs asynchronously and refreshes the
;;   note's subtree in place.  Also bound to `a' in `snorg-view-mode'.
;;
;; - `snorg-command-map' -- an (unbound) prefix keymap gathering the interactive
;;   commands; bind it to a prefix key of your choice.
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
(require 'tabulated-list)

;; `server' is loaded lazily by `snorg--emacsclient-editor' (only that command
;; needs it), so declare its function to keep the byte-compiler quiet.
(declare-function server-running-p "server")

;; `project' is consulted opportunistically by `snorg--search-root' (it falls
;; back to a `.git' walk), so declare its function rather than requiring it.
(declare-function project-root "project" (project))

;;;; Configuration

(defgroup snorg nil
  "Org client for the snorg archive."
  :group 'org
  :prefix "snorg-")

(defvar snorg-executable nil
  "Name or path of the snorg CLI binary, or nil to resolve it per call.
A string is used verbatim -- the explicit override.  When nil,
`snorg--executable' prefers the newest locally built binary found from
`default-directory' up to the project root, falling back to \"snorg\" on
`exec-path'.  That keeps the client talking to the binary you just built
while developing the CLI itself.")

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

(defvar snorg--executable-cache (make-hash-table :test 'equal)
  "Cache of search root -> resolved snorg binary path.
Cleared by `snorg-reset-cache', which is how a rebuild is picked up.")

(defun snorg--search-root ()
  "Return the directory to search for a locally built snorg binary.
The project root of `default-directory' when one is detectable (a
`project.el' root, else the nearest ancestor holding a `.git'), otherwise
`default-directory' itself."
  (let ((dir (expand-file-name default-directory)))
    (or (and (fboundp 'project-current)
             (let ((proj (project-current nil dir)))
               (and proj (expand-file-name (project-root proj)))))
        (locate-dominating-file dir ".git")
        dir)))

(defun snorg--newest-executable (root)
  "Return the newest executable file named snorg at or under ROOT, or nil.
ROOT and each of its ancestors up to the filesystem root are *not*
searched -- only ROOT and the directories between `default-directory' and
ROOT, which is where a `go build -o' lands.  The repository directory is
itself named snorg, so `file-regular-p' is what keeps it out."
  (let ((dir (expand-file-name default-directory))
        (candidates nil))
    ;; Walk up from default-directory through ROOT, collecting candidates.
    (catch 'done
      (while t
        (let ((f (expand-file-name "snorg" dir)))
          (when (and (file-regular-p f) (file-executable-p f))
            (push f candidates)))
        (when (file-equal-p dir root) (throw 'done nil))
        (let ((up (file-name-directory (directory-file-name dir))))
          (when (or (null up) (string= up dir)) (throw 'done nil))
          (setq dir up))))
    (car (sort candidates
               (lambda (a b)
                 (time-less-p (file-attribute-modification-time
                               (file-attributes b))
                              (file-attribute-modification-time
                               (file-attributes a))))))))

(defun snorg--executable ()
  "Return the snorg CLI command to run.
`snorg-executable' wins when set.  Otherwise the newest locally built
binary between `default-directory' and the project root, memoized per
root in `snorg--executable-cache'; failing that, plain \"snorg\" so
`exec-path' resolves it."
  (or snorg-executable
      (let ((root (snorg--search-root)))
        (or (gethash root snorg--executable-cache)
            (puthash root (or (snorg--newest-executable root) "snorg")
                     snorg--executable-cache)))))

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

(defun snorg--call (&rest args)
  "Run the snorg CLI with global flags followed by ARGS.
Return stdout as a string, or signal an error with the CLI output.
Note the nil INFILE: the CLI reads PAGEIDs from stdin whenever stdin is
not a character device, and `query' answers an *empty* read with zero
matches rather than an error -- nil means /dev/null, which is a character
device, so the commands see no stdin at all."
  (with-temp-buffer
    (let ((status (apply #'call-process (snorg--executable) nil t nil
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
  "Return the PAGEIDs of FILE-ID (placement order) via `query note=FILE_ID'."
  (split-string (snorg--call "query" (concat "note=" file-id)) "\n" t))

(defvar snorg--archive-root nil
  "Absolute archive root, cached from `retrieve' output for the session.
Used to resolve archive-relative SVG paths when `snorg-archive' is nil.")

(defun snorg--parse-retrieve (json)
  "Parse retrieve JSON string, cache its archive root and return the note list.
`retrieve' emits an object {archive, notes}; the root is stashed in
`snorg--archive-root' and the `notes' array (an alist list) is returned."
  (let* ((json-object-type 'alist)
         (json-array-type 'list)
         (json-key-type 'symbol)
         (data (json-read-from-string json)))
    (setq snorg--archive-root (alist-get 'archive data))
    (alist-get 'notes data)))

(defun snorg-retrieve (file-id)
  "Return the retrieve NoteView for FILE-ID parsed as an alist.
`retrieve' takes PAGEIDs and groups them per note; passing one note's
pages yields a single-element `notes' array, whose sole NoteView is returned."
  (car (snorg--parse-retrieve
        (apply #'snorg--call "retrieve" (snorg--note-pageids file-id)))))

(defun snorg-export (file-id)
  "Return the exported org text for FILE-ID."
  (apply #'snorg--call "export" (snorg--note-pageids file-id)))

;;;; Note selection

(defvar snorg--retrieve-cache (make-hash-table :test 'equal)
  "Cache of FILE_ID -> retrieve alist for the current session.")

(defvar snorg--page-cache (make-hash-table :test 'equal)
  "Cache of PAGEID -> (NOTE-VIEW . PAGE-VIEW) for the current session.
Filled by `snorg--page-view'; kept here so `snorg-reset-cache' can clear
every cache in one place.")

(defvar snorg--note-page-count-cache (make-hash-table :test 'equal)
  "Cache of FILE_ID -> number of pages, for the page overview's \"N of M\".")

(defun snorg--retrieve-cached (file-id)
  "Return the retrieve alist for FILE-ID, caching the result."
  (or (gethash file-id snorg--retrieve-cache)
      (puthash file-id (snorg-retrieve file-id) snorg--retrieve-cache)))

(defun snorg-reset-cache ()
  "Clear every session cache: retrieves, pages, page counts, binary, root.
Also how a freshly rebuilt CLI binary is picked up, since
`snorg--executable' memoizes its search per project root."
  (interactive)
  (clrhash snorg--retrieve-cache)
  (clrhash snorg--page-cache)
  (clrhash snorg--note-page-count-cache)
  (clrhash snorg--executable-cache)
  (setq snorg--archive-root nil))

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

;;;; Page lookup

(defun snorg--page-view (pageid &optional refresh)
  "Return (NOTE-VIEW . PAGE-VIEW) for PAGEID, caching the result.
With REFRESH non-nil, re-fetch even when cached.  `retrieve' groups the
pages it is given per owning note, so a single PAGEID comes back as one
note holding one page."
  (or (and (not refresh) (gethash pageid snorg--page-cache))
      (let* ((note (car (snorg--parse-retrieve
                         (snorg--call "retrieve" pageid))))
             (page (car (alist-get 'pages note))))
        (unless page
          (error "snorg: no page %s in the archive" pageid))
        (puthash pageid (cons note page) snorg--page-cache))))

(defun snorg--note-page-count (file-id)
  "Return how many pages FILE-ID has, caching the result."
  (or (gethash file-id snorg--note-page-count-cache)
      (puthash file-id (length (snorg--note-pageids file-id))
               snorg--note-page-count-cache)))

(defun snorg--page-svg (pageid)
  "Return the absolute SVG path of PAGEID.
The retrieve `svg' key is archive-relative (FILE_ID/PAGEID.svg), so it is
resolved against the archive root."
  (let ((rel (alist-get 'svg (cdr (snorg--page-view pageid)))))
    (unless rel
      (user-error "snorg: page %s has no SVG" pageid))
    (expand-file-name rel (snorg--archive-root-for rel))))

;;;; Page overview

(defvar-local snorg-page--pageid nil
  "PAGEID the `*snorg-page*' buffer is showing.")

(defun snorg--pageid-at-point ()
  "Return the PAGEID the current snorg buffer points at.
Signal a `user-error' when the buffer has none, so every page command can
be written against this one accessor regardless of which UI invoked it."
  (or (and (derived-mode-p 'snorg-query-mode) (tabulated-list-get-id))
      (and (derived-mode-p 'snorg-page-mode) snorg-page--pageid)
      (user-error "snorg: point is not on a page")))

(defun snorg-find-page-svg ()
  "Visit the SVG of the page at point in Emacs."
  (interactive)
  (let ((svg (snorg--page-svg (snorg--pageid-at-point))))
    (unless (file-exists-p svg)
      (user-error "snorg: missing SVG %s" svg))
    (find-file svg)))

(defun snorg-open-page-svg ()
  "Open the SVG of the page at point in the system viewer."
  (interactive)
  (snorg--open-external (snorg--page-svg (snorg--pageid-at-point))))

(defun snorg-page--name (view)
  "Return the analyzed name of title/link alist VIEW, or nil."
  (alist-get 'name (alist-get 'analysis view)))

(defun snorg-page--rule (label)
  "Insert a section rule titled LABEL."
  (insert "\n"
          (propertize (concat "── " label " "
                              (make-string (max 2 (- 62 (length label))) ?─))
                      'face 'shadow)
          "\n"))

(defun snorg-page--field (label value)
  "Insert a LABEL/VALUE line, skipping it when VALUE is empty."
  (when (and value (not (string-empty-p value)))
    (insert " "
            (propertize (format "%-10s" label) 'face 'font-lock-keyword-face)
            value "\n")))

(defun snorg-page--indent (text)
  "Return TEXT with every non-empty line indented by one space.
Empty lines are left alone rather than padded into trailing whitespace."
  (replace-regexp-in-string "^\\(.\\)" " \\1"
                            (string-trim-right (or text "")) t))

(defun snorg-page--link-target (link)
  "Return a short description of LINK's target.
A note link names the target page number, which costs one extra
`retrieve' (cached); a link out of the archive resolves to nothing, so
the raw target is shown instead."
  (let ((pid (alist-get 'target_page_id link))
        (target (alist-get 'target link)))
    (cond
     (pid (let ((page (ignore-errors (cdr (snorg--page-view pid)))))
            (if page (format "p%s" (alist-get 'number page)) pid)))
     (target target)
     (t ""))))

(defun snorg-page--render (pageid &optional refresh)
  "Draw everything known about PAGEID into the current buffer.
With REFRESH non-nil, bypass the page cache."
  (let* ((pair (snorg--page-view pageid refresh))
         (note (car pair))
         (page (cdr pair))
         (analysis (alist-get 'analysis page))
         (titles (alist-get 'titles page))
         (links (alist-get 'links page))
         (regions (alist-get 'regions analysis))
         (content (alist-get 'content analysis))
         (fields (alist-get 'fields analysis))
         (inhibit-read-only t))
    (erase-buffer)
    (setq snorg-page--pageid pageid)
    (insert (propertize (or (cl-some #'snorg-page--name titles)
                            (format "Page %s" (alist-get 'number page)))
                        'face 'bold)
            (if (eq (alist-get 'starred page) t) "  ★" "")
            "\n\n")
    (snorg-page--field "note" (format "%s [%s]"
                                      (alist-get 'source note)
                                      (alist-get 'file_id note)))
    (snorg-page--field "page" (format "%s of %s"
                                      (alist-get 'number page)
                                      (snorg--note-page-count
                                       (alist-get 'file_id note))))
    (snorg-page--field "pageid" pageid)
    (snorg-page--field "svg" (or (alist-get 'svg page) ""))
    ;; `page.tags' is already the effective set; the note's own tags are shown
    ;; separately so inherited and page-local tags stay distinguishable.
    (snorg-page--field "tags" (mapconcat (lambda (tag) (concat "@" tag))
                                         (alist-get 'tags page) " "))
    (snorg-page--field "note tags" (mapconcat (lambda (tag) (concat "@" tag))
                                              (alist-get 'tags note) " "))
    (snorg-page--field "keywords" (mapconcat
                                   (lambda (kw)
                                     (concat "#" (alist-get 'text kw)))
                                   (alist-get 'keywords page) " "))
    (when titles
      (snorg-page--rule "Titles")
      (dolist (title titles)
        (insert (format "  h%s  %s\n"
                        (alist-get 'level title)
                        (or (snorg-page--name title) "(unanalyzed)")))))
    (when links
      (snorg-page--rule "Links")
      (dolist (link links)
        (let ((name (or (snorg-page--name link) (alist-get 'name link) ""))
              (target (snorg-page--link-target link)))
          ;; A web link's name *is* its URL; printing "URL → URL" is noise.
          (insert (format "  %-6s %s\n" (alist-get 'kind link)
                          (if (or (string-empty-p target) (equal name target))
                              name
                            (format "%s → %s" name target)))))))
    ;; A templated page carries its text per region and leaves `content' empty.
    (cond
     (regions
      (snorg-page--rule "Regions")
      (dolist (region regions)
        (insert (propertize (format "  %s" (alist-get 'label region))
                            'face 'bold)
                "\n"
                (snorg-page--indent (alist-get 'content region))
                "\n\n")))
     ((and content (not (string-empty-p content)))
      (snorg-page--rule "Transcription")
      (insert (snorg-page--indent content) "\n")))
    (when fields
      (snorg-page--rule "Fields")
      (dolist (field fields)
        (insert (format "  %-16s %s\n" (car field) (cdr field)))))
    (goto-char (point-min))))

(defvar snorg-page-mode-map
  (let ((map (make-sparse-keymap)))
    (define-key map (kbd "e") #'snorg-find-page-svg)
    (define-key map (kbd "E") #'snorg-open-page-svg)
    (define-key map (kbd "g") #'snorg-page-revert)
    map)
  "Keymap for `snorg-page-mode'.
`q' (bury the buffer) comes from `special-mode'.")

(define-derived-mode snorg-page-mode special-mode "SnPage"
  "Major mode for the snorg page overview buffer.
Read-only; the keys are summarized in the header line."
  (setq header-line-format
        "snorg-page:  e open svg   E system viewer   g refresh   q quit"))

(defun snorg-page-revert ()
  "Re-fetch the shown page from the archive and redraw it."
  (interactive)
  (unless snorg-page--pageid
    (user-error "snorg: nothing to refresh"))
  (snorg-page--render snorg-page--pageid t)
  (message "snorg: refreshed %s" snorg-page--pageid))

;;;###autoload
(defun snorg-show-page (&optional pageid)
  "Show everything snorg knows about PAGEID in `*snorg-page*'.
Interactively, PAGEID is the page at point in a `snorg-query' list (or
the page already shown).  The overview gathers the note, the page's
position and star, its tags and device keywords, its titles and links,
and its transcription -- per region on a templated page."
  (interactive)
  (let ((pageid (or pageid (snorg--pageid-at-point)))
        (buffer (get-buffer-create "*snorg-page*")))
    (with-current-buffer buffer
      (snorg-page-mode)
      (snorg-page--render pageid))
    (pop-to-buffer buffer)))

;;;; Query list

(defvar snorg--query-history nil
  "Minibuffer history of `snorg-query' expressions.")

(defvar-local snorg-query--expr nil
  "Query expression the `*snorg-query*' buffer is showing.")

(defun snorg--query-entries (expr)
  "Return `tabulated-list-entries' for the query EXPR.
Parses the CLI's browse-only long form (`query -l'): seven tab-separated
columns, PAGEID first.  The columns are never escaped, so the split is
strictly positional -- a value is never re-split on whitespace."
  (mapcar
   (lambda (line)
     (let ((column (split-string line "\t")))
       (list (nth 0 column)
             (vector (or (nth 1 column) "")
                     (or (nth 2 column) "")
                     (if (equal (nth 3 column) "*") "★" "")
                     (or (nth 4 column) "")
                     (or (nth 5 column) "")
                     (or (nth 6 column) "")))))
   (split-string (snorg--call "query" "-l" expr) "\n" t)))

(defun snorg-query--page< (a b)
  "Sort tabulated-list rows A and B by page number, numerically.
The column reads `pN', which sorts wrong as a string (p10 before p2)."
  (< (string-to-number (string-remove-prefix "p" (aref (cadr a) 1)))
     (string-to-number (string-remove-prefix "p" (aref (cadr b) 1)))))

(defun snorg-query--refresh ()
  "Re-run the buffer's query, refilling `tabulated-list-entries'."
  (setq tabulated-list-entries (snorg--query-entries snorg-query--expr)))

(defvar snorg-query-mode-map
  (let ((map (make-sparse-keymap)))
    (define-key map (kbd "RET") #'snorg-show-page)
    (define-key map (kbd "e")   #'snorg-find-page-svg)
    (define-key map (kbd "E")   #'snorg-open-page-svg)
    (define-key map (kbd "n")   #'next-line)
    (define-key map (kbd "p")   #'previous-line)
    (define-key map (kbd "h")   #'snorg-query-help)
    (define-key map (kbd "?")   #'snorg-query-help)
    map)
  "Keymap for `snorg-query-mode'.
`g' (re-run the query) and `q' (bury the list) come from
`tabulated-list-mode'.")

(define-derived-mode snorg-query-mode tabulated-list-mode "SnQuery"
  "Major mode listing the archive pages matched by a snorg query.
One row per page.  The PAGEID is the row *id* rather than a column --
it is long and opaque, and every command reads it back with
`tabulated-list-get-id'.  `h' lists the keys."
  (setq tabulated-list-format
        (vector '("Note" 24 t)
                (list "Page" 5 #'snorg-query--page<)
                '("★" 2 t)
                '("Headings" 40 t)
                '("Keywords" 16 t)
                '("Tags" 16 t)))
  (setq tabulated-list-padding 1)
  (add-hook 'tabulated-list-revert-hook #'snorg-query--refresh nil t)
  (tabulated-list-init-header))

(defun snorg-query-help ()
  "Show the `snorg-query-mode' keys in the echo area."
  (interactive)
  (message "%s"
           (concat
            "snorg-query keys:\n"
            "  RET        page overview -- everything snorg knows about it\n"
            "  e          visit the page SVG in Emacs\n"
            "  E          open the page SVG in the system viewer\n"
            "  n / p      next / previous page\n"
            "  g          re-run the query\n"
            "  q          bury the list\n"
            "  h / ?      this help")))

;;;###autoload
(defun snorg-query (expr)
  "List the archive pages matching the query EXPR in `*snorg-query*'.
EXPR is a snorg query expression -- terms joined by AND/OR/NOT and
grouped with parentheses, e.g. \"all\", \"starred AND tag:work\",
\"date:today\", \"content~regexp\"; see the snorg README for the full
vocabulary.  Defaults to \"all\"."
  (interactive
   (list (read-string "snorg query: " nil 'snorg--query-history "all")))
  (let ((buffer (get-buffer-create "*snorg-query*")))
    (with-current-buffer buffer
      (snorg-query-mode)
      (setq snorg-query--expr expr)
      (snorg-query--refresh)
      (tabulated-list-print)
      (when (null tabulated-list-entries)
        (message "snorg: no pages match %s" expr)))
    (pop-to-buffer buffer)))

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
         (view (snorg--retrieve-cached file-id))
         (title (snorg--title view))
         (keywords (snorg--keywords view))
         (body (snorg-export file-id))
         (existing (snorg-backend-find backend file-id))
         (path
          (or existing
              (snorg-backend-create backend file-id title keywords
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
  (let* ((ids (snorg-list))
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
  (let* ((view (car (snorg--parse-retrieve (snorg--call "retrieve" pageid))))
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
                  :command (append (list (snorg--executable))
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
               ;; Refresh when anything actually changed.  The CLI reports the
               ;; save in the change vocabulary -- "PAGEID: updated +2/-0, 1
               ;; name" -- so any text verdict other than a bare "unchanged", or
               ;; a name or region clause alongside it, means there is something
               ;; to re-read.
               (when (and snorg-analyze-edit-refresh
                          (string-match "\\(new\\|updated\\|cleared\\|reverted\\|name\\|region\\)" out))
                 (snorg--analyze-edit-refresh pageid origin view-p)))))))
      (message "snorg: editing %s -- finish with C-x #" pageid))))

;;;; Analyze (re-transcription)

;;;###autoload
(defun snorg-analyze (&optional force)
  "(Re-)transcribe the page heading at point via the CLI `analyze'.
Grab the page's PAGEID (its :SNORG_PAGEID: property, as `snorg-view'
does) and, after a `yes-or-no-p' confirmation -- since analysis may spend
an LLM call -- run `snorg analyze' on it.  With a prefix argument FORCE,
pass `--force' to re-transcribe even a page whose geometry is unchanged.
The call runs asynchronously so Emacs is not blocked; on success the
note's org subtree is refreshed in place (see `snorg-analyze-edit-refresh')."
  (interactive "P")
  (let ((pageid (snorg--page-id-at-point)))
    (unless pageid
      (user-error "Point is not on a heading with a :SNORG_PAGEID: property"))
    (unless (yes-or-no-p (format "Analyze page %s? " pageid))
      (user-error "Aborted"))
    (let* ((origin (current-buffer))
           (view-p (bound-and-true-p snorg-view-mode))
           (proc (make-process
                  :name "snorg-analyze"
                  :buffer (generate-new-buffer " *snorg-analyze*")
                  :noquery t
                  :command (append (list (snorg--executable))
                                   (snorg--global-args)
                                   (list "analyze")
                                   (and force (list "--force"))
                                   (list pageid)))))
      (set-process-sentinel
       proc
       (lambda (p _event)
         (when (memq (process-status p) '(exit signal))
           (let ((out (with-current-buffer (process-buffer p)
                        (string-trim (buffer-string)))))
             (kill-buffer (process-buffer p))
             (if (not (eq (process-exit-status p) 0))
                 (message "snorg analyze failed: %s"
                          (if (string-empty-p out) "(no output)" out))
               (message "snorg: %s" out)
               (when snorg-analyze-edit-refresh
                 (snorg--analyze-edit-refresh pageid origin view-p)))))))
      (message "snorg: analyzing %s..." pageid))))

;;;; Org link types

(defun snorg--archive-root-for (rel)
  "Return the absolute archive root REL resolves against.
REL is an archive-relative path.
`snorg-archive' wins when set; otherwise the root cached from `retrieve'
output; otherwise it is fetched by retrieving REL's owning note — every
archive-relative path is prefixed by its FILE_ID, so REL's first path
component names the note to retrieve."
  (or (and snorg-archive (expand-file-name snorg-archive))
      snorg--archive-root
      (progn (snorg--retrieve-cached (car (split-string rel "/")))
             snorg--archive-root)
      (user-error "Cannot resolve the archive root for %s" rel)))

(defun snorg--follow-svg (pageid _)
  "Open the SVG of PAGEID from the archive.
The link carries the bare PAGEID -- it is unique across the archive, so
`retrieve' (cached) resolves both the owning note and the SVG path."
  (let ((svg (snorg--page-svg pageid)))
    (unless (file-exists-p svg)
      (user-error "snorg: missing SVG %s" svg))
    (find-file svg)))

(defun snorg--note-page-id (id)
  "Split link payload ID into (FILE_ID . PAGEID), PAGEID nil for a note link.
A PAGEID (`P...') is unique across the archive, so `retrieve' (cached)
resolves the note that owns it; a FILE_ID (`F...') already names the note
and targets no page.  The prefixes are the device's own id format (see
docs/supernote-format.md), which is what lets one bare id serve both."
  (if (string-prefix-p "P" id)
      (cons (alist-get 'file_id (car (snorg--page-view id))) id)
    (cons id nil)))

(defun snorg--follow-note-page (backend id)
  "Follow a BACKEND note-page link for ID, a snorg PAGEID or FILE_ID.
BACKEND maps the owning FILE_ID to its own note; open it and, for a
PAGEID, move point to the heading whose :SNORG_PAGEID: matches.  Core
registers the generic `snorg-note:' org link type on top of this."
  (let* ((target (snorg--note-page-id id))
         (file-id (car target))
         (pageid (cdr target))
         (file (snorg-backend-find backend file-id)))
    (unless file
      (user-error "No %s note for snorg %s" backend file-id))
    (find-file file)
    (widen)
    (goto-char (point-min))
    (when pageid
      (let ((pos (org-find-property "SNORG_PAGEID" pageid)))
        (if pos
            (progn
              (goto-char pos)
              (org-fold-show-entry))
          (message "snorg: page %s not found in %s" pageid file-id))))))

(defun snorg--read-page (view prompt)
  "Prompt with PROMPT for a page of retrieve alist VIEW; return its PAGEID."
  (let* ((choices
          (mapcar (lambda (page)
                    (cons (format "page %s" (alist-get 'number page))
                          (alist-get 'page_id page)))
                  (alist-get 'pages view)))
         (sel (completing-read prompt choices nil t)))
    (or (cdr (assoc sel choices))
        (user-error "Unknown page: %s" sel))))

(defun snorg--complete-pageid ()
  "Prompt for an archived note, then one of its pages; return the PAGEID.
Both link types carry the bare PAGEID, so both build on this."
  (snorg--read-page (snorg--retrieve-cached (snorg-read-file-id)) "Page: "))

(defun snorg--complete-svg ()
  "Completion for `snorg:' links: pick a note, then a page."
  (concat "snorg:" (snorg--complete-pageid)))

(defun snorg--complete-note-page ()
  "Completion for `snorg-note:' links: pick a note, then a page."
  (concat "snorg-note:" (snorg--complete-pageid)))

(org-link-set-parameters "snorg"
                         :follow #'snorg--follow-svg
                         :complete #'snorg--complete-svg)

;; One backend-agnostic note-page link type.  The link stores one bare snorg id
;; (not any backend's id): a PAGEID, which the archive resolves to its owning
;; FILE_ID, or a FILE_ID when the link targets a note rather than a page.  Either
;; way whichever backend is active maps that FILE_ID to its own note -- backends
;; no longer register their own link type.
(org-link-set-parameters
 "snorg-note"
 :follow (lambda (path _) (snorg--follow-note-page (snorg--backend) path))
 :complete #'snorg--complete-note-page)

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
      (expand-file-name rel (snorg--archive-root-for rel)))))

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

;;;; Open externally

(defun snorg--open-external (file)
  "Hand FILE to the system viewer via `xdg-open'.
Runs asynchronously so Emacs is not blocked.  This is the `dired-do-open'
equivalent for the Emacs 28 baseline this package targets, where that
command does not exist yet."
  (unless (and file (file-exists-p file))
    (user-error "snorg: no such file to open: %s" (or file "")))
  (if (fboundp 'browse-url-xdg-open)
      (browse-url-xdg-open file)
    (start-process "snorg-xdg-open" nil "xdg-open" file)))

(defun snorg-view-open-external ()
  "Open the SVG under review in the system viewer."
  (interactive)
  (unless snorg-view--svg
    (user-error "snorg: no SVG to open"))
  (snorg--open-external snorg-view--svg))

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

;;;; Command keymap

;;;###autoload (autoload 'snorg-command-map "snorg" nil t 'keymap)
(defvar snorg-command-map
  (let ((map (make-sparse-keymap)))
    (define-key map "i" #'snorg-import)
    (define-key map "I" #'snorg-import-all)
    (define-key map "v" #'snorg-view)
    (define-key map "e" #'snorg-analyze-edit)
    (define-key map "a" #'snorg-analyze)
    (define-key map "q" #'snorg-query)
    (define-key map "r" #'snorg-reset-cache)
    map)
  "Prefix keymap gathering the interactive snorg commands.
It is left unbound; bind it to a prefix key of your choice, e.g.
  (define-key global-map (kbd \"C-c n\") \\='snorg-command-map)")
(fset 'snorg-command-map snorg-command-map)

(provide 'snorg)
;;; snorg.el ends here
