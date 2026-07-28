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
	if err := a.Write(n, map[string][]byte{"Pa": []byte("<svg>a</svg>"), "Pb": []byte("<svg>b</svg>")}); err != nil {
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
	if !strings.Contains(body, `/svg/F_TEST/Pa.svg`) {
		t.Errorf("index missing first-page thumbnail:\n%s", body)
	}
}

func TestNoteListsPages(t *testing.T) {
	srv := fixture(t)
	resp, body := get(t, srv, "/note/F_TEST")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	for _, want := range []string{"/svg/F_TEST/Pa.svg", "/svg/F_TEST/Pb.svg", `class="thumb"`} {
		if !strings.Contains(body, want) {
			t.Errorf("note page missing %q:\n%s", want, body)
		}
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
