package serve

import (
	"strings"
	"testing"
)

func TestRewriteViewerLinks(t *testing.T) {
	in := `<svg>` +
		`<image xlink:href="backgrounds/abc123.png" y="0"/>` +
		`<a xlink:href="Pb.svg"><rect/></a>` +
		`<a xlink:href="../F_OTHER/Pz.svg"><rect/></a>` +
		`</svg>`
	got := string(rewriteViewerLinks([]byte(in), "F_HERE"))

	// Same-note link resolves against the owning note; cross-note keeps its FID.
	for _, want := range []string{
		`target="_top" xlink:href="/note/F_HERE?page=Pb"`,
		`target="_top" xlink:href="/note/F_OTHER?page=Pz"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	// The extracted background reference (a PNG, not a page .svg) is left alone.
	if !strings.Contains(got, `xlink:href="backgrounds/abc123.png"`) {
		t.Errorf("background href was rewritten:\n%s", got)
	}
	// No raw page-svg link should survive.
	if strings.Contains(got, `.svg"`) {
		t.Errorf("a raw .svg link survived rewriting:\n%s", got)
	}
}

func TestResponsiveSVGRoot(t *testing.T) {
	// Fixed pixel dimensions become the viewBox and are replaced by 100% so the
	// SVG scales inside the <object> instead of clipping at native size.
	got := string(responsiveSVGRoot([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="1920" height="2560"><path/></svg>`)))
	for _, want := range []string{`viewBox="0 0 1920 2560"`, `width="100%"`, `height="100%"`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, `width="1920"`) || strings.Contains(got, `height="2560"`) {
		t.Errorf("fixed pixel size survived:\n%s", got)
	}

	// An existing viewBox is preserved (not duplicated).
	got2 := string(responsiveSVGRoot([]byte(`<svg viewBox="0 0 100 200" width="100" height="200"><path/></svg>`)))
	if strings.Count(got2, "viewBox=") != 1 {
		t.Errorf("viewBox should not be duplicated:\n%s", got2)
	}
	if !strings.Contains(got2, `width="100%"`) {
		t.Errorf("width not made responsive:\n%s", got2)
	}
}
