package analyze

import (
	"context"
	"strings"
	"testing"

	"github.com/jdlugosz963/snorg/internal/archive"
	"github.com/jdlugosz963/snorg/internal/snote"
)

// fakeTranscriber returns a canned reply per prompt prefix and records every
// prompt it saw. It implements both Transcriber and Generator.
type fakeTranscriber struct {
	replies map[string]string
	calls   int
	prompts []string
}

func (f *fakeTranscriber) Transcribe(_ context.Context, prompt string, img []byte) (string, error) {
	f.calls++
	f.prompts = append(f.prompts, prompt)
	if len(img) == 0 {
		return "", nil
	}
	return f.reply(prompt), nil
}

func (f *fakeTranscriber) Generate(_ context.Context, prompt, _ string) (string, error) {
	f.calls++
	f.prompts = append(f.prompts, prompt)
	return f.reply(prompt), nil
}

func (f *fakeTranscriber) reply(prompt string) string {
	for key, reply := range f.replies {
		if strings.HasPrefix(prompt, key) {
			return reply
		}
	}
	return ""
}

const sampleSVG = `<svg xmlns="http://www.w3.org/2000/svg" width="1920" height="2560" viewBox="0 0 1920 2560"><path d="M100 100 L300 300" stroke="black"/></svg>`

// editedSVG draws a different stroke, so the rasterized pixels (and the source
// hash) differ from sampleSVG.
const editedSVG = `<svg xmlns="http://www.w3.org/2000/svg" width="1920" height="2560" viewBox="0 0 1920 2560"><path d="M100 100 L300 300 L500 100" stroke="black"/></svg>`

func sampleNote() *snote.Note {
	return &snote.Note{
		FileID: "F_A",
		Pages: []snote.Page{{
			ID:     "Pa",
			Number: 1,
			Titles: []snote.Title{{Rect: snote.Rect{X: 100, Y: 100, W: 300, H: 200}}},
			Links:  []snote.Link{{Rect: snote.Rect{X: 50, Y: 400, W: 200, H: 120}}},
		}},
	}
}

var sampleSpec = Spec{
	Content: "Transcribe all text on this page.",
	Update:  "Update the previous transcription.",
	Title:   "This is a cropped title region.",
	Link:    "This is a cropped link region.",
	Fields:  []Field{{Name: "summary", Prompt: "Summarize the page."}},
}

func sampleReplies() map[string]string {
	return map[string]string{
		"Transcribe all text":     "  # Heading\n\npage body text  ",
		"Update the previous":     "# Heading\n\npage body text, edited",
		"This is a cropped title": "Essay",
		"This is a cropped link":  "link to note",
		"Summarize":               "  a short summary  ",
	}
}

func TestPageWritesAnalysis(t *testing.T) {
	a := archive.New(t.TempDir())
	if err := a.Write(sampleNote(), map[string][]byte{"Pa": []byte(sampleSVG)}); err != nil {
		t.Fatal(err)
	}

	tr := &fakeTranscriber{replies: sampleReplies()}
	outcome, err := Page(context.Background(), a, tr, tr, sampleSpec, "Pa", false)
	if err != nil {
		t.Fatal(err)
	}
	if outcome != Analyzed {
		t.Errorf("outcome = %q, want %q", outcome, Analyzed)
	}

	pd, err := a.ReadPage("F_A", "Pa")
	if err != nil {
		t.Fatal(err)
	}
	if pd.Analysis == nil {
		t.Fatal("analysis not written")
	}
	if pd.Analysis.SourceHash == "" {
		t.Error("source_hash not written")
	}
	content, err := a.ReadAnalysisMD("F_A", "Pa")
	if err != nil {
		t.Fatal(err)
	}
	if content != "# Heading\n\npage body text\n" {
		t.Errorf("sidecar content = %q, want trimmed markdown with one trailing newline", content)
	}
	if len(pd.Titles) != 1 || pd.Titles[0].Analysis == nil || pd.Titles[0].Analysis.Name != "Essay" {
		t.Errorf("titles = %+v", pd.Titles)
	}
	if len(pd.Links) != 1 || pd.Links[0].Analysis == nil || pd.Links[0].Analysis.Name != "link to note" {
		t.Errorf("links = %+v", pd.Links)
	}
	if pd.Analysis.Fields["summary"] != "a short summary" {
		t.Errorf("fields[summary] = %q, want trimmed %q", pd.Analysis.Fields["summary"], "a short summary")
	}
	// 1 page + 1 title + 1 link + 1 field.
	if tr.calls != 4 {
		t.Errorf("transcriber calls = %d, want 4", tr.calls)
	}
}

