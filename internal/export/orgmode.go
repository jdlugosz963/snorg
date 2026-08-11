// Org-mode-exclusive template filters. They only matter to users exporting into
// org-mode (see examples/emacs/orgmode.yaml); every other export format ignores
// them. The org filter shells out to pandoc — an external PATH tool, not a
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
	for name, fn := range map[string]pongo2.FilterFunction{
		"org":             filterOrg,
		"nestorgheadings": filterNestOrgHeadings,
	} {
		if err := pongo2.RegisterFilter(name, fn); err != nil {
			panic(err)
		}
	}
}

// filterOrg converts markdown (the analysis content format) to org-mode via
// `pandoc -f markdown -t org`. Empty input renders empty without invoking
// pandoc, so notes with unanalyzed pages export fine on a pandoc-less machine
// as long as nothing non-empty flows through the filter.
func filterOrg(in *pongo2.Value, _ *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
	md := in.String()
	if strings.TrimSpace(md) == "" {
		return pongo2.AsValue(""), nil
	}
	org, err := markdownToOrg(md)
	if err != nil {
		return nil, &pongo2.Error{Sender: "filter:org", OrigError: err}
	}
	return pongo2.AsSafeValue(org), nil
}

func markdownToOrg(md string) (string, error) {
	if _, err := exec.LookPath("pandoc"); err != nil {
		return "", fmt.Errorf("pandoc not found on PATH (required by the org filter)")
	}
	// -auto_identifiers: without it every converted heading drags a
	// :PROPERTIES:/:CUSTOM_ID: drawer into the org output.
	cmd := exec.Command("pandoc", "-f", "markdown-auto_identifiers", "-t", "org")
	cmd.Stdin = strings.NewReader(md)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("pandoc: %w: %s", err, strings.TrimSpace(errb.String()))
	}
	return strings.TrimRight(out.String(), "\n"), nil
}

// filterNestOrgHeadings demotes org headings by N levels: every line starting with
// '*' gets N stars prepended, e.g. {{ content|org|nestorgheadings:2 }} turns "* H"
// into "*** H" so the page's own headings nest under the template's. N defaults
// to 1 when no parameter is given.
func filterNestOrgHeadings(in *pongo2.Value, param *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
	n := 1
	if param != nil && !param.IsNil() {
		n = param.Integer()
	}
	if n < 1 {
		return nil, &pongo2.Error{Sender: "filter:nestorgheadings",
			OrigError: fmt.Errorf("level must be >= 1, got %d", n)}
	}
	stars := strings.Repeat("*", n)
	lines := strings.Split(in.String(), "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "*") {
			lines[i] = stars + line
		}
	}
	return pongo2.AsSafeValue(strings.Join(lines, "\n")), nil
}
