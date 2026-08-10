package snorg

import (
	"context"

	"github.com/jdlugosz963/snorg/internal/analyze"
)

// Provider is an analysis backend: a vision Transcriber (page/region image → text)
// and a text Generator (content → field) in one. NewOpenAIProvider builds one for
// any OpenAI-compatible endpoint; a caller may supply its own implementation to
// AnalyzePage.
type Provider interface {
	Transcriber
	Generator
}

// NewOpenAIProvider builds a Provider backed by an OpenAI-compatible endpoint.
func NewOpenAIProvider(endpoint, apiKey, model string) (Provider, error) {
	return analyze.NewOpenAI(endpoint, apiKey, model)
}

// AnalyzeOptions tunes a batch analysis.
type AnalyzeOptions struct {
	Force bool // re-analyze even pages whose path geometry is unchanged
}

// AnalyzeResult is one page's analysis outcome; Err is set when that page failed
// (the batch continues past failures).
type AnalyzeResult struct {
	PageID  string
	Outcome Outcome
	Err     error
}

// AnalyzeSpec builds the analysis Spec from the client's configuration (the content/
// title/link prompts plus any custom fields).
func (c *Client) AnalyzeSpec() Spec {
	spec := analyze.Spec{
		Content: c.cfg.Analysis.Content.Prompt,
		Update:  c.cfg.Analysis.Content.UpdatePrompt,
		Title:   c.cfg.Analysis.Titles.Prompt,
		Link:    c.cfg.Analysis.Links.Prompt,
	}
	for name, t := range c.cfg.Analysis.Fields {
		spec.Fields = append(spec.Fields, analyze.Field{Name: name, Prompt: t.Prompt})
	}
	return spec
}

// Analyze transcribes each page, skipping unchanged ones unless opts.Force. It
// resolves the provider API key and builds an OpenAI provider plus the Spec from the
// client's configuration, then processes pages sequentially (LLM rate limits); a
// page failure lands in its AnalyzeResult.Err without aborting the batch. To inject
// a custom Provider (or one built once for many calls), use AnalyzePage.
func (c *Client) Analyze(ctx context.Context, pageIDs []string, opts AnalyzeOptions) ([]AnalyzeResult, error) {
	if err := c.cfg.ResolveAPIKey(); err != nil {
		return nil, err
	}
	if err := c.cfg.ValidateProvider(); err != nil {
		return nil, err
	}
	prov, err := NewOpenAIProvider(c.cfg.Provider.Endpoint, c.cfg.Provider.APIKey, c.cfg.Provider.Model)
	if err != nil {
		return nil, err
	}
	spec := c.AnalyzeSpec()

	results := make([]AnalyzeResult, 0, len(pageIDs))
	for _, pageID := range pageIDs {
		outcome, err := analyze.Page(ctx, c.arch, prov, prov, spec, pageID, opts.Force)
		results = append(results, AnalyzeResult{PageID: pageID, Outcome: outcome, Err: err})
	}
	return results, nil
}

// AnalyzePage analyzes one page with a caller-supplied Provider and Spec — the seam
// for an alternate backend or a test double. force re-analyzes even an unchanged
// page.
func (c *Client) AnalyzePage(ctx context.Context, prov Provider, spec Spec, pageID string, force bool) (Outcome, error) {
	return analyze.Page(ctx, c.arch, prov, prov, spec, pageID, force)
}
