package archive

import (
	"strings"
	"testing"
)

func TestFormatSVGSplitsPathCommands(t *testing.T) {
	in := `<svg><defs /><path d="M 833.5 1972.04 C 830.70 1970.88 827.58 1967.27 824.84 1962.0 L 1.0 2.0 Z" fill="#000000" /></svg>`
	got := string(formatSVG([]byte(in)))

	want := "<svg><defs />\n" +
		`<path d="M 833.5 1972.04` + "\n" +
		`         C 830.70 1970.88 827.58 1967.27 824.84 1962.0` + "\n" +
		`         L 1.0 2.0` + "\n" +
		`         Z" fill="#000000" /></svg>`
	if got != want {
		t.Errorf("formatSVG mismatch:\n got=%q\nwant=%q", got, want)
	}
}

func TestFormatSVGNoPathsUnchanged(t *testing.T) {
	in := []byte(`<svg><image xlink:href="data:image/png;base64,AAAA" /></svg>`)
	if got := formatSVG(in); string(got) != string(in) {
		t.Errorf("image-only svg changed: %q", got)
	}
}

func TestFormatSVGIdempotent(t *testing.T) {
	in := []byte(`<svg><path d="M 1.0 2.0 C 3.0 4.0 5.0 6.0 7.0 8.0" fill="#000000" /></svg>`)
	once := formatSVG(in)
	twice := formatSVG(once)
	if string(once) != string(twice) {
		t.Errorf("formatSVG not idempotent:\n once=%q\ntwice=%q", once, twice)
	}
}

func TestFormatSVGMultiplePathsEachOnOwnLine(t *testing.T) {
	in := []byte(`<svg><path d="M 1 2 L 3 4" fill="#000000" /><path d="M 5 6 L 7 8" fill="#000000" /></svg>`)
	got := string(formatSVG(in))
	if n := strings.Count(got, "\n<path "); n != 2 {
		t.Errorf("expected each <path> on its own line, got %d path-starting lines\n%s", n, got)
	}
}
