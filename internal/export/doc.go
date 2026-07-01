// Package export renders an assembled note (retrieve.NoteView) through a single
// user-supplied template, producing arbitrary text output (org-mode, markdown, html,
// …). It is the engine behind the export command.
//
// Like internal/analyze, this package is allowed an external dependency the rest of
// snorg is not: the pongo2 template engine (Jinja2-style syntax). pongo2 is isolated
// here so callers depend only on Render.
//
// The template context is the retrieve JSON verbatim. Render marshals the NoteView to
// JSON and unmarshals it into a map[string]any, so templates address fields by their
// json tags (snake_case) and walk the same nested shape as `snorg retrieve` output:
// pages, then page.titles / page.keywords / page.links / page.analysis.content.
package export
