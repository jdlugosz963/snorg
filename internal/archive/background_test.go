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

func TestApplyBackgroundBlank(t *testing.T) {
	in := bgSVG(base64.StdEncoding.EncodeToString([]byte("bg")))
	out := string(applyBackground(in, BackgroundBlank))
	if strings.Contains(out, "data:image") {
		t.Errorf("blank kept the image:\n%s", out)
	}
	if !strings.Contains(out, `<rect x="0" y="0" width="1920" height="2560" fill="#ffffff" />`) {
		t.Errorf("blank did not insert the white rect:\n%s", out)
	}
	if !strings.Contains(out, `<path d="M 1 2 L 3 4"`) {
		t.Errorf("blank disturbed handwriting:\n%s", out)
	}
}

func TestApplyBackgroundRemove(t *testing.T) {
	in := bgSVG(base64.StdEncoding.EncodeToString([]byte("bg")))
	out := string(applyBackground(in, BackgroundRemove))
	if strings.Contains(out, "data:image") || strings.Contains(out, "<image") {
		t.Errorf("remove kept the image:\n%s", out)
	}
	if strings.Contains(out, "<rect") {
		t.Errorf("remove inserted a rect:\n%s", out)
	}
	if !strings.Contains(out, `<path d="M 1 2 L 3 4"`) {
		t.Errorf("remove disturbed handwriting:\n%s", out)
	}
}

func TestApplyBackgroundInlineNoop(t *testing.T) {
	in := bgSVG(base64.StdEncoding.EncodeToString([]byte("bg")))
	if out := applyBackground(in, BackgroundInline); !bytes.Equal(out, in) {
		t.Errorf("inline changed svg:\n%s", out)
	}
}

func TestApplyBackgroundNoImage(t *testing.T) {
	in := []byte(`<svg><path d="M 1 2" fill="#000000" /></svg>`)
	if out := applyBackground(in, BackgroundBlank); !bytes.Equal(out, in) {
		t.Errorf("blank on image-less svg changed it:\n%s", out)
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
