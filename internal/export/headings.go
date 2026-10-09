// The nestheadings template filter: demotes headings in any format whose
// headings are a run of one marker character at line start — org (`*`),
// Markdown ATX (`#`), … No external tools: pure string rewriting.
package export

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/flosch/pongo2/v6"
)

func init() {
	if err := pongo2.RegisterFilter("nestheadings", filterNestHeadings); err != nil {
		panic(err)
	}
}

// filterNestHeadings prepends its required prefix parameter to every line
// starting with the prefix's character, e.g. {{ content|pandoc:"org"|nestheadings:"**" }}
// turns "* H" into "*** H", and {{ content|nestheadings:"##" }} turns "# H" into
// "### H", so the page's own headings nest under the template's. The prefix must
// repeat a single character; its length is the number of levels. Deliberately
// naive — it also touches such lines inside code blocks; keep that content out
// of demoted regions if that matters.
func filterNestHeadings(in *pongo2.Value, param *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
	prefix := ""
	if param != nil && !param.IsNil() {
		prefix = param.String()
	}
	mark, _ := utf8.DecodeRuneInString(prefix)
	if prefix == "" || strings.Trim(prefix, string(mark)) != "" {
		return nil, &pongo2.Error{Sender: "filter:nestheadings",
			OrigError: fmt.Errorf(`prefix must repeat one heading character, e.g. nestheadings:"**", got %q`, prefix)}
	}
	lines := strings.Split(in.String(), "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, string(mark)) {
			lines[i] = prefix + line
		}
	}
	return pongo2.AsSafeValue(strings.Join(lines, "\n")), nil
}
