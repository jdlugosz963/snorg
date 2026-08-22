package archive

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"testing"

	"github.com/jdlugosz963/snorg/internal/snote"
)

// TestWriteStampsBackgroundHash: the page's background_hash is the sha256 of the
// decoded inline background image, stamped under every background mode (it is
// computed from the source SVG before the pipeline touches the background).
func TestWriteStampsBackgroundHash(t *testing.T) {
	imgBytes := []byte("the-template-image")
	b64 := base64.StdEncoding.EncodeToString(imgBytes)
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink">` +
		`<image xlink:href="data:image/png;base64,` + b64 + `"/>` +
		`<path d="M1 1 L2 2"/></svg>`)
	sum := sha256.Sum256(imgBytes)
	want := hex.EncodeToString(sum[:])

	note := &snote.Note{FileID: "F_A", Pages: []snote.Page{{ID: "Pa", Number: 1}}}
	for _, mode := range []BackgroundMode{BackgroundExtract, BackgroundInline, BackgroundBlank, BackgroundRemove} {
		t.Run(string(mode), func(t *testing.T) {
			a := New(t.TempDir())
			a.SVG.Background = mode
			if err := a.Write(note, map[string][]byte{"Pa": svg}); err != nil {
				t.Fatal(err)
			}
			pd, err := a.ReadPage("F_A", "Pa")
			if err != nil {
				t.Fatal(err)
			}
			if pd.BackgroundHash != want {
				t.Errorf("background_hash = %q, want %q (mode %s)", pd.BackgroundHash, want, mode)
			}
		})
	}
}

// A page with no inline background leaves the hash empty.
func TestWriteNoBackgroundLeavesHashEmpty(t *testing.T) {
	a := New(t.TempDir())
	note := &snote.Note{FileID: "F_A", Pages: []snote.Page{{ID: "Pa", Number: 1}}}
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><path d="M1 1 L2 2"/></svg>`)
	if err := a.Write(note, map[string][]byte{"Pa": svg}); err != nil {
		t.Fatal(err)
	}
	pd, _ := a.ReadPage("F_A", "Pa")
	if pd.BackgroundHash != "" {
		t.Errorf("background_hash = %q, want empty", pd.BackgroundHash)
	}
}
