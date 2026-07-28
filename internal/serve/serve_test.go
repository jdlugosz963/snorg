package serve_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jdlugosz963/snorg/internal/archive"
	"github.com/jdlugosz963/snorg/internal/retrieve"
	"github.com/jdlugosz963/snorg/internal/serve"
	"github.com/jdlugosz963/snorg/internal/snote"
)

// fixture writes a two-page note into a fresh archive and returns the server
// over its assembled views.
func fixture(t *testing.T) *httptest.Server {
	t.Helper()
	a := archive.New(t.TempDir())
	n := &snote.Note{
		FileID: "F_TEST",
		Source: "My Meeting.note",
		Pages: []snote.Page{
			{ID: "Pa", Number: 1},
			{ID: "Pb", Number: 2},
		},
	}
	svg := `<svg xmlns="http://www.w3.org/2000/svg" width="1920" height="2560"><path d="M10 10 L100 100" stroke="#000"/></svg>`
	if err := a.Write(n, map[string][]byte{"Pa": []byte(svg), "Pb": []byte(svg)}); err != nil {
		t.Fatal(err)
	}
	// A transcription on the first page so the lightbox has content to show.
	if err := a.WriteAnalysisMD("F_TEST", "Pa", "# Heading\nsome notes"); err != nil {
		t.Fatal(err)
	}
	views, err := retrieve.Get(a, []string{"Pa", "Pb"})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(serve.Handler(a, views))
	t.Cleanup(srv.Close)
	return srv
}

func get(t *testing.T, srv *httptest.Server, path string) (*http.Response, string) {
	t.Helper()
	resp, err := http.Get(srv.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	return resp, string(b)
}

func TestIndexListsNotes(t *testing.T) {
	srv := fixture(t)
	resp, body := get(t, srv, "/")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	// Note name is the .note filename without extension, and the card links to
	// the note and shows the first page as the thumbnail.
	if !strings.Contains(body, "My Meeting") {
		t.Errorf("index missing note name:\n%s", body)
	}
	if !strings.Contains(body, `href="/note/F_TEST"`) {
		t.Errorf("index missing note link:\n%s", body)
	}
	// The card thumbnail is the lightweight rasterized PNG, not the SVG.
	if !strings.Contains(body, `/thumb/F_TEST/Pa.png`) {
		t.Errorf("index missing first-page thumbnail:\n%s", body)
	}
}

func TestNoteListsPages(t *testing.T) {
	srv := fixture(t)
	resp, body := get(t, srv, "/note/F_TEST")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	// Thumbnails point at /thumb PNGs; the lightbox opens the full SVG (data-full).
	for _, want := range []string{
		`class="thumb"`,
		`/thumb/F_TEST/Pa.png`, `/thumb/F_TEST/Pb.png`,
		`data-full="/svg/F_TEST/Pa.svg"`, `data-full="/svg/F_TEST/Pb.svg"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("note page missing %q:\n%s", want, body)
		}
	}
	// The first page's transcription rides along in the hidden .txt span so the
	// lightbox can show it under the enlarged page.
	if !strings.Contains(body, `class="txt"`) || !strings.Contains(body, "some notes") {
		t.Errorf("note page missing transcription span:\n%s", body)
	}
}

func TestThumbnailServedAsPNGWithCaching(t *testing.T) {
	srv := fixture(t)
	resp, body := get(t, srv, "/thumb/F_TEST/Pa.png")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "image/png" {
		t.Errorf("content-type = %q", ct)
	}
	if !strings.HasPrefix(body, "\x89PNG") {
		t.Errorf("body is not a PNG (first bytes %q)", body[:min(8, len(body))])
	}
	etag := resp.Header.Get("ETag")
	if etag == "" || resp.Header.Get("Cache-Control") == "" {
		t.Fatalf("missing cache headers: etag=%q cc=%q", etag, resp.Header.Get("Cache-Control"))
	}
	// A conditional re-request with the ETag gets 304, not the bytes again.
	req, _ := http.NewRequest("GET", srv.URL+"/thumb/F_TEST/Pa.png", nil)
	req.Header.Set("If-None-Match", etag)
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusNotModified {
		t.Errorf("conditional GET status = %d want 304", resp2.StatusCode)
	}
}

func TestSVGCarriesETag(t *testing.T) {
	srv := fixture(t)
	resp, _ := get(t, srv, "/svg/F_TEST/Pa.svg")
	if resp.Header.Get("ETag") == "" || resp.Header.Get("Cache-Control") == "" {
		t.Errorf("svg missing cache headers: etag=%q cc=%q",
			resp.Header.Get("ETag"), resp.Header.Get("Cache-Control"))
	}
}

func TestThumbForUnservedPageIs404(t *testing.T) {
	srv := fixture(t)
	if resp, _ := get(t, srv, "/thumb/F_TEST/Pzzz.png"); resp.StatusCode != http.StatusNotFound {
		t.Errorf("unserved thumb status = %d want 404", resp.StatusCode)
	}
}

func TestNoteUnknownIs404(t *testing.T) {
	srv := fixture(t)
	if resp, _ := get(t, srv, "/note/NOPE"); resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown note status = %d want 404", resp.StatusCode)
	}
}

func TestSVGServedForSelectedPage(t *testing.T) {
	srv := fixture(t)
	resp, body := get(t, srv, "/svg/F_TEST/Pa.svg")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "image/svg+xml" {
		t.Errorf("content-type = %q", ct)
	}
	if !strings.Contains(body, "<svg") {
		t.Errorf("body is not svg: %q", body)
	}
}

func TestSVGForUnservedPageIs404(t *testing.T) {
	// A real archive page (Pb exists) but outside the served set is still 404 —
	// the viewer only exposes selected pages.
	a := archive.New(t.TempDir())
	if err := a.Write(&snote.Note{FileID: "F_TEST", Source: "x.note", Pages: []snote.Page{{ID: "Pa", Number: 1}}}, map[string][]byte{"Pa": []byte("<svg/>")}); err != nil {
		t.Fatal(err)
	}
	views, err := retrieve.Get(a, []string{"Pa"})
	if err != nil {
		t.Fatal(err)
	}
	only := httptest.NewServer(serve.Handler(a, views))
	t.Cleanup(only.Close)
	if resp, _ := get(t, only, "/svg/F_TEST/Pb.svg"); resp.StatusCode != http.StatusNotFound {
		t.Errorf("unserved page status = %d want 404", resp.StatusCode)
	}
}
