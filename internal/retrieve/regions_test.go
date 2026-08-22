package retrieve

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/jdlugosz963/snorg/internal/archive"
	"github.com/jdlugosz963/snorg/internal/snote"
)

func TestRetrieveExposesRegionsAndHidesTombstones(t *testing.T) {
	a := archive.New(t.TempDir())
	note := &snote.Note{FileID: "F_A", Pages: []snote.Page{{ID: "Pa", Number: 1}}}
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><path d="M1 1 L2 2"/></svg>`)
	if _, err := a.Write(note, map[string][]byte{"Pa": svg}); err != nil {
		t.Fatal(err)
	}

	img := []byte("template-image")
	sum := sha256.Sum256(img)
	hash := hex.EncodeToString(sum[:])
	imgPath := filepath.Join(a.Root, "bg.png")
	os.WriteFile(imgPath, img, 0o644)
	a.SetTemplateSpecs([]archive.TemplateSpec{{Image: imgPath, Boxes: []archive.Box{
		{ID: "title", Label: "Title", Rect: snote.Rect{X: 0, Y: 0, W: 1920, H: 400}, Analyze: true},
	}}})

	pd, _ := a.ReadPage("F_A", "Pa")
	pd.BackgroundHash = hash
	a.WritePage("F_A", pd)
	// The md has the config box plus a stale "gone" section (a tombstone).
	if _, _, err := a.MergeAnalysis("F_A", "Pa", archive.AssembleRegions([]archive.RegionSection{
		{ID: "title", Label: "Title", Text: "The Title"},
		{ID: "gone", Label: "Gone", Text: "orphaned text"},
	})); err != nil {
		t.Fatal(err)
	}

	res, err := Get(a, []string{"Pa"})
	if err != nil {
		t.Fatal(err)
	}
	an := res.Notes[0].Pages[0].Analysis
	if an == nil || len(an.Regions) != 1 {
		t.Fatalf("analysis.regions = %+v, want exactly the one config box", an)
	}
	r := an.Regions[0]
	if r.ID != "title" || r.Label != "Title" || r.Content != "The Title" {
		t.Errorf("region = %+v", r)
	}
	if want := (snote.Rect{X: 0, Y: 0, W: 1920, H: 400}); r.Rect != want {
		t.Errorf("rect = %+v, want config rect %+v", r.Rect, want)
	}
}

func TestRetrieveNonTemplatedHasNoRegions(t *testing.T) {
	a := archive.New(t.TempDir())
	note := &snote.Note{FileID: "F_A", Pages: []snote.Page{{ID: "Pa", Number: 1}}}
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><path d="M1 1 L2 2"/></svg>`)
	if _, err := a.Write(note, map[string][]byte{"Pa": svg}); err != nil {
		t.Fatal(err)
	}
	res, err := Get(a, []string{"Pa"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Notes[0].Pages[0].Analysis != nil {
		t.Error("a bare page must have no analysis/regions")
	}
}
