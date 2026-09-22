package snorg

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// fakeProvider satisfies Provider (Transcriber + Generator) with canned replies,
// recording every prompt so a test can prove which Spec was used.
type fakeProvider struct {
	calls   int
	prompts []string
}

func (f *fakeProvider) Transcribe(_ context.Context, prompt string, _ []byte) (string, error) {
	f.calls++
	f.prompts = append(f.prompts, prompt)
	return "transcribed", nil
}

func (f *fakeProvider) Generate(_ context.Context, prompt, _ string) (string, error) {
	f.calls++
	f.prompts = append(f.prompts, prompt)
	return "generated", nil
}

// TestAnalyzeStreamsResults drives a batch with an unknown PAGEID in the middle:
// OnResult must see every page as it lands (before the next one is analyzed), the
// failure must not abort the batch, and the callback stream must match the return.
func TestAnalyzeStreamsResults(t *testing.T) {
	c := seedArchive(t)
	prov := &fakeProvider{}

	var seen []AnalyzeResult
	var callsAtCallback []int
	results, err := c.Analyze(context.Background(), prov, []string{"P1", "nope", "P2"}, AnalyzeOptions{
		OnResult: func(r AnalyzeResult) {
			seen = append(seen, r)
			callsAtCallback = append(callsAtCallback, prov.calls)
		},
	})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("Analyze = %d results, want 3", len(results))
	}
	if len(seen) != len(results) {
		t.Fatalf("OnResult fired %d times, want %d", len(seen), len(results))
	}
	for i, r := range results {
		if seen[i].PageID != r.PageID || !reflect.DeepEqual(seen[i].PageResult, r.PageResult) {
			t.Errorf("OnResult[%d] = %+v, want %+v", i, seen[i], r)
		}
	}
	if results[1].Err == nil {
		t.Error("unknown PAGEID: want an error in AnalyzeResult.Err")
	}
	for _, i := range []int{0, 2} {
		if results[i].Err != nil {
			t.Errorf("%s: unexpected error %v", results[i].PageID, results[i].Err)
		}
		if results[i].Content == nil || results[i].Content.Kind != TextNew {
			t.Errorf("%s content = %+v, want %q", results[i].PageID, results[i].Content, TextNew)
		}
	}
	// The first callback ran before the last page's LLM calls — i.e. results are
	// observable mid-batch rather than only after it.
	if callsAtCallback[0] >= prov.calls {
		t.Errorf("OnResult[0] saw %d provider calls, want fewer than the final %d", callsAtCallback[0], prov.calls)
	}
}

// TestAnalyzeSpecOverride proves opts.Spec replaces the client's config prompts —
// the seam that replaced the removed AnalyzePage.
func TestAnalyzeSpecOverride(t *testing.T) {
	c := seedArchive(t)
	prov := &fakeProvider{}
	spec := Spec{Content: "CUSTOM-CONTENT-PROMPT"}

	if _, err := c.Analyze(context.Background(), prov, []string{"P1"}, AnalyzeOptions{Spec: &spec}); err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if len(prov.prompts) == 0 {
		t.Fatal("provider saw no prompts")
	}
	for _, p := range prov.prompts {
		if !strings.HasPrefix(p, "CUSTOM-CONTENT-PROMPT") {
			t.Errorf("prompt %q does not come from the override Spec", p)
		}
	}

	// Without the override the config's default content prompt is used instead.
	def := &fakeProvider{}
	if _, err := c.Analyze(context.Background(), def, []string{"P2"}, AnalyzeOptions{}); err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if len(def.prompts) == 0 || strings.HasPrefix(def.prompts[0], "CUSTOM-CONTENT-PROMPT") {
		t.Errorf("default prompts = %q, want the config's", def.prompts)
	}
}

// TestAnalyzeContextCancel: a cancelled context stops the batch instead of failing
// every remaining page one LLM call at a time.
func TestAnalyzeContextCancel(t *testing.T) {
	c := seedArchive(t)
	prov := &fakeProvider{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	results, err := c.Analyze(ctx, prov, []string{"P1", "P2"}, AnalyzeOptions{})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Analyze error = %v, want context.Canceled", err)
	}
	if len(results) != 0 {
		t.Errorf("results = %v, want none", results)
	}
	if prov.calls != 0 {
		t.Errorf("provider called %d times after cancel, want 0", prov.calls)
	}
}

// TestNewProviderValidates: credentials are checked when the Provider is built, not
// on the first page.
func TestNewProviderValidates(t *testing.T) {
	c := seedArchive(t)
	c.cfg.Provider.Endpoint = "https://example.invalid/v1"
	c.cfg.Provider.APIKey = "k"
	c.cfg.Provider.Model = "" // missing

	if _, err := c.NewProvider(); err == nil {
		t.Fatal("NewProvider with no model = nil error, want a validation failure")
	}

	c.cfg.Provider.Model = "m"
	prov, err := c.NewProvider()
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	if prov == nil {
		t.Fatal("NewProvider returned a nil Provider")
	}
}

// TestNewProviderResolvesKeyOnce: api_key_command is a subprocess; building the
// Provider twice must not run it twice (the reason to build once at startup).
func TestNewProviderResolvesKeyOnce(t *testing.T) {
	c := seedArchive(t)
	marker := filepath.Join(t.TempDir(), "runs")
	c.cfg.Provider.Endpoint = "https://example.invalid/v1"
	c.cfg.Provider.Model = "m"
	c.cfg.Provider.APIKey = ""
	c.cfg.Provider.APIKeyCommand = "echo run >> " + marker + "; echo secret"

	for i := 0; i < 2; i++ {
		if _, err := c.NewProvider(); err != nil {
			t.Fatalf("NewProvider #%d: %v", i+1, err)
		}
	}
	if got := c.cfg.Provider.APIKey; got != "secret" {
		t.Errorf("resolved api key = %q, want %q", got, "secret")
	}
	b, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	if runs := len(strings.Fields(string(b))); runs != 1 {
		t.Errorf("api_key_command ran %d times, want 1", runs)
	}
}
