package snorg

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/jdlugosz963/snorg/internal/archive"
	"github.com/jdlugosz963/snorg/internal/snote"
)

// writeNote writes one note (with a page SVG each) straight through the archive
// layer, so a filter fixture needs no .note parsing.
func writeNote(t *testing.T, a *archive.Archive, n *snote.Note) {
	t.Helper()
	svgs := make(map[string][]byte, len(n.Pages))
	for _, p := range n.Pages {
		svgs[p.ID] = []byte(testSVG)
	}
	if _, err := a.Write(n, svgs); err != nil {
		t.Fatal(err)
	}
}

// seedFiltered: F_A holds Pa (starred, keyword "foo", transcribed) and Pb (keyword
// "foobar"); F_B holds Pc (starred, transcribed) and Pd (nothing at all).
func seedFiltered(t *testing.T) *Client {
	t.Helper()
	root := t.TempDir()
	a := archive.New(root)
	writeNote(t, a, &snote.Note{
		FileID: "F_A",
		Source: "alpha.note",
		Pages: []snote.Page{
			{ID: "Pa", Number: 1, Starred: true, Keywords: []snote.Keyword{{Text: "foo"}}},
			{ID: "Pb", Number: 2, Keywords: []snote.Keyword{{Text: "foobar"}}},
		},
	})
	writeNote(t, a, &snote.Note{
		FileID: "F_B",
		Source: "beta.note",
		Pages: []snote.Page{
			{ID: "Pc", Number: 1, Starred: true},
			{ID: "Pd", Number: 2},
		},
	})
	if err := a.WriteAnalysisMD("F_A", "Pa", "meeting notes about foo"); err != nil {
		t.Fatal(err)
	}
	if err := a.WriteAnalysisMD("F_B", "Pc", "grocery list"); err != nil {
		t.Fatal(err)
	}
	c, err := Open(root, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return c
}

// matched runs pred over the client's archive and returns the PAGEIDs in order.
func matched(t *testing.T, c *Client, pred Predicate) []string {
	t.Helper()
	ms, err := c.Query(pred)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, len(ms))
	for i, m := range ms {
		ids[i] = m.PageID
	}
	return ids
}

func want(t *testing.T, label string, got, exp []string) {
	t.Helper()
	if len(got) == 0 && len(exp) == 0 {
		return
	}
	if !reflect.DeepEqual(got, exp) {
		t.Errorf("%s = %v, want %v", label, got, exp)
	}
}

func TestMatchDocumentFilters(t *testing.T) {
	c := seedFiltered(t)

	want(t, "MatchAll", matched(t, c, MatchAll), []string{"Pa", "Pb", "Pc", "Pd"})
	want(t, "MatchStarred", matched(t, c, MatchStarred), []string{"Pa", "Pc"})
	want(t, "MatchNote(=F_B)", matched(t, c, MatchNote(Exact("F_B"))), []string{"Pc", "Pd"})
	// The three matchers on one field: "foo" is a substring of both keywords,
	// an exact match for one, and the regexp anchors the same way.
	want(t, "MatchKeyword(:foo)", matched(t, c, MatchKeyword(Substring("foo"))), []string{"Pa", "Pb"})
	want(t, "MatchKeyword(=foo)", matched(t, c, MatchKeyword(Exact("foo"))), []string{"Pa"})
	want(t, "MatchKeyword(~^foo$)", matched(t, c, MatchKeyword(Regexp(regexp.MustCompile("^foo$")))), []string{"Pa"})
	want(t, "MatchKeyword(:nope)", matched(t, c, MatchKeyword(Substring("nope"))), nil)
	// Substring folds case; Exact does not.
	want(t, "MatchKeyword(:FOO)", matched(t, c, MatchKeyword(Substring("FOO"))), []string{"Pa", "Pb"})
	want(t, "MatchKeyword(=FOO)", matched(t, c, MatchKeyword(Exact("FOO"))), nil)
}

// MatchContent reads the <PAGEID>.md sidecar through the client the predicate is
// handed — the whole reason a candidate carries one.
func TestMatchContent(t *testing.T) {
	c := seedFiltered(t)

	want(t, "MatchContent(:meeting)", matched(t, c, MatchContent(Substring("meeting"))), []string{"Pa"})
	want(t, "MatchContent(~^grocery)", matched(t, c, MatchContent(Regexp(regexp.MustCompile("^grocery")))), []string{"Pc"})
	want(t, "MatchContent(:nonexistent)", matched(t, c, MatchContent(Substring("nonexistent"))), nil)
}

