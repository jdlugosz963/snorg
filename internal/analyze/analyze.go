// Package analyze runs vision-LLM analysis on a single archived page: it
// rasterizes the page SVG, crops each title/link region, transcribes them and the
// whole page, and writes the result into the page's <PAGEID>.json under "analysis".
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
// are vision prompts; Fields are text prompts over the content.
type Spec struct {
	Content string
	Title   string
	Link    string
	Fields  []Field
}

// Page locates the page owning pageID, analyzes it through t and g per spec, and
// writes the result into its <PAGEID>.json. Existing analysis is overwritten.
func Page(ctx context.Context, a *archive.Archive, t Transcriber, g Generator, spec Spec, pageID string) error {
	fileID, err := a.FindPage(pageID)
	if err != nil {
		return err
	}
	pd, err := a.ReadPage(fileID, pageID)
	if err != nil {
		return err
	}
	svg, err := a.ReadSVG(fileID, pageID)
	if err != nil {
		return err
	}
	img, err := rasterize(svg)
	if err != nil {
		return err
	}

	page, err := toPNG(img)
	if err != nil {
		return err
	}
	content, err := t.Transcribe(ctx, spec.Content, page)
	if err != nil {
		return fmt.Errorf("transcribe page: %w", err)
	}
	content = strings.TrimSpace(content)

	analysis := &archive.PageAnalysis{Content: content}

	for i, title := range pd.Titles {
		name, err := transcribeRegion(ctx, t, img, title.Rect, spec.Title)
		if err != nil {
			return fmt.Errorf("title %d: %w", i, err)
		}
		analysis.Titles = append(analysis.Titles, archive.TitleAnalysis{Name: name})
	}
	for i, link := range pd.Links {
		name, err := transcribeRegion(ctx, t, img, link.Rect, spec.Link)
		if err != nil {
			return fmt.Errorf("link %d: %w", i, err)
		}
		analysis.Links = append(analysis.Links, archive.LinkAnalysis{Name: name})
	}

	for _, f := range spec.Fields {
		out, err := g.Generate(ctx, f.Prompt, content)
		if err != nil {
			return fmt.Errorf("field %s: %w", f.Name, err)
		}
		if analysis.Fields == nil {
			analysis.Fields = map[string]string{}
		}
		analysis.Fields[f.Name] = strings.TrimSpace(out)
	}

	pd.Analysis = analysis
	return a.WritePage(fileID, pd)
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
