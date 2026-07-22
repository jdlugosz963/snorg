// Package export renders assembled notes (retrieve.NoteViews) through a single
// user-supplied template, producing arbitrary text output (org-mode, markdown, html,
// …). It is the engine behind the export command.
//
// Like internal/analyze, this package is allowed an external dependency the rest of
// snorg is not: the pongo2 template engine (Jinja2-style syntax). pongo2 is isolated
// here so callers depend only on Render.
//
// The template context is the retrieve JSON verbatim, under the `notes` key (pongo2
// needs a map root). Render marshals the NoteViews to JSON and unmarshals them back,
// so templates address fields by their json tags (snake_case) and walk the same
// nested shape as `snorg retrieve` output: notes, then note.pages, then
// page.titles / page.keywords / page.links / page.analysis.content. One render sees
// every retrieved note, so a template can put pages from many notes under one root.
package export
