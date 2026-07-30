package export

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/flosch/pongo2/v6"

	"github.com/jdlugosz963/snorg/internal/retrieve"
)

// templateSet renders with trim_blocks + lstrip_blocks enabled (Jinja2 parity), so
// templates can put block tags (`{% for %}`, `{% endif %}`) on their own indented lines
// without leaking a blank line or indentation into the output. Note: trim_blocks drops
// the newline after *any* `%}`, so an output line must not end with a block tag.
var templateSet = newTemplateSet()

func newTemplateSet() *pongo2.TemplateSet {
	s := pongo2.NewSet("snorg-export", pongo2.DefaultLoader)
	s.Options.TrimBlocks = true
	s.Options.LStripBlocks = true
	return s
}

// Render executes template (pongo2 syntax) once over the retrieve Result and
// returns the rendered text. The context is the whole Result (pongo2 needs a map
// root): marshalled to JSON and back into a map, so templates bind to the same
// shape retrieve emits — `notes` (the note array) and `archive` (the absolute
// root). Numbers are decoded as json.Number so integers (page number, title
// level) render as "1" rather than "1.000000". pongo2 resolves missing keys to
// empty, so pages without analysis render blank rather than erroring.
func Render(res *retrieve.Result, template string) (string, error) {
	b, err := json.Marshal(res)
	if err != nil {
		return "", fmt.Errorf("marshal result: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var ctx map[string]any
	if err := dec.Decode(&ctx); err != nil {
		return "", fmt.Errorf("build context: %w", err)
	}
	tpl, err := templateSet.FromString(template)
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}
	out, err := tpl.Execute(pongo2.Context(ctx))
	if err != nil {
		return "", fmt.Errorf("render template: %w", err)
	}
	return out, nil
}
