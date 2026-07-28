// Package serve is the built-in, zero-setup viewer: it turns a set of assembled
// NoteViews into a small local HTTP site so notes can be browsed in a browser
// without the Emacs client. Everything is in-memory — the views are computed
// once by the caller (via retrieve.Get) and the page SVGs are streamed straight
// from the archive on demand; nothing is copied to disk.
//
// Three routes:
//
//	GET /                    gallery of notes (name + first-page thumbnail)
//	GET /note/{fid}          gallery of that note's pages, click to enlarge
//	GET /svg/{fid}/{pid}.svg the page SVG, streamed from the archive
//
// The SVG route only serves pages that belong to the served set, so the viewer
// never exposes the whole archive — just the selected pages.
package serve

import (
	"html/template"
	"net/http"
	"strings"

	"github.com/jdlugosz963/snorg/internal/archive"
	"github.com/jdlugosz963/snorg/internal/retrieve"
)

// Handler builds the viewer over the given views. The archive is used only to
// stream page SVGs on demand; views carry everything else.
func Handler(a *archive.Archive, views []*retrieve.NoteView) http.Handler {
	byID := make(map[string]*retrieve.NoteView, len(views))
	allowed := make(map[string]bool) // "fid/pid" pairs this viewer may serve
	for _, v := range views {
		byID[v.FileID] = v
		for _, p := range v.Pages {
			allowed[v.FileID+"/"+p.PageID] = true
		}
	}

	mux := http.NewServeMux()

	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		cards := make([]noteCard, 0, len(views))
		for _, v := range views {
			if len(v.Pages) == 0 {
				continue
			}
			cards = append(cards, noteCard{FileID: v.FileID, Name: noteName(v), FirstPageID: v.Pages[0].PageID})
		}
		render(w, indexTmpl, indexData{Notes: cards})
	})

	mux.HandleFunc("GET /note/{fid}", func(w http.ResponseWriter, r *http.Request) {
		v := byID[r.PathValue("fid")]
		if v == nil {
			http.NotFound(w, r)
			return
		}
		pages := make([]string, 0, len(v.Pages))
		for _, p := range v.Pages {
			pages = append(pages, p.PageID)
		}
		render(w, noteTmpl, noteData{FileID: v.FileID, Name: noteName(v), PageIDs: pages})
	})

	// The .svg suffix stays in the URL for clarity but is not a routable wildcard
	// segment (Go's mux forbids {pid}.svg), so match the whole filename and strip.
	mux.HandleFunc("GET /svg/{fid}/{name}", func(w http.ResponseWriter, r *http.Request) {
		fid, pid := r.PathValue("fid"), strings.TrimSuffix(r.PathValue("name"), ".svg")
		if !allowed[fid+"/"+pid] {
			http.NotFound(w, r)
			return
		}
		b, err := a.ReadSVG(fid, pid)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Write(b)
	})

	return mux
}

// noteName is the human label for a note: the original .note filename without
// the extension, falling back to the FILE_ID when the source is unknown.
func noteName(v *retrieve.NoteView) string {
	name := strings.TrimSuffix(v.Source, ".note")
	if name == "" {
		return v.FileID
	}
	return name
}

