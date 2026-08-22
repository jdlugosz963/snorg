// Package analyze runs vision-LLM analysis on a single archived page: it
// rasterizes the page SVG, crops each title/link region, transcribes them and the
// whole page, and writes the result into the page's <PAGEID>.json (per-region
// names, custom fields) and <PAGEID>.md sidecar (the transcribed content).
//
// Analysis is incremental: a hash of the rasterized page is stored alongside the
// analysis, unchanged pages are skipped without any LLM call, and re-analysis of
// a changed page feeds the previous transcription back through an update prompt
// so the new content diffs minimally against the old.
//
// It is the only module with external dependencies (an LLM client and an SVG
// rasterizer); the Transcriber seam keeps the orchestration testable without a
// network. How/when analysis is triggered in bulk, and credential handling, are
// out of scope here.
package analyze

import (
	"context"
	"fmt"
	"image"
	"strings"

	"github.com/jdlugosz963/snorg/internal/archive"
	"github.com/jdlugosz963/snorg/internal/snote"
)

// Transcriber sends one image plus a prompt to a vision model and returns the
// model's plaintext reply. It is the seam isolating the LLM provider for the
// image-based tasks (content, titles, links).
type Transcriber interface {
	Transcribe(ctx context.Context, prompt string, imagePNG []byte) (string, error)
}

// Generator sends a prompt plus input text to a model and returns its reply. It
// drives the custom Fields, which are derived from the transcribed content rather
// than from the page image (cheaper, text-only).
type Generator interface {
	Generate(ctx context.Context, prompt, input string) (string, error)
}

// Field is one custom analysis output: its name (the key under analysis.fields)
// and the prompt run against the transcribed content.
type Field struct {
	Name   string
	Prompt string
}

// Spec carries the fully-resolved prompts for one analysis run. Content/Title/Link
// are vision prompts; Update is the content prompt used when a previous
// transcription exists (it is sent with the previous content appended, so the
// model minimizes the diff); Fields are text prompts over the content.
type Spec struct {
	Content string
	Update  string
	Title   string
	Link    string
	Fields  []Field
}

// Outcome says what Page did: skipped an unchanged page, analyzed a fresh one,
// updated an existing analysis against the previous transcription, or left
// conflict markers where the new analysis and the user's edits overlap.
type Outcome string

const (
	Skipped    Outcome = "skipped"
	Analyzed   Outcome = "analyzed"
	Updated    Outcome = "updated"
	Conflicted Outcome = "conflict" // resolve via analyze-edit
)

// Page locates the page owning pageID and analyzes it through t and g per spec:
// the transcribed content goes to the <PAGEID>.md sidecar, per-region names and
// custom fields into <PAGEID>.json. A page whose rasterized pixels match the
// stored analysis source hash is skipped without any LLM call unless force is
// set; a changed page with a previous transcription is re-analyzed through the
// update prompt so the new content diffs minimally against the old. User edits
// (analyze-edit) are 3-way merged back onto the new transcription; overlaps
// leave conflict markers in the md and report the Conflicted outcome.
func Page(ctx context.Context, a *archive.Archive, t Transcriber, g Generator, spec Spec, pageID string, force bool) (Outcome, error) {
	fileID, err := a.FindPage(pageID)
	if err != nil {
		return "", err
	}
	pd, err := a.ReadPage(fileID, pageID)
	if err != nil {
		return "", err
	}
	svg, err := a.ReadSVG(fileID, pageID)
	if err != nil {
		return "", err
	}
	// A page drawn on a known template is matched to it by its background hash and
	// analyzed per region (docs/templates); everything else keeps the whole-page
	// content path. The template set (from the merged config, injected into the
	// archive) needs no provider creds. It is resolved before the skip check because
	// a templated page's skip also depends on the per-box fingerprints, not just
	// page geometry.
	templates, err := a.Templates()
	if err != nil {
		return "", err
	}
	tmpl := templates.MatchBackground(pd.BackgroundHash)

	// One canonical black-on-white rasterization drives everything: the page hash,
	// the per-box region hashes, and the LLM crops (so a Page call rasterizes
	// exactly once, on every path including a skip). It is built before the skip
	// check because the fingerprint is the ink mask of this very image.
	img, err := rasterize(canonicalSVG(svg))
	if err != nil {
		return "", err
	}
	m := newMask(img)
	hash := m.hash()
	if !force && pd.Analysis != nil && pd.Analysis.SourceHash == hash {
		// An unchanged page skips a non-templated page outright; a templated page
		// skips only when every analyze box's fingerprint also matches, so a
		// moved/added box rect (config change, same handwriting) still re-triggers.
		if tmpl == nil || regionsCurrent(m, tmpl, pd) {
			return Skipped, nil
		}
	}

	// A templated page transcribes per region; every other page transcribes its
	// whole content. Either runs before the title/link regions so the page/content
	// image is the first LLM call.
	var outcome Outcome
	if tmpl != nil {
		outcome, err = analyzeRegions(ctx, a, t, spec, img, m, fileID, pageID, &pd, tmpl, hash, force)
	} else {
		outcome, err = analyzeContent(ctx, a, t, g, spec, img, fileID, pageID, &pd, hash)
	}
	if err != nil {
		return "", err
	}

	// Title/link region transcription is template-independent (device metadata) and
	// runs for every page. A user-overridden name (analyze-edit set Edited) is kept
	// as-is and not re-transcribed — the override wins and it saves an LLM call.
	// --force only bypasses the source-hash skip, it never discards user edits.
	for i, title := range pd.Titles {
		if title.Analysis != nil && title.Analysis.Edited {
			continue
		}
		name, err := transcribeRegion(ctx, t, img, title.Rect, spec.Title)
		if err != nil {
			return "", fmt.Errorf("title %d: %w", i, err)
		}
		pd.Titles[i].Analysis = &archive.TitleAnalysis{Name: name}
	}
	for i, link := range pd.Links {
		if link.Analysis != nil && link.Analysis.Edited {
			continue
		}
		name, err := transcribeRegion(ctx, t, img, link.Rect, spec.Link)
		if err != nil {
			return "", fmt.Errorf("link %d: %w", i, err)
		}
		pd.Links[i].Analysis = &archive.LinkAnalysis{Name: name}
	}

	if err := a.WritePage(fileID, pd); err != nil {
		return "", err
	}
	return outcome, nil
}

