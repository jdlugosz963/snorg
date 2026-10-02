package snorg

import (
	"context"

	"github.com/jdlugosz963/snorg/internal/analyze"
)

// Provider is an analysis backend: a vision Transcriber (page/region image → text)
// and a text Generator (content → field) in one. Client.NewProvider builds the one
// the configuration describes; NewOpenAIProvider builds one for any
// OpenAI-compatible endpoint from explicit credentials. A caller may pass its own
// implementation to Analyze instead — that argument is the backend seam.
type Provider interface {
	Transcriber
	Generator
}

// NewOpenAIProvider builds a Provider backed by an OpenAI-compatible endpoint.
func NewOpenAIProvider(endpoint, apiKey, model string) (Provider, error) {
	return analyze.NewOpenAI(endpoint, apiKey, model)
}

// NewProvider builds the Provider the client's configuration describes, resolving
// the API key (api_key > api_key_command stdout > $OPENAI_API_KEY). Call it once
// and reuse the Provider across Analyze calls.
func (c *Client) NewProvider() (Provider, error) {
	// ResolveAPIKey writes the secret into its receiver, so resolve on a copy: the
	// client's own config stays free of it and Config cannot hand it out. Seeding the
	// copy with the key an earlier call resolved is what keeps a shell
	// api_key_command to one run — ResolveAPIKey's own early-out does the memoizing.
	cfg := c.cfg.Clone()
	if cfg.Provider.APIKey == "" {
		cfg.Provider.APIKey = c.apiKey
	}
	if err := cfg.ResolveAPIKey(); err != nil {
		return nil, err
	}
	if err := cfg.ValidateProvider(); err != nil {
		return nil, err
	}
	c.apiKey = cfg.Provider.APIKey
	return NewOpenAIProvider(cfg.Provider.Endpoint, c.apiKey, cfg.Provider.Model)
}

// AnalyzeOptions tunes a batch analysis.
type AnalyzeOptions struct {
	Force bool  // re-analyze even pages whose fingerprint is unchanged (blank pages/boxes still cost no call)
	Spec  *Spec // prompts to analyze with; nil = the client's config (AnalyzeSpec)

	// OnResult, when set, is called with each page's result as it lands — before
	// the next page is analyzed — so a caller can log or commit incrementally
	// instead of waiting for the whole batch. It runs synchronously on the
	// calling goroutine. Every result passed here is also in the returned slice.
	OnResult func(AnalyzeResult)
}

// AnalyzeResult is one page's PageResult plus its error; the batch continues past
// failures.
type AnalyzeResult struct {
	PageResult
	Err error
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

// Analyze transcribes each page with prov, skipping unchanged ones unless
// opts.Force. The caller owns the Provider (build it once with NewProvider), so a
// batch costs no credential setup. Pages are processed sequentially (LLM rate
// limits) and a page failure lands in its AnalyzeResult.Err without aborting the
// batch; set opts.OnResult to observe results as they land.
//
// A cancelled ctx stops the batch: the results so far are returned with ctx.Err().
func (c *Client) Analyze(ctx context.Context, prov Provider, pageIDs []string, opts AnalyzeOptions) ([]AnalyzeResult, error) {
	spec := c.AnalyzeSpec()
	if opts.Spec != nil {
		spec = *opts.Spec
	}

	results := make([]AnalyzeResult, 0, len(pageIDs))
	for _, pageID := range pageIDs {
		if err := ctx.Err(); err != nil {
			return results, err
		}
		res, err := analyze.Page(ctx, c.arch, prov, prov, spec, pageID, opts.Force)
		// analyze.Page stamps res.PageID itself, but a failure returns the zero
		// PageResult, so name the page here too — a reporter needs an id either way.
		res.PageID = pageID
		r := AnalyzeResult{PageResult: res, Err: err}
		results = append(results, r)
		if opts.OnResult != nil {
			opts.OnResult(r)
		}
	}
	return results, nil
}