func TestPageSkipsWhenUnchanged(t *testing.T) {
	a := archive.New(t.TempDir())
	if err := a.Write(sampleNote(), map[string][]byte{"Pa": []byte(sampleSVG)}); err != nil {
		t.Fatal(err)
	}
	tr := &fakeTranscriber{replies: sampleReplies()}
	if _, err := Page(context.Background(), a, tr, tr, sampleSpec, "Pa", false); err != nil {
		t.Fatal(err)
	}

	tr.calls = 0
	outcome, err := Page(context.Background(), a, tr, tr, sampleSpec, "Pa", false)
	if err != nil {
		t.Fatal(err)
	}
	if outcome != Skipped {
		t.Errorf("outcome = %q, want %q", outcome, Skipped)
	}
	if tr.calls != 0 {
		t.Errorf("unchanged page made %d LLM calls, want 0", tr.calls)
	}
}

func TestPageUpdateUsesPreviousTranscription(t *testing.T) {
	a := archive.New(t.TempDir())
	if err := a.Write(sampleNote(), map[string][]byte{"Pa": []byte(sampleSVG)}); err != nil {
		t.Fatal(err)
	}
	tr := &fakeTranscriber{replies: sampleReplies()}
	if _, err := Page(context.Background(), a, tr, tr, sampleSpec, "Pa", false); err != nil {
		t.Fatal(err)
	}

	// The page changes: the update prompt must carry the previous transcription.
	if err := a.Write(sampleNote(), map[string][]byte{"Pa": []byte(editedSVG)}); err != nil {
		t.Fatal(err)
	}
	tr.prompts = nil
	outcome, err := Page(context.Background(), a, tr, tr, sampleSpec, "Pa", false)
	if err != nil {
		t.Fatal(err)
	}
	if outcome != Updated {
		t.Errorf("outcome = %q, want %q", outcome, Updated)
	}
	want := "Update the previous transcription.\n\n# Heading\n\npage body text\n"
	if len(tr.prompts) == 0 || tr.prompts[0] != want {
		t.Errorf("content prompt = %q, want %q", tr.prompts, want)
	}
	content, err := a.ReadAnalysisMD("F_A", "Pa")
	if err != nil {
		t.Fatal(err)
	}
	if content != "# Heading\n\npage body text, edited\n" {
		t.Errorf("sidecar not updated: %q", content)
	}
}

func TestPageForceReanalyzes(t *testing.T) {
	a := archive.New(t.TempDir())
	if err := a.Write(sampleNote(), map[string][]byte{"Pa": []byte(sampleSVG)}); err != nil {
		t.Fatal(err)
	}
	tr := &fakeTranscriber{replies: sampleReplies()}
	if _, err := Page(context.Background(), a, tr, tr, sampleSpec, "Pa", false); err != nil {
		t.Fatal(err)
	}

	tr.calls = 0
	outcome, err := Page(context.Background(), a, tr, tr, sampleSpec, "Pa", true)
	if err != nil {
		t.Fatal(err)
	}
	if outcome != Updated {
		t.Errorf("outcome = %q, want %q (previous transcription exists)", outcome, Updated)
	}
	if tr.calls == 0 {
		t.Error("force did not re-analyze")
	}
}

func TestPageNotFound(t *testing.T) {
	a := archive.New(t.TempDir())
	fake := &fakeTranscriber{}
	if _, err := Page(context.Background(), a, fake, fake, Spec{}, "missing", false); err == nil {
		t.Fatal("expected error for missing page")
	}
}

// TestPathHashInvariance: the fingerprint follows path geometry only, so recolor,
// background changes, formatting whitespace and link/nav overlays leave it
// unchanged, while an actual stroke edit changes it.
func TestPathHashInvariance(t *testing.T) {
	base := `<svg><path d="M100 100 L300 300" fill="#000000" /></svg>`
	h := pathHash([]byte(base))

	same := map[string]string{
		"recolor":    `<svg><path d="M100 100 L300 300" fill="#1a1aff" /></svg>`,
		"whitespace": "<svg>\n<path d=\"M100   100\n L300 300\" fill=\"#000000\" /></svg>",
		"background": `<svg><image xlink:href="data:image/png;base64,AAAA" /><path d="M100 100 L300 300" fill="#000000" /></svg>`,
		"overlay":    `<svg><path d="M100 100 L300 300" fill="#000000" /><a xlink:href="P2.svg"><rect x="0" y="0" width="10" height="10" fill="none" /></a></svg>`,
	}
	for name, s := range same {
		if pathHash([]byte(s)) != h {
			t.Errorf("%s changed the hash", name)
		}
	}
	edited := `<svg><path d="M100 100 L300 300 L500 100" fill="#000000" /></svg>`
	if pathHash([]byte(edited)) == h {
		t.Error("edited stroke did not change the hash")
	}
}

func TestRasterizeAndCrop(t *testing.T) {
	img, err := rasterize([]byte(sampleSVG))
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != pageW || b.Dy() != pageH {
		t.Fatalf("raster bounds = %v, want %dx%d", b, pageW, pageH)
	}
	png, err := crop(img, snote.Rect{X: 100, Y: 100, W: 300, H: 200})
	if err != nil {
		t.Fatal(err)
	}
	if len(png) == 0 {
		t.Error("crop produced empty PNG")
	}
}
