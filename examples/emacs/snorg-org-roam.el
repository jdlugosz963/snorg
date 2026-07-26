;;; snorg-org-roam.el --- org-roam backend for snorg -*- lexical-binding: t; -*-

;; Author: Jakub Dlugosz
;; Keywords: outlines, convenience
;; Package-Requires: ((emacs "28.1") (org-roam "2.2"))

;;; Commentary:

;; snorg backend storing imported notes as org-roam nodes.  This backend owns
;; the snorg-id <-> node translation, keeping the node a perfectly ordinary
;; org-roam node: the `:ID:' is a native org id (`org-id-new', a UUID) and the
;; file is named in org-roam's native `<timestamp>-slug.org' style -- no
;; snorg-derived id is forced into either.  The snorg identity is carried
;; instead as a ROAM_REF, `snorg:FILE_ID', which org-roam indexes; re-import
;; resolves the note through it (`org-roam-node-from-ref').  Core hands this
;; backend the raw FILE_ID and never sees the node's `:ID:'.
;;
;; (require 'snorg-org-roam) selects this backend when none is set yet;
;; see `snorg-backend'.

;;; Code:

(require 'snorg)
(require 'org-roam)
(require 'org-id)

(defun snorg-org-roam--ref (file-id)
  "Return the ROAM_REF carrying snorg FILE-ID."
  (concat "snorg:" file-id))

(cl-defmethod snorg-backend-find ((_backend (eql 'org-roam)) file-id)
  "Return the file of the org-roam node for snorg FILE-ID, or nil.
Resolved through the `snorg:FILE_ID' ROAM_REF the note was created with."
  (let ((node (org-roam-node-from-ref (snorg-org-roam--ref file-id))))
    (and node (org-roam-node-file node))))

(defun snorg-org-roam--slug (title)
  "Return a filename slug for TITLE (lowercase, non-alphanumerics to `-')."
  (string-trim (replace-regexp-in-string "[^[:alnum:]]+" "-" (downcase title))
               "-+" "-+"))

(defun snorg-org-roam--filetags (keywords)
  "Return a `#+filetags:' line for KEYWORDS, or nil when empty.
Characters org tags cannot contain are replaced with `_'."
  (when keywords
    (format "#+filetags: :%s:\n"
            (mapconcat
             (lambda (kw)
               (replace-regexp-in-string "[^[:alnum:]_@#%]" "_" kw))
             keywords ":"))))

(cl-defmethod snorg-backend-create ((_backend (eql 'org-roam)) file-id
                                    title keywords directory)
  "Create an org-roam node for snorg FILE-ID and return its path.
An ordinary org-roam node: a native `:ID:' (`org-id-new') and a native
`<timestamp>-slug.org' filename in DIRECTORY (nil means
`org-roam-directory').  The snorg identity is carried by a `snorg:FILE_ID'
ROAM_REF -- that is what `snorg-backend-find' and re-import resolve by.
The file is registered in the org-roam database immediately so the ref is
indexed and the find sees it."
  (let* ((dir (or directory org-roam-directory))
         (path (expand-file-name
                (concat (format-time-string "%Y%m%d%H%M%S") "-"
                        (snorg-org-roam--slug title) ".org")
                dir)))
    (make-directory dir t)
    (with-temp-file path
      (insert ":PROPERTIES:\n"
              ":ID:       " (org-id-new) "\n"
              ":ROAM_REFS: " (snorg-org-roam--ref file-id) "\n"
              ":END:\n"
              "#+title: " title "\n"
              (or (snorg-org-roam--filetags keywords) "")))
    (org-roam-db-update-file path)
    path))

(unless snorg-backend (setq snorg-backend 'org-roam))

(provide 'snorg-org-roam)
;;; snorg-org-roam.el ends here
