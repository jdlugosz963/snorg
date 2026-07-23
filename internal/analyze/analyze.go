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
	"crypto/sha256"
	"encoding/hex"
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
	hash := pathHash(svg)
	if !force && pd.Analysis != nil && pd.Analysis.SourceHash == hash {
		return Skipped, nil
	}

	// Only rasterize once the page is known to need analysis (the skip check above
	// works off the SVG bytes alone).
	img, err := rasterize(svg)
	if err != nil {
		return "", err
	}
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

	for i, title := range pd.Titles {
		name, err := transcribeRegion(ctx, t, img, title.Rect, spec.Title)
		if err != nil {
			return "", fmt.Errorf("title %d: %w", i, err)
		}
		pd.Titles[i].Analysis = &archive.TitleAnalysis{Name: name}
	}
	for i, link := range pd.Links {
		name, err := transcribeRegion(ctx, t, img, link.Rect, spec.Link)
		if err != nil {
			return "", fmt.Errorf("link %d: %w", i, err)
		}
		pd.Links[i].Analysis = &archive.LinkAnalysis{Name: name}
	}

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

	pd.Analysis = analysis
	if err := a.WritePage(fileID, pd); err != nil {
		return "", err
	}
	return outcome, nil
}

// pathHash fingerprints a page by the geometry of its handwriting: the d
// attribute of every <path>, whitespace-normalized and concatenated in document
// order. It is invariant under everything the SVG pipeline does that does not
// change the drawing itself — recolor (rewrites fill=), background mode (touches
// the <image>), the link/nav overlays (<a><rect>, no d) and the formatSVG reflow
// (only whitespace within d) — so none of those force a re-analysis, while an
// actual stroke edit (a different potrace d) does. It reads the SVG bytes alone,
// so the skip decision needs no rasterization.
func pathHash(svg []byte) string {
	h := sha256.New()
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
			h.Write([]byte(strings.Join(strings.Fields(d), " ")))
			h.Write([]byte{'\n'})
		}
		s = s[end+1:]
	}
	return hex.EncodeToString(h.Sum(nil))
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
