package export

import (
	"strings"
	"testing"
)

func TestNestHeadings(t *testing.T) {
	for _, tc := range []struct{ prefix, in, want string }{
		{"**", "* H1\ntext with * inside\n** H2\n- list item", "*** H1\ntext with * inside\n**** H2\n- list item"},
		{"##", "# H1\ntext with # inside\n## H2\n- list item", "### H1\ntext with # inside\n#### H2\n- list item"},
		{"*", "* H", "** H"},
	} {
		got, err := renderContent(t, tc.in, `{{ notes.0.pages.0.analysis.content|nestheadings:"`+tc.prefix+`" }}`)
		if err != nil {
			t.Fatal(err)
		}
		if got != tc.want {
			t.Errorf("nestheadings:%q = %q, want %q", tc.prefix, got, tc.want)
		}
	}
}

func TestNestHeadingsBadPrefix(t *testing.T) {
	for _, tpl := range []string{
		`{{ notes.0.pages.0.analysis.content|nestheadings }}`,
		`{{ notes.0.pages.0.analysis.content|nestheadings:"" }}`,
		`{{ notes.0.pages.0.analysis.content|nestheadings:"*#" }}`,
	} {
		if _, err := renderContent(t, "* H", tpl); err == nil || !strings.Contains(err.Error(), "prefix") {
			t.Errorf("%s: err = %v, want a prefix error", tpl, err)
		}
	}
}
