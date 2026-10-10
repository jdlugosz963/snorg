// The pandoc template filter: converts Markdown (the analysis content format) to
// any pandoc output format — org-mode (examples/emacs/orgmode.yaml), HTML,
// gfm, … It shells out to pandoc — an external PATH tool, not a
// go.mod dependency.
package export

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"

	"github.com/flosch/pongo2/v6"
)

func init() {
	if err := pongo2.RegisterFilter("pandoc", filterPandoc); err != nil {
		panic(err)
	}
}

// filterPandoc converts markdown to the pandoc output format named by its
// required parameter, e.g. {{ content|pandoc:"org" }} or pandoc:"html" (pandoc
// extensions allowed: pandoc:"org+smart"). Empty input renders empty without
// invoking pandoc, so notes with unanalyzed pages export fine on a pandoc-less
// machine as long as nothing non-empty flows through the filter. The result is
// marked safe so pongo2 emits markup (HTML) verbatim instead of escaping it.
func filterPandoc(in *pongo2.Value, param *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
	to := ""
	if param != nil && !param.IsNil() {
		to = strings.TrimSpace(param.String())
	}
	if to == "" {
		return nil, &pongo2.Error{Sender: "filter:pandoc",
			OrigError: fmt.Errorf(`missing output format, e.g. pandoc:"org"`)}
	}
	md := in.String()
	if strings.TrimSpace(md) == "" {
		return pongo2.AsValue(""), nil
	}
	out, err := markdownTo(to, md)
	if err != nil {
		return nil, &pongo2.Error{Sender: "filter:pandoc", OrigError: err}
	}
	return pongo2.AsSafeValue(out), nil
}

func markdownTo(to, md string) (string, error) {
	if _, err := exec.LookPath("pandoc"); err != nil {
		return "", fmt.Errorf("pandoc not found on PATH (required by the pandoc filter)")
	}
	// -auto_identifiers: without it every converted heading drags an id along —
	// a :PROPERTIES:/:CUSTOM_ID: drawer in org, an id= attribute in html.
	cmd := exec.Command("pandoc", "-f", "markdown-auto_identifiers", "-t", to)
	cmd.Stdin = strings.NewReader(md)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("pandoc: %w: %s", err, strings.TrimSpace(errb.String()))
	}
	return strings.TrimRight(out.String(), "\n"), nil
}