// ParseQuery is the whole language over one fixture: precedence (NOT > AND > OR),
// grouping, and each operator on each field. The predicates themselves are covered
// above, so this is about what the expression compiles to.
func TestParseQueryComposition(t *testing.T) {
	c := seedFiltered(t)

	for _, tc := range []struct {
		expr string
		exp  []string
	}{
		{"all", []string{"Pa", "Pb", "Pc", "Pd"}},
		{"starred", []string{"Pa", "Pc"}},
		{"starred AND keyword:foo", []string{"Pa"}},
		{"starred OR keyword:foo", []string{"Pa", "Pb", "Pc"}},
		{"keyword:foo AND NOT starred", []string{"Pb"}},
		{"NOT (starred OR keyword:foo)", []string{"Pd"}},
		{"starred AND (content:grocery OR keyword=foobar)", []string{"Pc"}},
		// AND binds tighter than OR: (foo AND starred) OR F_B, not foo AND (…).
		{"keyword:foo AND starred OR note=F_B", []string{"Pa", "Pc", "Pd"}},
		{"note=F_B", []string{"Pc", "Pd"}},
		{"note:f_b", []string{"Pc", "Pd"}}, // substring folds case, exact does not
		{"note=f_b", nil},
		{`content~"^grocery"`, []string{"Pc"}},
		{"unanalyzed AND starred", []string{"Pa", "Pc"}}, // nothing here is analyzed
	} {
		pred, err := ParseQuery(tc.expr)
		if err != nil {
			t.Errorf("ParseQuery(%q): %v", tc.expr, err)
			continue
		}
		want(t, tc.expr, matched(t, c, pred), tc.exp)
	}
}

// A hand-written predicate reaches the same accessors the built-in ones use, and
// reports a read it could not complete the same way they do.
func TestHandWrittenPredicate(t *testing.T) {
	c := seedFiltered(t)

	pred := func(p Page) (bool, error) {
		md, err := p.Transcription()
		if err != nil {
			return false, err
		}
		return md != "" && p.Doc.Starred && p.Note.Source == "alpha.note", nil
	}
	want(t, "custom", matched(t, c, pred), []string{"Pa"})

	failing := func(Page) (bool, error) { return false, errors.New("cannot decide") }
	_, err := c.Query(failing)
	if err == nil || !strings.Contains(err.Error(), "cannot decide") {
		t.Errorf("Query(failing predicate) error = %v, want it to carry the predicate's error", err)
	}
}

func TestMatchUnanalyzed(t *testing.T) {
	c := seedFiltered(t)
	pd, err := c.ReadPage("Pa")
	if err != nil {
		t.Fatal(err)
	}
	pd.Analysis = &archive.PageAnalysis{SourceHash: "abc"}
	if _, err := c.arch.WritePage("F_A", pd); err != nil {
		t.Fatal(err)
	}

	want(t, "MatchUnanalyzed", matched(t, c, MatchUnanalyzed), []string{"Pb", "Pc", "Pd"})
}

func TestMatchNotAndIDs(t *testing.T) {
	c := seedFiltered(t)

	want(t, "not starred", matched(t, c, MatchNot(MatchStarred)), []string{"Pb", "Pd"})
	// What piping does: restrict to the candidate set, then intersect.
	want(t, "ids ∩ starred",
		matched(t, c, MatchAnd(MatchIDs([]string{"Pa", "Pc", "Pd"}), MatchStarred)), []string{"Pa", "Pc"})
	want(t, "ids ∩ not starred",
		matched(t, c, MatchAnd(MatchIDs([]string{"Pa", "Pb", "Pc"}), MatchNot(MatchStarred))), []string{"Pb"})
	want(t, "empty id set", matched(t, c, MatchIDs(nil)), nil)
}

