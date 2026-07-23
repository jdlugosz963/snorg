// HTML-exclusive template filter — the analog of the org-mode `org` filter in
// orgmode.go, for exports that render the analysis content as HTML (see
// examples/web/). Like `org` it shells out to pandoc — an external PATH tool, not
// a go.mod dependency.
package export

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"

	"github.com/flosch/pongo2/v6"
)

func init() {
	if err := pongo2.RegisterFilter("html", filterHTML); err != nil {
		panic(err)
	}
}

// filterHTML converts markdown (the analysis content format) to an HTML fragment
// via `pandoc -f markdown -t html`. Empty input renders empty without invoking
// pandoc, so notes with unanalyzed pages export fine on a pandoc-less machine as
// long as nothing non-empty flows through the filter. The result is marked safe
// so pongo2 emits the HTML verbatim instead of escaping it.
func filterHTML(in *pongo2.Value, _ *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
	md := in.String()
	if strings.TrimSpace(md) == "" {
		return pongo2.AsValue(""), nil
	}
	html, err := markdownToHTML(md)
	if err != nil {
		return nil, &pongo2.Error{Sender: "filter:html", OrigError: err}
	}
	return pongo2.AsSafeValue(html), nil
}

func markdownToHTML(md string) (string, error) {
	if _, err := exec.LookPath("pandoc"); err != nil {
		return "", fmt.Errorf("pandoc not found on PATH (required by the html filter)")
	}
	cmd := exec.Command("pandoc", "-f", "markdown", "-t", "html")
	cmd.Stdin = strings.NewReader(md)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("pandoc: %w: %s", err, strings.TrimSpace(errb.String()))
	}
	return strings.TrimRight(out.String(), "\n"), nil
}
