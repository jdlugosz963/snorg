package snorg

import (
	"errors"

	"github.com/jdlugosz963/snorg/internal/archive"
)

// Sentinel errors, for errors.Is.
var (
	// ErrSchemaVersion: a file was written by another schema version; run Migrate.
	ErrSchemaVersion = archive.ErrSchemaVersion
	// ErrNotFound: an unknown PAGEID or FILE_ID.
	ErrNotFound = archive.ErrNotFound
	// ErrNoExportTemplate: Export was called with export.template unset.
	ErrNoExportTemplate = errors.New("export.template is required")
)