func TestMatchTagIncludingInherited(t *testing.T) {
	c := seedFiltered(t)
	if _, err := c.Tag([]string{"Pa", "Pb"}, "todo"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Tag([]string{"Pa"}, "important"); err != nil {
		t.Fatal(err)
	}
	// A note tag lives only in note.json, yet every page of the note matches it.
	if _, err := c.TagNote([]string{"F_B"}, "shared"); err != nil {
		t.Fatal(err)
	}

	want(t, "tag todo", matched(t, c, MatchTag(Exact("todo"))), []string{"Pa", "Pb"})
	want(t, "tag important", matched(t, c, MatchTag(Exact("important"))), []string{"Pa"})
	want(t, "tag shared (inherited)", matched(t, c, MatchTag(Exact("shared"))), []string{"Pc", "Pd"})
}

func TestMatchDate(t *testing.T) {
	root := t.TempDir()
	a := archive.New(root)
	writeNote(t, a, &snote.Note{
		FileID: "F_D",
		Pages: []snote.Page{
			{ID: "P20260701090000AB", Number: 1},
			{ID: "P20260715120000CD", Number: 2},
			{ID: "P20260722080000EF", Number: 3},
			{ID: "Pnodate", Number: 4},
		},
	})
	c, err := Open(root, nil)
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name     string
		from, to string
		exp      []string
	}{
		{"exact day", "20260715", "20260715", []string{"P20260715120000CD"}},
		{"range", "20260701", "20260715", []string{"P20260701090000AB", "P20260715120000CD"}},
		{"open from", "", "20260715", []string{"P20260701090000AB", "P20260715120000CD"}},
		{"open to", "20260715", "", []string{"P20260715120000CD", "P20260722080000EF"}},
		{"no match", "20250101", "20250101", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want(t, "MatchDate", matched(t, c, MatchDate(tc.from, tc.to)), tc.exp)
		})
	}
}

