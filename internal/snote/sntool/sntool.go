// Package sntool implements snote.Source by shelling out to the `supernote-tool`
// CLI (supernotelib). It is one interchangeable adapter behind the snote.Source
// seam; a native-Go parser could replace it without touching callers.
package sntool

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"

	"github.com/jdlugosz963/snorg/internal/snote"
)

// Binary is the external tool invoked for parsing and rendering.
const Binary = "supernote-tool"

// Source is the supernote-tool backed implementation of snote.Source.
type Source struct{}

// New returns a supernote-tool backed Source.
func New() *Source { return &Source{} }

type analyzeOut struct {
	Signature string                       `json:"__signature__"`
	Header    map[string]string            `json:"__header__"`
	Footer    json.RawMessage              `json:"__footer__"`
	Pages     []map[string]json.RawMessage `json:"__pages__"`
}

type footerArrays struct {
	Titles   []map[string]string `json:"__titles__"`
	Keywords []map[string]string `json:"__keywords__"`
	Links    []map[string]string `json:"__links__"`
}

// Read runs `supernote-tool analyze` and maps the result into the domain model.
func (s *Source) Read(path string) (*snote.Note, error) {
	out, err := exec.Command(Binary, "analyze", path).Output()
	if err != nil {
		return nil, fmt.Errorf("%s analyze: %w", Binary, err)
	}

	var a analyzeOut
	if err := json.Unmarshal(out, &a); err != nil {
		return nil, fmt.Errorf("parse analyze output: %w", err)
	}
	var fa footerArrays
	if err := json.Unmarshal(a.Footer, &fa); err != nil {
		return nil, fmt.Errorf("parse footer arrays: %w", err)
	}
	footerMap := map[string]json.RawMessage{}
	if err := json.Unmarshal(a.Footer, &footerMap); err != nil {
		return nil, fmt.Errorf("parse footer keys: %w", err)
	}
	footerKeys := make([]string, 0, len(footerMap))
	for k := range footerMap {
		footerKeys = append(footerKeys, k)
	}

	note := &snote.Note{
		FileID:    a.Header["FILE_ID"],
		Signature: a.Signature,
		Device:    a.Header["APPLY_EQUIPMENT"],
	}
	for i, p := range a.Pages {
		note.Pages = append(note.Pages, snote.Page{
			ID:      rawString(p["PAGEID"]),
			Number:  i + 1,
			Starred: isStarred(p["FIVESTAR"]),
		})
	}
	pageAt := func(n int) *snote.Page {
		if n < 1 || n > len(note.Pages) {
			return nil
		}
		return &note.Pages[n-1]
	}

	titlePages := pageByPoint(footerKeys, prefixTitle)
	for _, t := range fa.Titles {
		r, ok := parseRectCSV(t["TITLERECT"])
		if !ok {
			continue
		}
		p := pageAt(titlePages[pointOf(r)])
		if p == nil {
			continue
		}
		level, _ := strconv.Atoi(t["TITLELEVEL"])
		seq, _ := strconv.Atoi(t["TITLESEQNO"])
		p.Titles = append(p.Titles, snote.Title{Rect: r, Level: level, Seq: seq})
	}

	linkPages := pageByPoint(footerKeys, prefixLink)
	for _, l := range fa.Links {
		r, ok := parseRectCSV(l["LINKRECT"])
		if !ok {
			continue
		}
		p := pageAt(linkPages[pointOf(r)])
		if p == nil {
			continue
		}
		p.Links = append(p.Links, snote.Link{
			Rect:         r,
			TargetPageID: l["PAGEID"],
			TargetFileID: l["LINKFILEID"],
			Name:         linkName(l["LINKFILE"]),
		})
	}

	kwPages := keywordPages(footerKeys)
	for i, k := range fa.Keywords {
		if i >= len(kwPages) {
			break
		}
		p := pageAt(kwPages[i])
		if p == nil {
			continue
		}
		p.Keywords = append(p.Keywords, snote.Keyword{Text: k["KEYWORD"]})
	}

	return note, nil
}

// RenderSVG renders a single 0-based page to SVG via `supernote-tool convert`.
func (s *Source) RenderSVG(path string, pageIndex int) ([]byte, error) {
	dir, err := os.MkdirTemp("", "snorg-svg")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	outPath := filepath.Join(dir, "page.svg")
	cmd := exec.Command(Binary, "convert", "-n", strconv.Itoa(pageIndex), "-t", "svg", path, outPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("%s convert page %d: %w: %s", Binary, pageIndex, err, out)
	}
	return os.ReadFile(outPath)
}

func rawString(r json.RawMessage) string {
	if len(r) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(r, &s); err == nil {
		return s
	}
	return ""
}

func isStarred(r json.RawMessage) bool {
	s := rawString(r)
	return s != "" && s != "0"
}

// linkName decodes LINKFILE (base64 of an on-device path like
// "/storage/emulated/0/Note/linked-note.note") into the bare note name
// "linked-note". It returns "" when b64 is empty or undecodable.
func linkName(b64 string) string {
	if b64 == "" {
		return ""
	}
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return ""
	}
	base := path.Base(string(raw))
	return base[:len(base)-len(path.Ext(base))]
}