// analyzeContent transcribes the whole page into the <PAGEID>.md content sidecar
// (the non-template path), 3-way-merging with user edits and generating custom
// fields from the effective content. It sets pd.Analysis (source hash + fields).
func analyzeContent(ctx context.Context, a *archive.Archive, t Transcriber, g Generator, spec Spec, img *image.RGBA, fileID, pageID string, pd *archive.PageDoc, hash string) (Outcome, error) {
	page, err := toPNG(img)
	if err != nil {
		return "", err
	}
	// "Previous" is the AI base: the md with any user edit diff reverse-applied
	// (see archive.ReadAnalysisBase). The LLM never sees the user's edits; they
	// are re-applied by the 3-way merge below. A page with only user-written
	// content has an empty base and gets a fresh transcription.
	outcome := Analyzed
	base, err := a.ReadAnalysisBase(fileID, pageID)
	if err != nil {
		return "", err
	}
	prompt := spec.Content
	if base != "" {
		outcome = Updated
		prompt = spec.Update + "\n\n" + base
	}
	content, err := t.Transcribe(ctx, prompt, page)
	if err != nil {
		return "", fmt.Errorf("transcribe page: %w", err)
	}
	content = strings.TrimSpace(content)

	// The merge writes the md: theirs (the fresh transcription) reconciled with
	// any user edits. Fields derive from the effective content — what the user
	// actually sees — not the raw LLM output.
	effective, conflicts, err := a.MergeAnalysis(fileID, pageID, content)
	if err != nil {
		return "", err
	}
	if conflicts {
		outcome = Conflicted
	}

	analysis := &archive.PageAnalysis{SourceHash: hash}
	for _, f := range spec.Fields {
		out, err := g.Generate(ctx, f.Prompt, effective)
		if err != nil {
			return "", fmt.Errorf("field %s: %w", f.Name, err)
		}
		if analysis.Fields == nil {
			analysis.Fields = map[string]string{}
		}
		analysis.Fields[f.Name] = strings.TrimSpace(out)
	}
	pd.Analysis = analysis // a non-template page carries no region state (analysis.Regions stays nil)
	return outcome, nil
}

