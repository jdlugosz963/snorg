package archive

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"

	"github.com/jdlugosz963/snorg/internal/snote"
)

// Template regions teach the archive that a page drawn on a known background is
// structured. The template list is folded into the merged runtime config (a
// templates: section, see internal/config) and injected here as []TemplateSpec via
// SetTemplateSpecs (pkg/snorg bridges config→archive); a page is matched to one by
// the sha256 of its decoded background image (PageDoc.BackgroundHash). Each template
// names a set of boxes — regions of the page whose transcription analyze produces
// into the <PAGEID>.md sidecar (as id-keyed sections, see regions.go), keyed by the
// box's stable id.
//
//	templates:
//	  - image: templates/prawo_jazdy.png  # device-form grayscale PNG, path per the config file
//	    boxes:
//	      - {id: title, label: "Title", rect: {x: 0, y: 0, w: 1920, h: 300}, analyze: true, prompt: "..."}
//
// rect is pixel space (1920×2560), the same as a title/link rect.
//
// The selector is the sha256 of the referenced image file bytes: that is exactly
// what backgroundHash hashes (the decoded inline image), so a page whose device
// background is that image matches. The image is stored in the device form the
// Supernote emits (8-bit grayscale PNG); a re-encoded copy hashes differently. Each
// TemplateSpec.Image is an absolute path (config.Load resolves it relative to the
// declaring config file), so building the set is a straight read+hash of that path.

// TemplateSpec is the raw, pre-hash form of one template as injected from the merged
// config: an absolute Image path and the boxes in author order. Templates() reads
// and hashes the image to produce the resolved Template.
type TemplateSpec struct {
	Image string
	Boxes []Box
}

// Box is one region of a template: an author-assigned stable id (identity only,
// never derived), a renameable human label, a pixel-space rect (`{x, y, w, h}` in
// the 1920×2560 page space — exactly like a title/link rect) and whether analyze
// transcribes it (with an optional per-box prompt). It is populated from the
// config's parsed form (config.Box) by pkg/snorg, not YAML-decoded here.
type Box struct {
	ID      string
	Label   string
	Rect    snote.Rect
	Analyze bool
	Prompt  string
}

// Template is one entry: the sha256 (hex) of its background image, filled at load
// from the referenced file, plus its boxes in author order.
type Template struct {
	Hash  string
	Image string
	Boxes []Box
}

// Templates is the loaded template set with a background-hash index.
type Templates struct {
	List   []*Template
	byHash map[string]*Template
}

// MatchBackground returns the template whose background image hashes to hash, or
// nil when none matches (an empty set always returns nil, so the feature is inert
// on an archive whose config declares no templates:).
func (t *Templates) MatchBackground(hash string) *Template {
	if t == nil || hash == "" {
		return nil
	}
	return t.byHash[hash]
}

// Box returns the template's box with the given id, or nil.
func (t *Template) Box(id string) *Box {
	for i := range t.Boxes {
		if t.Boxes[i].ID == id {
			return &t.Boxes[i]
		}
	}
	return nil
}

// AnalyzeBoxes returns the boxes marked analyze, in author order.
func (t *Template) AnalyzeBoxes() []Box {
	var out []Box
	for _, b := range t.Boxes {
		if b.Analyze {
			out = append(out, b)
		}
	}
	return out
}

// SetTemplateSpecs injects the template specs (from the merged config, via
// pkg/snorg) that Templates() builds from. Passing nil leaves the feature inert.
// It clears any cached build so the next Templates() call reflects the new specs.
func (a *Archive) SetTemplateSpecs(specs []TemplateSpec) {
	a.templateSpecs = specs
	a.templates = nil
}

// Templates builds (and caches) the resolved template set from the injected specs,
// hashing each spec's image to fill Template.Hash. No specs yields an empty set, so
// the whole feature is simply inert. A missing image file or invalid boxes
// (duplicate ids, out-of-range rects) are errors.
func (a *Archive) Templates() (*Templates, error) {
	if a.templates != nil {
		return a.templates, nil
	}
	ts := &Templates{byHash: map[string]*Template{}}
	for _, e := range a.templateSpecs {
		if e.Image == "" {
			return nil, fmt.Errorf("templates config: a template has no image")
		}
		if err := validateBoxes(e.Image, e.Boxes); err != nil {
			return nil, err
		}
		data, err := os.ReadFile(e.Image)
		if err != nil {
			return nil, fmt.Errorf("templates config: read image %s: %w", e.Image, err)
		}
		sum := sha256.Sum256(data)
		hash := hex.EncodeToString(sum[:])
		t := &Template{Hash: hash, Image: e.Image, Boxes: e.Boxes}
		if _, dup := ts.byHash[hash]; dup {
			return nil, fmt.Errorf("templates config: two templates share the background image hash %s", hash)
		}
		ts.byHash[hash] = t
		ts.List = append(ts.List, t)
	}
	a.templates = ts
	return ts, nil
}

func validateBoxes(image string, boxes []Box) error {
	seen := map[string]bool{}
	for _, b := range boxes {
		if b.ID == "" {
			return fmt.Errorf("templates config: %s has a box with no id", image)
		}
		if seen[b.ID] {
			return fmt.Errorf("templates config: %s has duplicate box id %q", image, b.ID)
		}
		seen[b.ID] = true
		r := b.Rect
		if r.W <= 0 || r.H <= 0 {
			return fmt.Errorf("templates config: %s box %q rect %+v has non-positive width/height", image, b.ID, r)
		}
		if r.X < 0 || r.Y < 0 || r.X+r.W > snote.PageWidth || r.Y+r.H > snote.PageHeight {
			return fmt.Errorf("templates config: %s box %q rect %+v is outside the %dx%d page", image, b.ID, r, snote.PageWidth, snote.PageHeight)
		}
	}
	return nil
}
