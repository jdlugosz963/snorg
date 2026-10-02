package analyze

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/jdlugosz963/snorg/internal/archive"
	"github.com/jdlugosz963/snorg/internal/snote"
)

// templateImgPath is the fixed on-disk image a test template points at; its bytes
// (and thus its hash) stay constant across box-rect changes.
func templateImgPath(a *archive.Archive) string { return filepath.Join(a.Root, "bg.png") }

// bodyBox is an analyze box whose prompt keys the fake's "region" reply.
func bodyBox(id string, r snote.Rect) archive.Box {
	return archive.Box{ID: id, Label: "Body", Rect: r, Analyze: true, Prompt: "Region transcription."}
}

// setTemplate injects a one-template spec (over the fixed image) with the given
// boxes, mirroring how pkg/snorg bridges the merged config into the archive.
func setTemplate(t *testing.T, a *archive.Archive, boxes ...archive.Box) {
	t.Helper()
	a.SetTemplateSpecs([]archive.TemplateSpec{{Image: templateImgPath(a), Boxes: boxes}})
}

// setupTemplated writes the template image, injects a single analyze box (boxRect)
// and stamps the page's BackgroundHash to match, so analyze takes the region path.
func setupTemplated(t *testing.T, boxRect snote.Rect) (*archive.Archive, string) {
	t.Helper()
	a := archive.New(t.TempDir())
	if _, err := a.Write(sampleNote(), map[string][]byte{"Pa": []byte(sampleSVG)}); err != nil {
		t.Fatal(err)
	}
	img := []byte("prawo-template-bytes")
	sum := sha256.Sum256(img)
	hash := hex.EncodeToString(sum[:])
	if err := os.WriteFile(templateImgPath(a), img, 0o644); err != nil {
		t.Fatal(err)
	}
	setTemplate(t, a, bodyBox("body", boxRect))

	pd, err := a.ReadPage("F_A", "Pa")
	if err != nil {
		t.Fatal(err)
	}
	pd.BackgroundHash = hash
	if _, err := a.WritePage("F_A", pd, false); err != nil {
		t.Fatal(err)
	}
	return a, hash
}

// wholePage is a box covering the entire page.
var wholePage = snote.Rect{X: 0, Y: 0, W: snote.PageWidth, H: snote.PageHeight}

func regionSpec() Spec {
	return Spec{Content: "Transcribe all text on this page.", Title: "This is a cropped title region.", Link: "This is a cropped link region."}
}

func regionReplies() map[string]string {
	return map[string]string{
		"Region transcription.":   "the region body",
		"This is a cropped title": "Essay",
		"This is a cropped link":  "link to note",
	}
}

func TestTemplatedPageWritesRegionsNotContent(t *testing.T) {
	a, _ := setupTemplated(t, wholePage)
	tr := &fakeTranscriber{replies: regionReplies()}
	res, err := Page(context.Background(), a, tr, tr, regionSpec(), "Pa", false)
	if err != nil {
		t.Fatal(err)
	}
	// A templated page's story is its region list; it has no whole-page content.
	if res.Content != nil {
		t.Errorf("content = %+v, want nil on a templated page", res.Content)
	}
	if len(res.Regions) != 1 || res.Regions[0].Text.Kind != archive.TextNew {
		t.Errorf("regions = %+v, want one %q box", res.Regions, archive.TextNew)
	}
	// The page md carries the region-section transcription...
	md, err := a.ReadAnalysisMD("F_A", "Pa")
	if err != nil {
		t.Fatal(err)
	}
	if got := archive.RegionText(archive.ParseRegions(md), "body"); got != "the region body" {
		t.Errorf(".md region body = %q", got)
	}
	// ...and no separate legacy regions sidecar is left behind.
	if _, err := os.Stat(filepath.Join(a.Root, "F_A", "Pa.regions.md")); !os.IsNotExist(err) {
		t.Error("templated page must not write a .regions.md sidecar")
	}
	pd, _ := a.ReadPage("F_A", "Pa")
	if pd.Analysis == nil || pd.Analysis.SourceHash == "" {
		t.Error("page-level source hash must still be set")
	}
	if pd.Analysis == nil || len(pd.Analysis.Regions) != 1 || pd.Analysis.Regions[0].ID != "body" || pd.Analysis.Regions[0].SourceHash == "" {
		t.Errorf("pd.Analysis.Regions = %+v", pd.Analysis)
	}
}

