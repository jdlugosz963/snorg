// Markdown-exclusive template filters — the analog of the org-mode ones in
// orgmode.go, for exports that keep the analysis content in its native Markdown
// (see examples/config.yaml). No external tools: pure string rewriting.
package export

import (
	"fmt"
	"strings"

	"github.com/flosch/pongo2/v6"
)

func init() {
	if err := pongo2.RegisterFilter("nestmdheadings", filterNestMDHeadings); err != nil {
		panic(err)
	}
}

// filterNestMDHeadings demotes Markdown ATX headings by N levels: every line
// starting with '#' gets N hashes prepended, e.g. {{ content|nestmdheadings:2 }}
// turns "# H" into "### H" so the page's own headings nest under the template's.
// N defaults to 1. Like its org counterpart it is deliberately naive — it also
// touches '#' lines inside fenced code blocks; keep such content out of demoted
// regions if that matters.
func filterNestMDHeadings(in *pongo2.Value, param *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
	n := 1
	if param != nil && !param.IsNil() {
		n = param.Integer()
	}
	if n < 1 {
		return nil, &pongo2.Error{Sender: "filter:nestmdheadings",
			OrigError: fmt.Errorf("level must be >= 1, got %d", n)}
	}
	hashes := strings.Repeat("#", n)
	lines := strings.Split(in.String(), "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "#") {
			lines[i] = hashes + line
		}
	}
	return pongo2.AsSafeValue(strings.Join(lines, "\n")), nil
}