// analyzeRegions transcribes each analyze:true box of the matched template into
// the <PAGEID>.md sidecar as id-keyed sections (a templated page's md holds these
// sections in place of free-form content). A box whose region fingerprint is
// unchanged (and not force) reuses its previous AI text without an LLM call;
// changed boxes are cropped and re-transcribed. The assembled document (config box
// order, then tombstoned sections for ids no longer in the config) is 3-way-merged
// with any user edits, and pd.Analysis.Regions records each analyze box's new fingerprint.
func analyzeRegions(ctx context.Context, a *archive.Archive, t Transcriber, spec Spec, img *image.RGBA, m *mask, fileID, pageID string, pd *archive.PageDoc, tmpl *archive.Template, hash string, force bool) (Outcome, error) {
	base, err := a.ReadAnalysisBase(fileID, pageID)
	if err != nil {
		return "", err
	}
	baseSections := archive.ParseRegions(base)

	prev := map[string]string{}
	if pd.Analysis != nil {
		for _, r := range pd.Analysis.Regions {
			prev[r.ID] = r.SourceHash
		}
	}

	var sections []archive.RegionSection
	var regions []archive.RegionDoc
	known := map[string]bool{}
	for _, box := range tmpl.Boxes {
		known[box.ID] = true
		if !box.Analyze {
			// A non-analyze box never gets a fresh transcription, but preserve any
			// existing text (e.g. a box toggled off) rather than dropping it.
			if txt := archive.RegionText(baseSections, box.ID); txt != "" {
				sections = append(sections, archive.RegionSection{ID: box.ID, Label: box.Label, Text: txt})
			}
			continue
		}
		rh := m.regionHash(box.Rect)
		var text string
		if !force && prev[box.ID] == rh {
			text = archive.RegionText(baseSections, box.ID) // unchanged: reuse AI base
		} else {
			png, err := crop(img, box.Rect)
			if err != nil {
				return "", fmt.Errorf("region %s: %w", box.ID, err)
			}
			prompt := box.Prompt
			if prompt == "" {
				prompt = spec.Content
			}
			out, err := t.Transcribe(ctx, prompt, png)
			if err != nil {
				return "", fmt.Errorf("region %s: %w", box.ID, err)
			}
			text = strings.TrimSpace(out)
		}
		sections = append(sections, archive.RegionSection{ID: box.ID, Label: box.Label, Text: text})
		regions = append(regions, archive.RegionDoc{ID: box.ID, SourceHash: rh})
	}
	// Tombstone sections whose id is no longer any config box: keep them at the end
	// (never delete transcribed text), ignored by export.
	for _, s := range baseSections {
		if !known[s.ID] {
			sections = append(sections, s)
		}
	}

	theirs := archive.AssembleRegions(sections)
	_, conflicts, err := a.MergeAnalysis(fileID, pageID, theirs)
	if err != nil {
		return "", err
	}

	outcome := Analyzed
	if strings.TrimSpace(base) != "" {
		outcome = Updated
	}
	if conflicts {
		outcome = Conflicted
	}
	// Keep a page-level source hash so the whole-page skip and list/query metadata
	// still work; the region sections are the md content, so no fields here. The
	// per-box fingerprints ride along under Analysis (analyze-produced state).
	pd.Analysis = &archive.PageAnalysis{SourceHash: hash, Regions: regions}
	return outcome, nil
}

// regionsCurrent reports whether pd.Analysis.Regions already matches the template's
// analyze boxes at their current rects: same set of ids and same per-box
// fingerprints (each a crop of the canonical page mask). A false means either the
// config changed (a box moved, was added or removed) or a box's cropped ink
// changed, so the page must be re-analyzed. The mask is already in hand from the
// page-hash check, so this adds no rasterization.
func regionsCurrent(m *mask, tmpl *archive.Template, pd archive.PageDoc) bool {
	stored := map[string]string{}
	if pd.Analysis != nil {
		for _, r := range pd.Analysis.Regions {
			stored[r.ID] = r.SourceHash
		}
	}
	boxes := tmpl.AnalyzeBoxes()
	if len(boxes) != len(stored) {
		return false
	}
	for _, box := range boxes {
		h, ok := stored[box.ID]
		if !ok || h != m.regionHash(box.Rect) {
			return false
		}
	}
	return true
}

// forEachPathData calls fn with the d of every <path> element in document order.
// It is the shared scan behind canonicalSVG (which renders the page's handwriting
// to the fingerprint mask).
func forEachPathData(svg []byte, fn func(d string)) {
	s := string(svg)
	for {
		i := strings.Index(s, "<path")
		if i < 0 {
			break
		}
		s = s[i:]
		end := strings.IndexByte(s, '>')
		if end < 0 {
			break
		}
		if d, ok := pathData(s[:end+1]); ok {
			fn(d)
		}
		s = s[end+1:]
	}
}

// pathData returns the d attribute value of a single <path .../> element.
func pathData(elem string) (string, bool) {
	di := strings.Index(elem, ` d="`)
	if di < 0 {
		return "", false
	}
	start := di + len(` d="`)
	q := strings.IndexByte(elem[start:], '"')
	if q < 0 {
		return "", false
	}
	return elem[start : start+q], true
}

func transcribeRegion(ctx context.Context, t Transcriber, img *image.RGBA, r snote.Rect, prompt string) (string, error) {
	png, err := crop(img, r)
	if err != nil {
		return "", err
	}
	name, err := t.Transcribe(ctx, prompt, png)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(name), nil
}
