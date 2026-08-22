package archive

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/jdlugosz963/snorg/internal/snote"
)

// writeImage drops an image file under root and returns its absolute path and
// content hash — the two inputs a TemplateSpec needs.
func writeImage(t *testing.T, root, name string, imgBytes []byte) (path, hash string) {
	t.Helper()
	path = filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, imgBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(imgBytes)
	return path, hex.EncodeToString(sum[:])
}

func TestTemplatesNoSpecsIsInert(t *testing.T) {
	a := New(t.TempDir())
	ts, err := a.Templates()
	if err != nil {
		t.Fatal(err)
	}
	if ts.MatchBackground("anything") != nil {
		t.Error("an archive with no template specs must match nothing")
	}
}

func TestTemplatesBuildAndMatch(t *testing.T) {
	root := t.TempDir()
	img, hash := writeImage(t, root, "templates/bg.png", []byte("fake-image-bytes"))
	a := New(root)
	a.SetTemplateSpecs([]TemplateSpec{{
		Image: img,
		Boxes: []Box{
			{ID: "title", Label: "Title", Rect: snote.Rect{X: 0, Y: 0, W: 1920, H: 400}, Analyze: true, Prompt: "do it"},
			{ID: "fig", Label: "Figure", Rect: snote.Rect{X: 0, Y: 400, W: 960, H: 1200}, Analyze: false},
		},
	}})
	ts, err := a.Templates()
	if err != nil {
		t.Fatal(err)
	}
	// The selector is the image file's own sha256.
	tmpl := ts.MatchBackground(hash)
	if tmpl == nil {
		t.Fatalf("no template matched hash %s", hash)
	}
	if len(tmpl.Boxes) != 2 || tmpl.Boxes[0].ID != "title" || !tmpl.Boxes[0].Analyze {
		t.Errorf("boxes = %+v", tmpl.Boxes)
	}
	if want := (snote.Rect{X: 0, Y: 0, W: 1920, H: 400}); tmpl.Boxes[0].Rect != want {
		t.Errorf("box rect = %+v, want %+v", tmpl.Boxes[0].Rect, want)
	}
	if got := tmpl.AnalyzeBoxes(); len(got) != 1 || got[0].ID != "title" {
		t.Errorf("AnalyzeBoxes = %+v, want just title", got)
	}
	if b := tmpl.Box("fig"); b == nil || b.Label != "Figure" {
		t.Errorf("Box(fig) = %+v", b)
	}
	if ts.MatchBackground("nope") != nil {
		t.Error("an unknown hash must not match")
	}
}

func TestTemplatesValidation(t *testing.T) {
	cases := map[string][]Box{
		"duplicate id": {
			{ID: "a", Rect: snote.Rect{X: 0, Y: 0, W: 100, H: 100}},
			{ID: "a", Rect: snote.Rect{X: 0, Y: 0, W: 100, H: 100}},
		},
		"rect out of range": {{ID: "a", Rect: snote.Rect{X: 0, Y: 0, W: 3000, H: 100}}},
		"non-positive size": {{ID: "a", Rect: snote.Rect{X: 0, Y: 0, W: 0, H: 100}}},
		"empty id":          {{ID: "", Rect: snote.Rect{X: 0, Y: 0, W: 100, H: 100}}},
	}
	for name, boxes := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			img, _ := writeImage(t, root, "bg.png", []byte("img"))
			a := New(root)
			a.SetTemplateSpecs([]TemplateSpec{{Image: img, Boxes: boxes}})
			if _, err := a.Templates(); err == nil {
				t.Errorf("expected a validation error for %s", name)
			}
		})
	}
}

func TestTemplatesMissingImageErrors(t *testing.T) {
	a := New(t.TempDir())
	a.SetTemplateSpecs([]TemplateSpec{{Image: filepath.Join(t.TempDir(), "gone.png")}})
	if _, err := a.Templates(); err == nil {
		t.Error("a template referencing a missing image should error")
	}
}