// templatedClient: F_A holds Pt (drawn on the configured template), Pother (some
// other background) and Pplain (none). The template declares the boxes title/grade;
// Pt's sidecar fills in title, leaves grade unwritten and carries a "gone" section
// the config no longer declares (a tombstone). Pother's sidecar holds the same text
// as plain content, so a region filter matching it would prove the template gate
// leaks.
func templatedClient(t *testing.T, declare bool) *Client {
	t.Helper()
	dir := t.TempDir()
	root := t.TempDir()
	a := archive.New(root)
	writeNote(t, a, &snote.Note{
		FileID: "F_A",
		Pages: []snote.Page{
			{ID: "Pt", Number: 1},
			{ID: "Pother", Number: 2},
			{ID: "Pplain", Number: 3},
		},
	})
	img := []byte("template-image")
	if err := os.WriteFile(filepath.Join(dir, "bg.png"), img, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(img)
	stamp := func(pageID, hash string) {
		pd, err := a.ReadPage("F_A", pageID)
		if err != nil {
			t.Fatal(err)
		}
		pd.BackgroundHash = hash
		if _, err := a.WritePage("F_A", pd); err != nil {
			t.Fatal(err)
		}
	}
	stamp("Pt", hex.EncodeToString(sum[:]))
	stamp("Pother", "0000")
	regions := "<!-- region title (Title) -->\nInvoice 2026\n<!-- region gone (Old) -->\ntombstoned text\n"
	if err := a.WriteAnalysisMD("F_A", "Pt", regions); err != nil {
		t.Fatal(err)
	}
	if err := a.WriteAnalysisMD("F_A", "Pother", "Invoice 2026"); err != nil {
		t.Fatal(err)
	}

	var cfg *Config
	if declare {
		cfgPath := filepath.Join(dir, "config.yaml")
		body := "templates:\n  - image: bg.png\n    boxes:\n" +
			"      - {id: title, label: \"Title\", rect: {x: 0, y: 0, w: 100, h: 100}, analyze: true}\n" +
			"      - {id: grade, label: \"Grade\", rect: {x: 0, y: 200, w: 100, h: 100}, analyze: true}\n"
		if err := os.WriteFile(cfgPath, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		var err error
		if cfg, err = LoadConfig([]string{cfgPath}); err != nil {
			t.Fatal(err)
		}
	}
	c, err := Open(root, cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return c
}

func TestMatchRegion(t *testing.T) {
	c := templatedClient(t, true)

	// Scoped to one box, the text of that box only.
	want(t, "region[title]:invoice", matched(t, c, MatchRegion("title", Substring("invoice"))), []string{"Pt"})
	want(t, "region[title]=Invoice 2026", matched(t, c, MatchRegion("title", Exact("Invoice 2026"))), []string{"Pt"})
	want(t, "region[grade]:invoice", matched(t, c, MatchRegion("grade", Substring("invoice"))), nil)
	// A declared box with no section yet reads as empty text, not as no box.
	want(t, "region[grade]=<empty>", matched(t, c, MatchRegion("grade", Exact(""))), []string{"Pt"})
	// Unscoped: any declared box.
	want(t, "region~^Invoice", matched(t, c, MatchRegion("", Regexp(regexp.MustCompile("^Invoice")))), []string{"Pt"})
	want(t, "region:nowhere", matched(t, c, MatchRegion("", Substring("nowhere"))), nil)
	// Box ids come from the config, so a section left behind for a box that is no
	// longer declared is never matched, scoped or not.
	want(t, "region[gone]:tombstoned", matched(t, c, MatchRegion("gone", Substring("tombstoned"))), nil)
	want(t, "region:tombstoned", matched(t, c, MatchRegion("", Substring("tombstoned"))), nil)
	want(t, "region[nosuch]:.", matched(t, c, MatchRegion("nosuch", Substring(""))), nil)
	// Box ids are config strings, so a numeric one is reachable from an expression.
	pred, err := ParseQuery(`region[title]:invoice OR region[1]:x`)
	if err != nil {
		t.Fatalf("ParseQuery: %v", err)
	}
	want(t, "region[title]:invoice OR region[1]:x", matched(t, c, pred), []string{"Pt"})
	// Pother holds the same text as plain content and is not templated: the gate
	// is the background hash, not the sidecar.
	want(t, "region:invoice (not Pother)", matched(t, c, MatchRegion("", Substring("invoice"))), []string{"Pt"})
}

// templated is the page-level half of the same resolution: which pages are drawn
// on a configured template, which is what `NOT templated` selects the complement of.
func TestMatchTemplated(t *testing.T) {
	c := templatedClient(t, true)

	want(t, "templated", matched(t, c, MatchTemplated), []string{"Pt"})
	want(t, "NOT templated", matched(t, c, MatchNot(MatchTemplated)), []string{"Pother", "Pplain"})
}

// With no templates: config both filters are inert — they match nothing rather
// than erroring, so an archive without the feature behaves sanely.
func TestMatchTemplatedWithoutTemplates(t *testing.T) {
	c := templatedClient(t, false)

	want(t, "templated (none configured)", matched(t, c, MatchTemplated), nil)
	want(t, "region (none configured)", matched(t, c, MatchRegion("", Substring("invoice"))), nil)
}

// A templates: section naming a missing image is a broken config: it fails at Open,
// which is why no predicate has to carry a template-resolution error.
func TestOpenRejectsBrokenTemplates(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	body := "templates:\n  - image: missing.png\n    boxes:\n" +
		"      - {id: title, rect: {x: 0, y: 0, w: 100, h: 100}, analyze: true}\n"
	if err := os.WriteFile(cfgPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig([]string{cfgPath})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Open(t.TempDir(), cfg); err == nil {
		t.Error("Open with a missing template image: want error")
	}
}

// TestQueryFailsOnUnreadableSidecar: a page whose transcription cannot be read must
// not silently count as a non-match, because MatchNot would then report it as a match —
// a query answering about text it never read. Lever: a directory where the .md goes, so
// the read fails for a reason other than "not there".
func TestQueryFailsOnUnreadableSidecar(t *testing.T) {
	c := seedFiltered(t)
	if err := os.Remove(filepath.Join(c.ArchivePath(), "F_A", "Pa.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(c.ArchivePath(), "F_A", "Pa.md"), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := c.Query(MatchContent(Substring("meeting")))
	if err == nil {
		t.Fatal("Query(content:meeting) = nil error, want the unreadable sidecar reported")
	}
	if !strings.Contains(err.Error(), "Pa") {
		t.Errorf("error %q does not name the page it failed on", err)
	}
	// The case that used to give a wrong answer: NOT inverts the false the read error
	// produced, so Pa would have been listed as a match.
	if ms, err := c.Query(MatchNot(MatchContent(Substring("meeting")))); err == nil {
		t.Errorf("Query(NOT content:meeting) = %v, nil error; want the read error", pageIDsOf(ms))
	}
	// A filter that reads only the two documents is unaffected.
	want(t, "MatchAll", matched(t, c, MatchAll), []string{"Pa", "Pb", "Pc", "Pd"})
}

// pageIDsOf is only for failure messages.
func pageIDsOf(ms []Match) []string {
	ids := make([]string, len(ms))
	for i, m := range ms {
		ids[i] = m.PageID
	}
	return ids
}
