package analyze

import (
	"context"
	"strings"
	"testing"

	"github.com/jdlugosz963/snorg/internal/archive"
	"github.com/jdlugosz963/snorg/internal/snote"
)

// fakeTranscriber returns a canned reply per prompt and records the images it saw.
// It implements both Transcriber and Generator.
type fakeTranscriber struct {
	replies map[string]string
	calls   int
}

func (f *fakeTranscriber) Transcribe(_ context.Context, prompt string, img []byte) (string, error) {
	f.calls++
	if len(img) == 0 {
		return "", nil
	}
	return f.reply(prompt), nil
}

func (f *fakeTranscriber) Generate(_ context.Context, prompt, _ string) (string, error) {
	f.calls++
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

func TestPageWritesAnalysis(t *testing.T) {
	a := archive.New(t.TempDir())
	note := &snote.Note{
		FileID: "F_A",
		Pages: []snote.Page{{
			ID:     "Pa",
			Number: 1,
			Titles: []snote.Title{{Rect: snote.Rect{X: 100, Y: 100, W: 300, H: 200}}},
			Links:  []snote.Link{{Rect: snote.Rect{X: 50, Y: 400, W: 200, H: 120}}},
		}},
	}
	if err := a.Write(note, map[string][]byte{"Pa": []byte(sampleSVG)}); err != nil {
		t.Fatal(err)
	}

	tr := &fakeTranscriber{replies: map[string]string{
		"Transcribe all text":     "  page body text  ",
		"This is a cropped title": "Essay",
		"This is a cropped link":  "link to note",
		"Summarize":               "  a short summary  ",
	}}
	spec := Spec{
		Content: "Transcribe all text on this page.",
		Title:   "This is a cropped title region.",
		Link:    "This is a cropped link region.",
		Fields:  []Field{{Name: "summary", Prompt: "Summarize the page."}},
	}
	if err := Page(context.Background(), a, tr, tr, spec, "Pa"); err != nil {
		t.Fatal(err)
	}

	pd, err := a.ReadPage("F_A", "Pa")
	if err != nil {
		t.Fatal(err)
	}
	if pd.Analysis == nil {
		t.Fatal("analysis not written")
	}
	if pd.Analysis.Content != "page body text" {
		t.Errorf("content = %q, want trimmed %q", pd.Analysis.Content, "page body text")
	}
	if len(pd.Analysis.Titles) != 1 || pd.Analysis.Titles[0].Name != "Essay" {
		t.Errorf("titles = %+v", pd.Analysis.Titles)
	}
	if len(pd.Analysis.Links) != 1 || pd.Analysis.Links[0].Name != "link to note" {
		t.Errorf("links = %+v", pd.Analysis.Links)
	}
	if pd.Analysis.Fields["summary"] != "a short summary" {
		t.Errorf("fields[summary] = %q, want trimmed %q", pd.Analysis.Fields["summary"], "a short summary")
	}
	// 1 page + 1 title + 1 link + 1 field.
	if tr.calls != 4 {
		t.Errorf("transcriber calls = %d, want 4", tr.calls)
	}
}

func TestPageNotFound(t *testing.T) {
	a := archive.New(t.TempDir())
	fake := &fakeTranscriber{}
	if err := Page(context.Background(), a, fake, fake, Spec{}, "missing"); err == nil {
		t.Fatal("expected error for missing page")
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
