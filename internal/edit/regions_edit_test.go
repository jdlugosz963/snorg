package edit

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jdlugosz963/snorg/internal/archive"
	"github.com/jdlugosz963/snorg/internal/snote"
)

// templatedPage sets up an archive with a one-box template and a matching page
// that already has a region transcription, returning the archive and page id.
func templatedPage(t *testing.T) *archive.Archive {
	t.Helper()
	a := archive.New(t.TempDir())
	note := &snote.Note{FileID: "F_A", Pages: []snote.Page{{ID: "Pa", Number: 1}}}
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><path d="M1 1 L2 2"/></svg>`)
	if err := a.Write(note, map[string][]byte{"Pa": svg}); err != nil {
		t.Fatal(err)
	}
	img := []byte("tmpl-image")
	sum := sha256.Sum256(img)
	hash := hex.EncodeToString(sum[:])
	imgPath := filepath.Join(a.Root, "bg.png")
	os.WriteFile(imgPath, img, 0o644)
	a.SetTemplateSpecs([]archive.TemplateSpec{{Image: imgPath, Boxes: []archive.Box{
		{ID: "body", Label: "Body", Rect: snote.Rect{X: 0, Y: 0, W: 1920, H: 2560}, Analyze: true},
	}}})

	pd, _ := a.ReadPage("F_A", "Pa")
	pd.BackgroundHash = hash
	pd.Analysis = &archive.PageAnalysis{Regions: []archive.RegionDoc{{ID: "body", SourceHash: "h"}}}
	a.WritePage("F_A", pd)
	// Seed an AI region transcription (no diff → the md becomes the AI base).
	if _, _, err := a.MergeAnalysis("F_A", "Pa",
		archive.AssembleRegions([]archive.RegionSection{{ID: "body", Label: "Body", Text: "ai text"}})); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestSerializeIncludesRegionSection(t *testing.T) {
	a := templatedPage(t)
	buf, err := Serialize(a, "Pa")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf, "<!-- region body (Body) -->") {
		t.Errorf("buffer lacks the region marker:\n%s", buf)
	}
	if !strings.Contains(buf, "ai text") {
		t.Errorf("buffer lacks the region text:\n%s", buf)
	}
	// A templated page has no free-form content section — the regions are the body.
	if strings.Contains(buf, "<!-- content -->") {
		t.Errorf("templated buffer must not carry a content marker:\n%s", buf)
	}
}

// A <!-- content --> marker injected into a templated buffer is a user error: the
// text under it would be silently dropped, so Apply rejects it and writes nothing.
func TestApplyRejectsContentMarkerOnTemplatedPage(t *testing.T) {
	a := templatedPage(t)
	buf, err := Serialize(a, "Pa")
	if err != nil {
		t.Fatal(err)
	}
	broken := buf + "\n<!-- content -->\nstray content\n"
	if _, _, err := Apply(a, "Pa", broken); err == nil {
		t.Fatal("expected an error for a content marker on a templated page")
	}
	// Nothing changed: the region md still holds the seeded AI text.
	md, _ := a.ReadAnalysisMD("F_A", "Pa")
	if got := archive.RegionText(archive.ParseRegions(md), "body"); got != "ai text" {
		t.Errorf("region text = %q, want the untouched seed", got)
	}
}

func TestApplyRegionEditStored(t *testing.T) {
	a := templatedPage(t)
	buf, err := Serialize(a, "Pa")
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(buf, "ai text", "hand-corrected text", 1)
	outcome, _, err := Apply(a, "Pa", edited)
	if err != nil {
		t.Fatal(err)
	}
	if outcome != Edited {
		t.Errorf("outcome = %q, want %q", outcome, Edited)
	}
	md, _ := a.ReadAnalysisMD("F_A", "Pa")
	if got := archive.RegionText(archive.ParseRegions(md), "body"); got != "hand-corrected text" {
		t.Errorf("region text = %q", got)
	}
	// The divergence from the AI base is stored as the page's edit diff.
	if _, err := os.Stat(filepath.Join(a.Root, "F_A", "Pa.md.diff")); err != nil {
		t.Errorf("expected Pa.md.diff: %v", err)
	}
}

func TestApplyRegionUnchanged(t *testing.T) {
	a := templatedPage(t)
	buf, err := Serialize(a, "Pa")
	if err != nil {
		t.Fatal(err)
	}
	outcome, _, err := Apply(a, "Pa", buf)
	if err != nil {
		t.Fatal(err)
	}
	if outcome != Unchanged {
		t.Errorf("re-saving an untouched buffer = %q, want %q", outcome, Unchanged)
	}
	if _, err := os.Stat(filepath.Join(a.Root, "F_A", "Pa.md.diff")); !os.IsNotExist(err) {
		t.Error("no edit diff should exist for an unchanged region buffer")
	}
}
