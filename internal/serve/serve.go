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
// In flat mode (Handler's flat=true), the index instead shows every selected
// page in one gallery (no per-note grouping), each captioned with its note name
// and page number; the /note/{fid} and asset routes are unchanged.
//
// The SVG route only serves pages that belong to the served set, so the viewer
// never exposes the whole archive — just the selected pages.
package serve

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"sync"

	"github.com/jdlugosz963/snorg/internal/archive"
	"github.com/jdlugosz963/snorg/internal/retrieve"
)

// Handler builds the viewer over the given views. The archive is used only to
// stream page SVGs (and rasterize thumbnails) on demand; views carry everything
// else. When flat is true, the index is a single flat gallery of every selected
// page instead of a per-note gallery.
func Handler(a *archive.Archive, views []*retrieve.NoteView, flat bool) http.Handler {
	byID := make(map[string]*retrieve.NoteView, len(views))
	allowed := make(map[string]bool) // "fid/pid" pairs this viewer may serve
	for _, v := range views {
		byID[v.FileID] = v
		for _, p := range v.Pages {
			allowed[v.FileID+"/"+p.PageID] = true
		}
	}

	// Thumbnails (rasterized PNG) and viewer SVGs (links rewritten to viewer routes)
	// are derived once per page and cached for the session — the archive SVG never
	// changes while serving, so a plain memo under a mutex is enough.
	var memoMu sync.Mutex
	thumbs := make(map[string][]byte)
	svgs := make(map[string][]byte)

	// The landing layout — the grouped note gallery by default, one flat page
	// gallery under --flat — is the single place the mode is chosen; the routes
	// below stay branch-free.
	var lay layout = newGroupedLayout(views)
	if flat {
		lay = newFlatLayout(views)
	}

	mux := http.NewServeMux()

	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		lay.render(w)
	})

	mux.HandleFunc("GET /note/{fid}", func(w http.ResponseWriter, r *http.Request) {
		v := byID[r.PathValue("fid")]
		if v == nil {
			http.NotFound(w, r)
			return
		}
		render(w, noteTmpl, gridData{Name: noteName(v), Pages: notePages(v)})
	})

	// The .svg/.png suffix stays in the URL for clarity but is not a routable
	// wildcard segment (Go's mux forbids {pid}.svg), so match the whole filename
	// and strip it.
	mux.HandleFunc("GET /svg/{fid}/{name}", func(w http.ResponseWriter, r *http.Request) {
		fid, pid := r.PathValue("fid"), strings.TrimSuffix(r.PathValue("name"), ".svg")
		key := fid + "/" + pid
		if !allowed[key] {
			http.NotFound(w, r)
			return
		}
		memoMu.Lock()
		b, ok := svgs[key]
		memoMu.Unlock()
		if !ok {
			raw, err := a.ReadSVG(fid, pid)
			if err != nil {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			// Retarget baked links so a tap in the enlarged view opens the note
			// and enlarges the target page, and make the root scale to the
			// <object> box instead of rendering at native size and clipping.
			b = responsiveSVGRoot(rewriteViewerLinks(raw, fid, lay.pageHref))
			memoMu.Lock()
			svgs[key] = b
			memoMu.Unlock()
		}
		writeAsset(w, r, b, "image/svg+xml")
	})

	mux.HandleFunc("GET /thumb/{fid}/{name}", func(w http.ResponseWriter, r *http.Request) {
		fid, pid := r.PathValue("fid"), strings.TrimSuffix(r.PathValue("name"), ".png")
		key := fid + "/" + pid
		if !allowed[key] {
			http.NotFound(w, r)
			return
		}
		memoMu.Lock()
		png, ok := thumbs[key]
		memoMu.Unlock()
		if !ok {
			svg, err := a.ReadSVG(fid, pid)
			if err != nil {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			if png, err = thumbnailPNG(svg); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			memoMu.Lock()
			thumbs[key] = png
			memoMu.Unlock()
		}
		writeAsset(w, r, png, "image/png")
	})

	return mux
}

// writeAsset serves an immutable-for-the-session binary asset with an ETag and a
// cache window, so a browser revisiting a page reuses it (304) instead of
// re-downloading. The ETag is the content hash, so it also invalidates correctly
// if the bytes ever differ.
func writeAsset(w http.ResponseWriter, r *http.Request, b []byte, contentType string) {
	sum := sha256.Sum256(b)
	etag := `"` + hex.EncodeToString(sum[:8]) + `"`
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Header().Set("Content-Type", contentType)
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Write(b)
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

// layout is the pluggable landing layout: it renders GET / and decides where a
// baked in-page SVG link should jump in the enlarged view. Grouped (note gallery)
// and flat (one page gallery) are the two implementations; Handler picks one and
// every route stays branch-free. This is the seam that keeps the grouped/flat
// difference in one place instead of scattered `if flat` checks.
type layout interface {
	render(w http.ResponseWriter)    // GET /
	pageHref(fid, pid string) string // target of a rewritten in-SVG link
}

// groupedLayout is the default: a gallery of notes, each linking to its own page
// gallery; an in-page link opens the target note and enlarges the page there.
type groupedLayout struct{ cards []noteCard }

func newGroupedLayout(views []*retrieve.NoteView) groupedLayout {
	cards := make([]noteCard, 0, len(views))
	for _, v := range views {
		if len(v.Pages) == 0 {
			continue
		}
		cards = append(cards, noteCard{FileID: v.FileID, Name: noteName(v), FirstPageID: v.Pages[0].PageID})
	}
	return groupedLayout{cards: cards}
}

func (l groupedLayout) render(w http.ResponseWriter)    { render(w, indexTmpl, indexData{Notes: l.cards}) }
func (l groupedLayout) pageHref(fid, pid string) string { return "/note/" + fid + "?page=" + pid }

// flatLayout is the --flat layout: one gallery of every selected page (captioned
// with its owning note and page number, since there is no per-note grouping to
// convey that); an in-page link reopens that single index on the target page.
type flatLayout struct{ pages []pageItem }

func newFlatLayout(views []*retrieve.NoteView) flatLayout {
	pages := make([]pageItem, 0)
	for _, v := range views {
		name := noteName(v)
		for _, p := range v.Pages {
			pages = append(pages, pageItem{
				FileID:  v.FileID,
				PageID:  p.PageID,
				Content: pageContent(p),
				Caption: fmt.Sprintf("%s · %d", name, p.Number),
			})
		}
	}
	return flatLayout{pages: pages}
}

func (l flatLayout) render(w http.ResponseWriter)  { render(w, flatTmpl, gridData{Pages: l.pages}) }
func (l flatLayout) pageHref(_, pid string) string { return "/?page=" + pid }

type indexData struct{ Notes []noteCard }

type noteCard struct {
	FileID      string
	Name        string
	FirstPageID string
}

// gridData feeds the shared "pagegrid" template. Name is the note-gallery header
// label (unused by the flat gallery); Pages are the tiles.
type gridData struct {
	Name  string
	Pages []pageItem
}

// pageItem is one page tile in a grid (a note's page gallery or the flat gallery).
// It carries its own note (FileID) so the asset/lightbox routes resolve per tile,
// and an optional Caption that renders only when set (the flat gallery names the
// owning note + page number; a note's own gallery leaves it empty).
type pageItem struct {
	FileID  string
	PageID  string
	Content string // transcription markdown, shown under the enlarged page
	Caption string // "<note name> · <page number>", or "" in a note's own gallery
}

// pageContent is a page's transcription, or "" when it has none.
func pageContent(p retrieve.PageView) string {
	if p.Analysis != nil {
		return p.Analysis.Content
	}
	return ""
}

// notePages is a note's pages as caption-less grid tiles (the note gallery's
// header already names the note).
func notePages(v *retrieve.NoteView) []pageItem {
	items := make([]pageItem, 0, len(v.Pages))
	for _, p := range v.Pages {
		items = append(items, pageItem{FileID: v.FileID, PageID: p.PageID, Content: pageContent(p)})
	}
	return items
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
  .lbinner { display: flex; flex-direction: column; align-items: center;
        max-width: 96vw; max-height: 96vh; overflow: auto; gap: .75rem; }
  .lbinner object { height: 78vh; aspect-ratio: 3 / 4; max-width: 94vw;
        background: #fff; }
  #lbtext { white-space: pre-wrap; word-break: break-word; max-width: 900px;
        width: 92vw; color: #eee; background: #1c1c1ccc; padding: .75rem 1rem;
        border-radius: 6px; font-size: .92rem; }
  #lb button { position: fixed; top: 50%; transform: translateY(-50%);
        font-size: 2.5rem; color: #fff; background: none; border: 0;
        cursor: pointer; padding: 0 1rem; }
  #lb .prev { left: 0; } #lb .next { right: 0; }
</style>
<header>{{template "head" .}}</header>
{{template "body" .}}
<div id="lb" role="dialog" aria-modal="true">
  <button class="prev" aria-label="previous">&#8249;</button>
  <div class="lbinner">
    <object type="image/svg+xml"></object>
    <div id="lbtext" hidden></div>
  </div>
  <button class="next" aria-label="next">&#8250;</button>
</div>
<script>
(function () {
  var thumbs = Array.prototype.slice.call(document.querySelectorAll('.thumb'));
  if (!thumbs.length) return;
  var lb = document.getElementById('lb'), obj = lb.querySelector('object'),
      txt = document.getElementById('lbtext'), i = 0;
  // The enlarged page is an <object> (a live SVG document), so its baked links —
  // retargeted to a viewer route (?page={pid}) with target="_top" — are clickable
  // and navigate the whole viewer; an <img> would render the SVG inertly.
  function show(n) { i = (n + thumbs.length) % thumbs.length;
    obj.data = thumbs[i].dataset.full;
    var el = thumbs[i].querySelector('.txt'), t = el ? el.textContent : '';
    txt.textContent = t; txt.hidden = (t.trim() === ''); }
  function open(n) { show(n); lb.classList.add('open'); }
  function close() { lb.classList.remove('open'); obj.removeAttribute('data'); }
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
  // Following an in-SVG link lands on a viewer route carrying ?page={pid}: open
  // the lightbox on that page so the tap "enlarges" the target.
  var want = new URLSearchParams(location.search).get('page');
  if (want) {
    var idx = thumbs.findIndex(function (t) { return t.dataset.pid === want; });
    if (idx >= 0) open(idx);
  }
})();
</script>
</html>
{{define "pagegrid"}}
<div class="grid">
  {{range $i, $p := .Pages}}
  <button class="thumb" data-full="/svg/{{$p.FileID}}/{{$p.PageID}}.svg" data-pid="{{$p.PageID}}" aria-label="{{if $p.Caption}}{{$p.Caption}}{{else}}page {{$i}}{{end}}">
    <img src="/thumb/{{$p.FileID}}/{{$p.PageID}}.png" alt="" loading="lazy" decoding="async">
    <span class="txt" hidden>{{$p.Content}}</span>
    {{if $p.Caption}}<div class="cap">{{$p.Caption}}</div>{{end}}
  </button>
  {{else}}
  <p>No pages.</p>
  {{end}}
</div>
{{end}}
`

var indexTmpl = mustShell(`
{{define "title"}}snorg{{end}}
{{define "head"}}<h1>Notes</h1>{{end}}
{{define "body"}}
<div class="grid">
  {{range .Notes}}
  <a class="card" href="/note/{{.FileID}}">
    <img src="/thumb/{{.FileID}}/{{.FirstPageID}}.png" alt="" loading="lazy" decoding="async">
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
{{define "body"}}{{template "pagegrid" .}}{{end}}
`)

// flatTmpl is the flat-mode index: the shared page grid over every selected page
// (captions set, so each tile names its owning note + page number).
var flatTmpl = mustShell(`
{{define "title"}}snorg{{end}}
{{define "head"}}<h1>Pages</h1>{{end}}
{{define "body"}}{{template "pagegrid" .}}{{end}}
`)

// mustShell parses the shared shell plus the page-specific blocks into one
// template. The block names (title/head/body) are defined by each page.
func mustShell(page string) *template.Template {
	return template.Must(template.Must(template.New("shell").Parse(shell)).Parse(page))
}
