package analyze

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jdlugosz963/snorg/internal/archive"
	"github.com/jdlugosz963/snorg/internal/snote"
)

// assertNoMD fails unless the page has no transcription sidecar at all — an empty
// one would mean "analyzed to nothing", which is not the same as having none.
func assertNoMD(t *testing.T, a *archive.Archive) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(a.Root, "F_A", "Pa.md")); !os.IsNotExist(err) {
		t.Errorf("a page with no transcription must leave no .md sidecar: %v", err)
	}
}

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

// sampleSVG is a page with handwriting on it: a closed, filled contour, the form
// every real archive SVG takes (a zero-area hairline would rasterize to no ink,
// making the page blank — see TestPageBlankSkipsTheModel).
var sampleSVG = string(pageSVG(pathEl(filledRect(100, 100, 400, 400), `fill="#000000"`)))

// editedSVG draws a bigger shape, so the rasterized pixels (and the source hash)
// differ from sampleSVG.
var editedSVG = string(pageSVG(pathEl(filledRect(100, 100, 600, 400), `fill="#000000"`)))

// blankSVG is a page nothing was drawn on.
var blankSVG = string(pageSVG())

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
	if _, err := a.Write(sampleNote(), map[string][]byte{"Pa": []byte(sampleSVG)}); err != nil {
		t.Fatal(err)
	}

	tr := &fakeTranscriber{replies: sampleReplies()}
	res, err := Page(context.Background(), a, tr, tr, sampleSpec, "Pa", false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Content == nil || res.Content.Kind != archive.TextNew {
		t.Errorf("content = %+v, want %q", res.Content, archive.TextNew)
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
	// The same four calls, reported by the result rather than counted by the fake.
	if res.Calls != 4 {
		t.Errorf("res.Calls = %d, want 4", res.Calls)
	}
	if len(res.Fields) != 1 || res.Fields[0] != "summary" {
		t.Errorf("res.Fields = %v, want [summary]", res.Fields)
	}
	if res.PageID != "Pa" {
		t.Errorf("res.PageID = %q, want Pa", res.PageID)
	}
	if !res.JSONChanged {
		t.Error("res.JSONChanged = false, want the first analysis to change the page json")
	}
	// A first analysis names both regions, and neither is a user override.
	want := []archive.NameChange{
		{Kind: "title", Index: 1, Was: "", Now: "Essay"},
		{Kind: "link", Index: 1, Was: "", Now: "link to note"},
	}
	if !reflect.DeepEqual(res.Names, want) {
		t.Errorf("res.Names = %+v, want %+v", res.Names, want)
	}
}

func TestPageSkipsWhenUnchanged(t *testing.T) {
	a := archive.New(t.TempDir())
	if _, err := a.Write(sampleNote(), map[string][]byte{"Pa": []byte(sampleSVG)}); err != nil {
		t.Fatal(err)
	}
	tr := &fakeTranscriber{replies: sampleReplies()}
	if _, err := Page(context.Background(), a, tr, tr, sampleSpec, "Pa", false); err != nil {
		t.Fatal(err)
	}

	tr.calls = 0
	res, err := Page(context.Background(), a, tr, tr, sampleSpec, "Pa", false)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Skipped {
		t.Errorf("res = %+v, want a skipped page", res)
	}
	if tr.calls != 0 {
		t.Errorf("unchanged page made %d LLM calls, want 0", tr.calls)
	}
}

func TestPageUpdateUsesPreviousTranscription(t *testing.T) {
	a := archive.New(t.TempDir())
	if _, err := a.Write(sampleNote(), map[string][]byte{"Pa": []byte(sampleSVG)}); err != nil {
		t.Fatal(err)
	}
	tr := &fakeTranscriber{replies: sampleReplies()}
	if _, err := Page(context.Background(), a, tr, tr, sampleSpec, "Pa", false); err != nil {
		t.Fatal(err)
	}

	// The page changes: the update prompt must carry the previous transcription.
	if _, err := a.Write(sampleNote(), map[string][]byte{"Pa": []byte(editedSVG)}); err != nil {
		t.Fatal(err)
	}
	tr.prompts = nil
	res, err := Page(context.Background(), a, tr, tr, sampleSpec, "Pa", false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Content == nil || res.Content.Kind != archive.TextUpdated {
		t.Errorf("content = %+v, want %q", res.Content, archive.TextUpdated)
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
	if _, err := a.Write(sampleNote(), map[string][]byte{"Pa": []byte(sampleSVG)}); err != nil {
		t.Fatal(err)
	}
	tr := &fakeTranscriber{replies: sampleReplies()}
	if _, err := Page(context.Background(), a, tr, tr, sampleSpec, "Pa", false); err != nil {
		t.Fatal(err)
	}

	tr.calls = 0
	res, err := Page(context.Background(), a, tr, tr, sampleSpec, "Pa", true)
	if err != nil {
		t.Fatal(err)
	}
	if res.Content == nil || res.Content.Kind != archive.TextUpdated {
		t.Errorf("content = %+v, want %q", res.Content, archive.TextUpdated)
	}
	if tr.calls == 0 {
		t.Error("force did not re-analyze")
	}
}

// TestPageMergePreservesUserEdits: a user edit (analyze-edit) must survive a
// re-analysis — the LLM sees the AI base, never the user's text, and the 3-way
// merge re-applies the edit onto the fresh transcription.
func TestPageMergePreservesUserEdits(t *testing.T) {
	a := archive.New(t.TempDir())
	if _, err := a.Write(sampleNote(), map[string][]byte{"Pa": []byte(sampleSVG)}); err != nil {
		t.Fatal(err)
	}
	tr := &fakeTranscriber{replies: sampleReplies()}
	if _, err := Page(context.Background(), a, tr, tr, sampleSpec, "Pa", false); err != nil {
		t.Fatal(err)
	}

	// The user renames the heading of the AI transcription ("# Heading\n\npage
	// body text") — away from the body line the re-analysis will touch, so the
	// merge stays clean — then the page changes on the device.
	base := "# Heading\n\npage body text\n"
	if _, err := a.WriteAnalysisEdit("F_A", "Pa", base, "# My own heading\n\npage body text\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Write(sampleNote(), map[string][]byte{"Pa": []byte(editedSVG)}); err != nil {
		t.Fatal(err)
	}

	tr.prompts = nil
	res, err := Page(context.Background(), a, tr, tr, sampleSpec, "Pa", false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Content == nil || res.Content.Kind != archive.TextUpdated {
		t.Errorf("content = %+v, want %q", res.Content, archive.TextUpdated)
	}
	// The update prompt carries the base, not the user's edit.
	if !strings.Contains(tr.prompts[0], base) {
		t.Errorf("update prompt lacks the AI base: %q", tr.prompts[0])
	}
	for _, p := range tr.prompts {
		if strings.Contains(p, "My own heading") {
			t.Fatalf("user edit leaked into an LLM prompt: %q", p)
		}
	}
	md, err := a.ReadAnalysisMD("F_A", "Pa")
	if err != nil {
		t.Fatal(err)
	}
	if md != "# My own heading\n\npage body text, edited\n" {
		t.Errorf("merge result = %q, want the user heading over the updated body", md)
	}
}

// TestPageMergeConflict: user edit and new analysis touching the same line
// leave conflict markers and set Content.Conflicts, not return an error.
func TestPageMergeConflict(t *testing.T) {
	a := archive.New(t.TempDir())
	if _, err := a.Write(sampleNote(), map[string][]byte{"Pa": []byte(sampleSVG)}); err != nil {
		t.Fatal(err)
	}
	tr := &fakeTranscriber{replies: sampleReplies()}
	if _, err := Page(context.Background(), a, tr, tr, sampleSpec, "Pa", false); err != nil {
		t.Fatal(err)
	}

	// Both sides rewrite the body line: "page body text" → user version vs
	// "page body text, edited" from the update reply.
	base := "# Heading\n\npage body text\n"
	if _, err := a.WriteAnalysisEdit("F_A", "Pa", base, "# Heading\n\npage body text, user version\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Write(sampleNote(), map[string][]byte{"Pa": []byte(editedSVG)}); err != nil {
		t.Fatal(err)
	}

	res, err := Page(context.Background(), a, tr, tr, sampleSpec, "Pa", false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Content == nil || !res.Content.Conflicts {
		t.Errorf("content = %+v, want conflicts", res.Content)
	}
	md, err := a.ReadAnalysisMD("F_A", "Pa")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(md, "<<<<<<< edited") || !strings.Contains(md, ">>>>>>> reanalyzed") {
		t.Errorf("conflict markers missing:\n%s", md)
	}
}

// TestPageHumanTranscriptionStaysOffLLM: a page transcribed by hand (empty AI
// base) gets a fresh transcription — the human text is never sent to the LLM —
// and the two sides land in a conflict for the user to resolve once.
func TestPageHumanTranscriptionStaysOffLLM(t *testing.T) {
	a := archive.New(t.TempDir())
	if _, err := a.Write(sampleNote(), map[string][]byte{"Pa": []byte(sampleSVG)}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.WriteAnalysisEdit("F_A", "Pa", "", "written by hand\n"); err != nil {
		t.Fatal(err)
	}

	tr := &fakeTranscriber{replies: sampleReplies()}
	res, err := Page(context.Background(), a, tr, tr, sampleSpec, "Pa", false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Content == nil || !res.Content.Conflicts {
		t.Errorf("content = %+v, want conflicts", res.Content)
	}
	if !strings.HasPrefix(tr.prompts[0], sampleSpec.Content) {
		t.Errorf("expected the fresh Content prompt, got %q", tr.prompts[0])
	}
	for _, p := range tr.prompts {
		if strings.Contains(p, "written by hand") {
			t.Fatalf("human transcription leaked into an LLM prompt: %q", p)
		}
	}
	md, err := a.ReadAnalysisMD("F_A", "Pa")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(md, "written by hand") || !strings.Contains(md, "page body text") {
		t.Errorf("conflict lost a side:\n%s", md)
	}
}

// TestPageKeepsEditedRegionNames: a user override on a title/link name (Edited)
// survives re-analysis untouched and its region is not re-transcribed, even
// under --force.
func TestPageKeepsEditedRegionNames(t *testing.T) {
	a := archive.New(t.TempDir())
	if _, err := a.Write(sampleNote(), map[string][]byte{"Pa": []byte(sampleSVG)}); err != nil {
		t.Fatal(err)
	}
	tr := &fakeTranscriber{replies: sampleReplies()}
	if _, err := Page(context.Background(), a, tr, tr, sampleSpec, "Pa", false); err != nil {
		t.Fatal(err)
	}

	// The user fixes the mis-transcribed title/link names (as analyze-edit does).
	pd, err := a.ReadPage("F_A", "Pa")
	if err != nil {
		t.Fatal(err)
	}
	pd.Titles[0].Analysis = &archive.TitleAnalysis{Name: "My title", Edited: true}
	pd.Links[0].Analysis = &archive.LinkAnalysis{Name: "My link", Edited: true}
	if _, err := a.WritePage("F_A", pd); err != nil {
		t.Fatal(err)
	}

	// Re-analyze under force: overrides stay and their regions are not re-sent.
	tr.prompts = nil
	if _, err := Page(context.Background(), a, tr, tr, sampleSpec, "Pa", true); err != nil {
		t.Fatal(err)
	}
	pd, err = a.ReadPage("F_A", "Pa")
	if err != nil {
		t.Fatal(err)
	}
	if pd.Titles[0].Analysis == nil || pd.Titles[0].Analysis.Name != "My title" || !pd.Titles[0].Analysis.Edited {
		t.Errorf("title override lost: %+v", pd.Titles[0].Analysis)
	}
	if pd.Links[0].Analysis == nil || pd.Links[0].Analysis.Name != "My link" || !pd.Links[0].Analysis.Edited {
		t.Errorf("link override lost: %+v", pd.Links[0].Analysis)
	}
	for _, p := range tr.prompts {
		if strings.HasPrefix(p, sampleSpec.Title) || strings.HasPrefix(p, sampleSpec.Link) {
			t.Errorf("edited region was re-transcribed: %q", p)
		}
	}
}

func TestPageNotFound(t *testing.T) {
	a := archive.New(t.TempDir())
	fake := &fakeTranscriber{}
	if _, err := Page(context.Background(), a, fake, fake, Spec{}, "missing", false); err == nil {
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

// TestPageBlankSkipsTheModel: a page with no ink has nothing to transcribe, so
// the content call and the content-derived fields are skipped outright — only
// the title/link crops (device metadata, not blank-gated) reach the model. The
// page is still analyzed: the source hash is stamped, so the next run skips it.
func TestPageBlankSkipsTheModel(t *testing.T) {
	a := archive.New(t.TempDir())
	if _, err := a.Write(sampleNote(), map[string][]byte{"Pa": []byte(blankSVG)}); err != nil {
		t.Fatal(err)
	}

	tr := &fakeTranscriber{replies: sampleReplies()}
	res, err := Page(context.Background(), a, tr, tr, sampleSpec, "Pa", false)
	if err != nil {
		t.Fatal(err)
	}
	// Nothing was there and nothing is there: unchanged, where the old vocabulary
	// reported "analyzed" and made a free page look like a transcribed one.
	if res.Content == nil || res.Content.Kind != archive.TextUnchanged {
		t.Errorf("content = %+v, want %q", res.Content, archive.TextUnchanged)
	}
	for _, p := range tr.prompts {
		if strings.HasPrefix(p, sampleSpec.Content) || strings.HasPrefix(p, sampleSpec.Fields[0].Prompt) {
			t.Errorf("blank page reached the model: %q", p)
		}
	}
	// 1 title + 1 link, no page and no field call.
	if tr.calls != 2 {
		t.Errorf("transcriber calls = %d, want 2 (title + link only), prompts %q", tr.calls, tr.prompts)
	}

	pd, err := a.ReadPage("F_A", "Pa")
	if err != nil {
		t.Fatal(err)
	}
	if pd.Analysis == nil || pd.Analysis.SourceHash == "" {
		t.Fatal("a blank page must still stamp its source hash")
	}
	if len(pd.Analysis.Fields) != 0 {
		t.Errorf("fields = %v, want none for empty content", pd.Analysis.Fields)
	}
	assertNoMD(t, a)

	tr.calls = 0
	res, err = Page(context.Background(), a, tr, tr, sampleSpec, "Pa", false)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Skipped || res.Calls != 0 {
		t.Errorf("re-run: res = %+v, want skipped with no calls", res)
	}
}

// TestErasedPageClearsContent: erasing a page's handwriting is a real change —
// the transcription goes away with the ink rather than lingering, and it does so
// without another model call.
func TestErasedPageClearsContent(t *testing.T) {
	a := archive.New(t.TempDir())
	if _, err := a.Write(sampleNote(), map[string][]byte{"Pa": []byte(sampleSVG)}); err != nil {
		t.Fatal(err)
	}
	tr := &fakeTranscriber{replies: sampleReplies()}
	if _, err := Page(context.Background(), a, tr, tr, sampleSpec, "Pa", false); err != nil {
		t.Fatal(err)
	}
	if md, _ := a.ReadAnalysisMD("F_A", "Pa"); md == "" {
		t.Fatal("precondition: the inked page must have content")
	}

	// Re-ingest the same page with the handwriting rubbed out.
	if _, err := a.Write(sampleNote(), map[string][]byte{"Pa": []byte(blankSVG)}); err != nil {
		t.Fatal(err)
	}
	tr.calls, tr.prompts = 0, nil
	res, err := Page(context.Background(), a, tr, tr, sampleSpec, "Pa", false)
	if err != nil {
		t.Fatal(err)
	}
	// The old vocabulary could only call this "updated" (a previous transcription
	// existed); the change vocabulary says what actually happened to the text.
	if res.Content == nil || res.Content.Kind != archive.TextCleared {
		t.Errorf("content = %+v, want %q", res.Content, archive.TextCleared)
	}
	if md, _ := a.ReadAnalysisMD("F_A", "Pa"); md != "" {
		t.Errorf("content = %q, want the erased page to clear", md)
	}
	assertNoMD(t, a)
	pd, _ := a.ReadPage("F_A", "Pa")
	// Calls is the page total: the erased content cost nothing (no vision call for
	// a blank page), while the title/link regions are template-independent and
	// still re-transcribe. The prompt check below pins the content half.
	if res.Calls != len(pd.Titles)+len(pd.Links) {
		t.Errorf("calls = %d, want only the title/link regions to have cost one each", res.Calls)
	}
	if len(pd.Analysis.Fields) != 0 {
		t.Errorf("fields = %v, want the erased page's fields gone", pd.Analysis.Fields)
	}
	for _, p := range tr.prompts {
		if strings.HasPrefix(p, sampleSpec.Update) || strings.HasPrefix(p, sampleSpec.Content) {
			t.Errorf("erased page reached the model: %q", p)
		}
	}
}

// TestBlankPageKeepsHandWrittenContent: a transcription the user typed for a
// blank page (analyze-edit on an empty page) is not clobbered and — unlike an
// LLM round-trip — does not conflict, because the empty transcription equals the
// empty AI base, so the merge has nothing to apply.
func TestBlankPageKeepsHandWrittenContent(t *testing.T) {
	a := archive.New(t.TempDir())
	if _, err := a.Write(sampleNote(), map[string][]byte{"Pa": []byte(blankSVG)}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.WriteAnalysisEdit("F_A", "Pa", "", "written by hand"); err != nil {
		t.Fatal(err)
	}

	tr := &fakeTranscriber{replies: sampleReplies()}
	res, err := Page(context.Background(), a, tr, tr, sampleSpec, "Pa", false)
	if err != nil {
		t.Fatal(err)
	}
	// Nothing moved: the empty transcription equals the empty AI base, so the
	// merge is a no-op. The old vocabulary reported the misleading "analyzed".
	if res.Content == nil || res.Content.Kind != archive.TextUnchanged {
		t.Errorf("content = %+v, want %q", res.Content, archive.TextUnchanged)
	}
	md, err := a.ReadAnalysisMD("F_A", "Pa")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(md, "written by hand") || strings.Contains(md, "<<<<<<<") {
		t.Errorf("hand-written content = %q, want it kept without conflict markers", md)
	}
}