func render(w http.ResponseWriter, t *template.Template, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.Execute(w, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

type indexData struct{ Notes []noteCard }

type noteCard struct {
	FileID      string
	Name        string
	FirstPageID string
}

type noteData struct {
	FileID  string
	Name    string
	PageIDs []string
}

// shell wraps a page body in a minimal, dependency-free HTML document. The
// lightbox JS is only meaningful on the note page (it keys off .thumb elements),
// but shipping it everywhere keeps the shell a single template.
const shell = `<!doctype html>
<html lang="en">
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{template "title" .}}</title>
<style>
  :root { color-scheme: light dark; }
  body { margin: 0; font: 15px/1.4 system-ui, sans-serif; }
  header { padding: 1rem 1.25rem; border-bottom: 1px solid #8884; }
  header a { text-decoration: none; color: inherit; opacity: .7; }
  h1 { margin: 0; font-size: 1.1rem; font-weight: 600; }
  .grid { display: grid; gap: 1rem; padding: 1.25rem;
          grid-template-columns: repeat(auto-fill, minmax(180px, 1fr)); }
  .card { display: block; text-decoration: none; color: inherit; }
  .card img, .thumb img { width: 100%; height: auto; display: block;
          background: #fff; border: 1px solid #8884; border-radius: 4px; }
  .thumb { cursor: zoom-in; border: 0; background: none; padding: 0; }
  .cap { margin-top: .4rem; font-size: .9rem; word-break: break-word; }
  /* lightbox */
  #lb { position: fixed; inset: 0; background: #000d; display: none;
        align-items: center; justify-content: center; }
  #lb.open { display: flex; }
  #lb img { max-width: 96vw; max-height: 96vh; background: #fff; }
  #lb button { position: fixed; top: 50%; transform: translateY(-50%);
        font-size: 2.5rem; color: #fff; background: none; border: 0;
        cursor: pointer; padding: 0 1rem; }
  #lb .prev { left: 0; } #lb .next { right: 0; }
</style>
<header>{{template "head" .}}</header>
{{template "body" .}}
<div id="lb" role="dialog" aria-modal="true">
  <button class="prev" aria-label="previous">&#8249;</button>
  <img alt="">
  <button class="next" aria-label="next">&#8250;</button>
</div>
<script>
(function () {
  var thumbs = Array.prototype.slice.call(document.querySelectorAll('.thumb'));
  if (!thumbs.length) return;
  var lb = document.getElementById('lb'), img = lb.querySelector('img'), i = 0;
  function show(n) { i = (n + thumbs.length) % thumbs.length;
    img.src = thumbs[i].dataset.src; }
  function open(n) { show(n); lb.classList.add('open'); }
  function close() { lb.classList.remove('open'); img.src = ''; }
  thumbs.forEach(function (t, n) {
    t.addEventListener('click', function () { open(n); });
  });
  lb.querySelector('.prev').addEventListener('click', function (e) {
    e.stopPropagation(); show(i - 1); });
  lb.querySelector('.next').addEventListener('click', function (e) {
    e.stopPropagation(); show(i + 1); });
  lb.addEventListener('click', function (e) { if (e.target === lb) close(); });
  document.addEventListener('keydown', function (e) {
    if (!lb.classList.contains('open')) return;
    if (e.key === 'Escape') close();
    else if (e.key === 'ArrowLeft') show(i - 1);
    else if (e.key === 'ArrowRight') show(i + 1);
  });
})();
</script>
</html>
`

var indexTmpl = mustShell(`
{{define "title"}}snorg{{end}}
{{define "head"}}<h1>Notes</h1>{{end}}
{{define "body"}}
<div class="grid">
  {{range .Notes}}
  <a class="card" href="/note/{{.FileID}}">
    <img src="/svg/{{.FileID}}/{{.FirstPageID}}.svg" alt="" loading="lazy">
    <div class="cap">{{.Name}}</div>
  </a>
  {{else}}
  <p>No notes.</p>
  {{end}}
</div>
{{end}}
`)

var noteTmpl = mustShell(`
{{define "title"}}{{.Name}} — snorg{{end}}
{{define "head"}}<a href="/">&#8592; Notes</a><h1>{{.Name}}</h1>{{end}}
{{define "body"}}
<div class="grid">
  {{range $n, $pid := .PageIDs}}
  <button class="thumb" data-src="/svg/{{$.FileID}}/{{$pid}}.svg" aria-label="page {{$n}}">
    <img src="/svg/{{$.FileID}}/{{$pid}}.svg" alt="" loading="lazy">
  </button>
  {{else}}
  <p>No pages.</p>
  {{end}}
</div>
{{end}}
`)

// mustShell parses the shared shell plus the page-specific blocks into one
// template. The block names (title/head/body) are defined by each page.
func mustShell(page string) *template.Template {
	return template.Must(template.Must(template.New("shell").Parse(shell)).Parse(page))
}
