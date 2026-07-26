;;; snorg-denote.el --- denote backend for snorg -*- lexical-binding: t; -*-

;; Author: Jakub Dlugosz
;; Keywords: outlines, convenience
;; Package-Requires: ((emacs "28.1") (denote "3.0"))

;;; Commentary:

;; snorg backend storing imported notes as denote org notes.  This backend
;; owns the snorg-id <-> denote-id translation: the denote identifier is
;; `YYYYMMDDTHHMMSS' derived from the snorg FILE_ID (`snorg-denote--id'), so
;; the same note maps to a stable denote id every time and native `denote:'
;; links between imported notes resolve too.  Core hands this backend the raw
;; FILE_ID and never sees the denote id.
;;
;; (require 'snorg-denote) selects this backend when none is set yet;
;; see `snorg-backend'.

;;; Code:

(require 'snorg)
(require 'denote)

(defun snorg-denote--id (file-id)
  "Translate a snorg FILE-ID (or PAGEID) into a denote identifier.
Port of the CLI `denote' filter: strip a leading F/P, then format the
first 14 digits as YYYYMMDDTHHMMSS.  Return FILE-ID unchanged if too short."
  (let ((s (if (and (> (length file-id) 0) (memq (aref file-id 0) '(?F ?P)))
               (substring file-id 1)
             file-id)))
    (let ((n 0))
      (while (and (< n (length s)) (<= ?0 (aref s n) ?9))
        (setq n (1+ n)))
      (if (< n 14)
          file-id
        (concat (substring s 0 8) "T" (substring s 8 14))))))

(defun snorg-denote--id-time (file-id)
  "Return an Emacs time value decoded from snorg FILE-ID.
The denote id derived from FILE-ID (`snorg-denote--id') is YYYYMMDDTHHMMSS;
this is the creation date denote re-derives that same id from."
  (let ((d (snorg-denote--id file-id)))
    (encode-time
     (string-to-number (substring d 13 15))  ; ss
     (string-to-number (substring d 11 13))  ; mm
     (string-to-number (substring d 9 11))   ; HH
     (string-to-number (substring d 6 8))    ; DD
     (string-to-number (substring d 4 6))    ; MM
     (string-to-number (substring d 0 4))))) ; YYYY

(cl-defmethod snorg-backend-find ((_backend (eql 'denote)) file-id)
  "Return the path of the denote note for snorg FILE-ID, or nil."
  (denote-get-path-by-id (snorg-denote--id file-id)))

(cl-defmethod snorg-backend-create ((_backend (eql 'denote)) file-id
                                    title keywords directory)
  "Create a denote note for snorg FILE-ID and return its path.
The snorg id is carried via the creation date, from which denote derives
the same YYYYMMDDTHHMMSS (`snorg-denote--id').  `denote' returns the path
directly -- do not re-derive it via `denote-get-path-by-id': the fresh
note lives in an unsaved buffer and is not yet on disk, so that disk scan
returns nil.  A nil DIRECTORY lets denote pick `denote-directory'."
  (denote title keywords 'org directory (snorg-denote--id-time file-id)))

(unless snorg-backend (setq snorg-backend 'denote))

(provide 'snorg-denote)
;;; snorg-denote.el ends here