func TestTemplatedUnchangedPageSkips(t *testing.T) {
	a, _ := setupTemplated(t, wholePage)
	tr := &fakeTranscriber{replies: regionReplies()}
	if _, err := Page(context.Background(), a, tr, tr, regionSpec(), "Pa", false); err != nil {
		t.Fatal(err)
	}
	tr.calls = 0
	res, err := Page(context.Background(), a, tr, tr, regionSpec(), "Pa", false)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Skipped {
		t.Errorf("res = %+v, want a skipped page", res)
	}
	if tr.calls != 0 {
		t.Errorf("skipped page made %d LLM calls", tr.calls)
	}
}

// TestMovedBoxReTriggers: strokes unchanged, but the config box rect moves — the
// page-level hash still matches, yet the box must re-analyze (its fingerprint
// changed). This is the skip-path bug the template-aware skip fixes.
func TestMovedBoxReTriggers(t *testing.T) {
	a, _ := setupTemplated(t, wholePage)
	tr := &fakeTranscriber{replies: regionReplies()}
	if _, err := Page(context.Background(), a, tr, tr, regionSpec(), "Pa", false); err != nil {
		t.Fatal(err)
	}
	// Move the box to the opposite quadrant (same id), so it no longer covers the
	// stroke. Strokes are untouched; only the rect moved.
	setTemplate(t, a, bodyBox("body", snote.Rect{X: 960, Y: 1280, W: 960, H: 1280}))
	tr.calls = 0
	res, err := Page(context.Background(), a, tr, tr, regionSpec(), "Pa", false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Skipped {
		t.Error("a moved box rect must re-trigger analysis, not skip")
	}
	if tr.calls == 0 {
		t.Error("moved box did not re-transcribe")
	}
}

// TestVanishedBoxIsTombstoned: a box id dropped from the config is not deleted
// from the page md — its section moves to the end (tombstone) and stops being a
// tracked region, so its text is never silently lost.
func TestVanishedBoxIsTombstoned(t *testing.T) {
	a, _ := setupTemplated(t, wholePage)
	tr := &fakeTranscriber{replies: regionReplies()}
	if _, err := Page(context.Background(), a, tr, tr, regionSpec(), "Pa", false); err != nil {
		t.Fatal(err)
	}
	// Replace box "body" with a new box "other".
	setTemplate(t, a, bodyBox("other", wholePage))
	if _, err := Page(context.Background(), a, tr, tr, regionSpec(), "Pa", true); err != nil {
		t.Fatal(err)
	}
	md, _ := a.ReadAnalysisMD("F_A", "Pa")
	sections := archive.ParseRegions(md)
	if len(sections) != 2 || sections[0].ID != "other" || sections[1].ID != "body" {
		t.Fatalf("want [other, body(tombstone)], got %+v", sections)
	}
	if archive.RegionText(sections, "body") != "the region body" {
		t.Error("tombstoned text was lost")
	}
	pd, _ := a.ReadPage("F_A", "Pa")
	if pd.Analysis == nil || len(pd.Analysis.Regions) != 1 || pd.Analysis.Regions[0].ID != "other" {
		t.Errorf("tracked regions = %+v, want just other", pd.Analysis)
	}
}

// TestTwoBoxesGetDistinctFingerprints: with the renderer's single-<path>,
// many-subpath output, two boxes over different stroke bands must fingerprint
// different strokes — the real-note regression where every box shared the
// whole-page hash.
func TestTwoBoxesGetDistinctFingerprints(t *testing.T) {
	a := archive.New(t.TempDir())
	// One <path>, two filled shapes: one near the top, one near the bottom.
	svgTwo := svgDoc(pathEl(filledRect(100, 200, 200, 200)+" "+filledRect(100, 1800, 200, 200), `fill="#000000"`))
	if _, err := a.Write(sampleNote(), map[string][]byte{"Pa": svgTwo}); err != nil {
		t.Fatal(err)
	}
	img := []byte("two-band-template")
	sum := sha256.Sum256(img)
	hash := hex.EncodeToString(sum[:])
	if err := os.WriteFile(templateImgPath(a), img, 0o644); err != nil {
		t.Fatal(err)
	}
	setTemplate(t, a,
		bodyBox("top", snote.Rect{X: 0, Y: 0, W: 1920, H: 800}),
		bodyBox("bot", snote.Rect{X: 0, Y: 1600, W: 1920, H: 960}),
	)
	pd, _ := a.ReadPage("F_A", "Pa")
	pd.BackgroundHash = hash
	a.WritePage("F_A", pd, false)

	tr := &fakeTranscriber{replies: regionReplies()}
	if _, err := Page(context.Background(), a, tr, tr, regionSpec(), "Pa", false); err != nil {
		t.Fatal(err)
	}
	pd, _ = a.ReadPage("F_A", "Pa")
	if pd.Analysis == nil || len(pd.Analysis.Regions) != 2 {
		t.Fatalf("regions = %+v, want 2", pd.Analysis)
	}
	if pd.Analysis.Regions[0].SourceHash == pd.Analysis.Regions[1].SourceHash {
		t.Errorf("two boxes over different stroke bands share a source hash: %s", pd.Analysis.Regions[0].SourceHash)
	}
}

// regionResult returns the RegionResult reported for box id, or fails.
func regionResult(t *testing.T, res PageResult, id string) RegionResult {
	t.Helper()
	for _, r := range res.Regions {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("no region result for %q in %+v", id, res.Regions)
	return RegionResult{}
}

// TestPerRegionResults: on a two-box templated page, a fresh analysis reports both
// boxes as newly transcribed; after moving only one box's rect, re-analysis reports
// that box re-transcribed and the untouched box skipped.
func TestPerRegionResults(t *testing.T) {
	a := archive.New(t.TempDir())
	svgTwo := svgDoc(pathEl(filledRect(100, 200, 200, 200)+" "+filledRect(100, 1800, 200, 200), `fill="#000000"`))
	if _, err := a.Write(sampleNote(), map[string][]byte{"Pa": svgTwo}); err != nil {
		t.Fatal(err)
	}
	img := []byte("two-band-template")
	sum := sha256.Sum256(img)
	hash := hex.EncodeToString(sum[:])
	if err := os.WriteFile(templateImgPath(a), img, 0o644); err != nil {
		t.Fatal(err)
	}
	setTemplate(t, a,
		bodyBox("top", snote.Rect{X: 0, Y: 0, W: 1920, H: 800}),
		bodyBox("bot", snote.Rect{X: 0, Y: 1600, W: 1920, H: 960}),
	)
	pd, _ := a.ReadPage("F_A", "Pa")
	pd.BackgroundHash = hash
	a.WritePage("F_A", pd, false)

	tr := &fakeTranscriber{replies: regionReplies()}
	res, err := Page(context.Background(), a, tr, tr, regionSpec(), "Pa", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Regions) != 2 {
		t.Fatalf("first analyze regions = %+v want 2", res.Regions)
	}
	if r := regionResult(t, res, "top"); r.Text.Kind != archive.TextNew || r.Skipped || r.Blank {
		t.Errorf("fresh top = %+v, want a transcribed %q box", r, archive.TextNew)
	}
	if r := regionResult(t, res, "bot"); r.Text.Kind != archive.TextNew || r.Skipped || r.Blank {
		t.Errorf("fresh bot = %+v, want a transcribed %q box", r, archive.TextNew)
	}

	// Move only the top box (its fingerprint changes); bot stays put.
	setTemplate(t, a,
		bodyBox("top", snote.Rect{X: 0, Y: 400, W: 1920, H: 800}),
		bodyBox("bot", snote.Rect{X: 0, Y: 1600, W: 1920, H: 960}),
	)
	res, err = Page(context.Background(), a, tr, tr, regionSpec(), "Pa", false)
	if err != nil {
		t.Fatal(err)
	}
	// The moved box was re-cropped and re-transcribed; the fake returns the same
	// text, so the honest verdict is "re-transcribed, text unchanged" — which the
	// cost axis (Skipped) and the text axis (Text.Kind) now report separately.
	if r := regionResult(t, res, "top"); r.Skipped {
		t.Errorf("moved top = %+v, want it re-transcribed, not skipped", r)
	}
	if r := regionResult(t, res, "bot"); !r.Skipped {
		t.Errorf("untouched bot = %+v, want it skipped", r)
	}
}

// TestRegionEditSurvivesReanalyze: a hand edit to a region's text is preserved
// across a forced re-analysis (3-way merge), exactly like page content.
func TestRegionEditSurvivesReanalyze(t *testing.T) {
	a, _ := setupTemplated(t, wholePage)
	tr := &fakeTranscriber{replies: regionReplies()}
	if _, err := Page(context.Background(), a, tr, tr, regionSpec(), "Pa", false); err != nil {
		t.Fatal(err)
	}
	// The user edits the body region away from the AI base.
	base, err := a.ReadAnalysisMD("F_A", "Pa")
	if err != nil {
		t.Fatal(err)
	}
	edited := archive.AssembleRegions([]archive.RegionSection{{ID: "body", Label: "Body", Text: "my own words"}})
	if _, err := a.WriteAnalysisEdit("F_A", "Pa", base, edited); err != nil {
		t.Fatal(err)
	}
	// Force a re-analysis: the fresh AI text is the same "the region body", the
	// edit is on a different line, so the merge keeps the user's text.
	if _, err := Page(context.Background(), a, tr, tr, regionSpec(), "Pa", true); err != nil {
		t.Fatal(err)
	}
	got, err := a.ReadAnalysisMD("F_A", "Pa")
	if err != nil {
		t.Fatal(err)
	}
	if txt := archive.RegionText(archive.ParseRegions(got), "body"); txt != "my own words" {
		t.Errorf("user edit lost after re-analyze: body = %q", txt)
	}
}

// TestBlankBoxCostsNoCall is the saving that matters on a form template: only the
// boxes actually filled in reach the model. The unfilled one still gets its
// section and its fingerprint, so it reports Analyzed like any other box and
// skips normally on the next run.
func TestBlankBoxCostsNoCall(t *testing.T) {
	a := archive.New(t.TempDir())
	// Ink in the top band only; the bottom box is an unfilled field.
	inked := svgDoc(pathEl(filledRect(100, 200, 200, 200), `fill="#000000"`))
	if _, err := a.Write(sampleNote(), map[string][]byte{"Pa": inked}); err != nil {
		t.Fatal(err)
	}
	img := []byte("form-template")
	sum := sha256.Sum256(img)
	if err := os.WriteFile(templateImgPath(a), img, 0o644); err != nil {
		t.Fatal(err)
	}
	setTemplate(t, a,
		bodyBox("filled", snote.Rect{X: 0, Y: 0, W: 1920, H: 800}),
		bodyBox("empty", snote.Rect{X: 0, Y: 1600, W: 1920, H: 960}),
	)
	pd, _ := a.ReadPage("F_A", "Pa")
	pd.BackgroundHash = hex.EncodeToString(sum[:])
	if _, err := a.WritePage("F_A", pd, false); err != nil {
		t.Fatal(err)
	}

	tr := &fakeTranscriber{replies: regionReplies()}
	res, err := Page(context.Background(), a, tr, tr, regionSpec(), "Pa", false)
	if err != nil {
		t.Fatal(err)
	}
	var regionCalls int
	for _, p := range tr.prompts {
		if p == "Region transcription." {
			regionCalls++
		}
	}
	if regionCalls != 1 {
		t.Errorf("region calls = %d, want 1 (the blank box must not be sent), prompts %q", regionCalls, tr.prompts)
	}
	// The blank box is now visible as such, instead of being indistinguishable
	// from a box that cost a real transcription.
	if r := regionResult(t, res, "filled"); r.Blank || r.Skipped {
		t.Errorf("filled = %+v, want a transcribed box", r)
	}
	if r := regionResult(t, res, "empty"); !r.Blank {
		t.Errorf("blank = %+v, want Blank", r)
	}

	md, err := a.ReadAnalysisMD("F_A", "Pa")
	if err != nil {
		t.Fatal(err)
	}
	sections := archive.ParseRegions(md)
	if got := archive.RegionText(sections, "filled"); got != "the region body" {
		t.Errorf("filled region = %q", got)
	}
	if got := archive.RegionText(sections, "empty"); got != "" {
		t.Errorf("blank region = %q, want empty", got)
	}
	pd, _ = a.ReadPage("F_A", "Pa")
	if len(pd.Analysis.Regions) != 2 {
		t.Fatalf("regions = %+v, want a fingerprint for both boxes", pd.Analysis.Regions)
	}

	// The blank box's stored fingerprint makes the next run a plain skip.
	tr.calls, tr.prompts = 0, nil
	res, err = Page(context.Background(), a, tr, tr, regionSpec(), "Pa", false)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Skipped || res.Calls != 0 {
		t.Errorf("re-run: res = %+v, want skipped with no calls", res)
	}
}
