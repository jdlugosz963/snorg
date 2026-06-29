package archive

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"
)

func bgSVG(pngB64 string) []byte {
	return []byte(`<svg><defs /><image height="2560" width="1920" x="0" ` +
		`xlink:href="data:image/png;base64,` + pngB64 + `" y="0" />` +
		`<path d="M 1 2 L 3 4" fill="#000000" /></svg>`)
}

func TestExtractBackgroundRewritesHref(t *testing.T) {
	raw := []byte("\x89PNG fake background bytes")
	b64 := base64.StdEncoding.EncodeToString(raw)
	sum := sha256.Sum256(raw)
	wantName := hex.EncodeToString(sum[:]) + ".png"

	out, img, name, ok := extractBackground(bgSVG(b64))
	if !ok {
		t.Fatal("expected ok=true for inline-image svg")
	}
	if name != wantName {
		t.Errorf("name = %q want %q", name, wantName)
	}
	if !bytes.Equal(img, raw) {
		t.Errorf("decoded image = %q want %q", img, raw)
	}
	s := string(out)
	if !strings.Contains(s, `xlink:href="backgrounds/`+wantName+`"`) {
		t.Errorf("rewritten href missing:\n%s", s)
	}
	if strings.Contains(s, "../") {
		t.Errorf("href must not escape upward (librsvg blocks it):\n%s", s)
	}
	if strings.Contains(s, "base64") {
		t.Errorf("base64 data still present:\n%s", s)
	}
	if !strings.Contains(s, `<path d="M 1 2 L 3 4"`) {
		t.Errorf("non-image content was disturbed:\n%s", s)
	}
}

func TestExtractBackgroundNoImageUnchanged(t *testing.T) {
	in := []byte(`<svg><defs /><path d="M 1 2 L 3 4" fill="#000000" /></svg>`)
	out, img, name, ok := extractBackground(in)
	if ok {
		t.Errorf("expected ok=false for image-less svg")
	}
	if img != nil || name != "" {
		t.Errorf("expected zero img/name, got img=%v name=%q", img, name)
	}
	if !bytes.Equal(out, in) {
		t.Errorf("image-less svg changed: %q", out)
	}
}

func TestExtractBackgroundDeterministic(t *testing.T) {
	in := bgSVG(base64.StdEncoding.EncodeToString([]byte("same bytes")))
	out1, _, name1, _ := extractBackground(in)
	out2, _, name2, _ := extractBackground(in)
	if name1 != name2 || !bytes.Equal(out1, out2) {
		t.Errorf("extractBackground not deterministic: names %q/%q", name1, name2)
	}
}
