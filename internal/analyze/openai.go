package analyze

import (
	"context"
	"encoding/base64"
	"fmt"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

// openAIClient implements both Transcriber (image+prompt) and Generator
// (text+prompt) against an OpenAI-compatible chat completions endpoint.
type openAIClient struct {
	client openai.Client
	model  string
}

// NewOpenAI builds a client talking to the given OpenAI-compatible endpoint with
// the given credential and model. The returned value satisfies both Transcriber
// and Generator.
func NewOpenAI(endpoint, apiKey, model string) (*openAIClient, error) {
	if endpoint == "" || apiKey == "" || model == "" {
		return nil, fmt.Errorf("endpoint, apiKey and model are required")
	}
	client := openai.NewClient(
		option.WithBaseURL(endpoint),
		option.WithAPIKey(apiKey),
	)
	return &openAIClient{client: client, model: model}, nil
}

func (o *openAIClient) Transcribe(ctx context.Context, prompt string, imagePNG []byte) (string, error) {
	dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(imagePNG)
	return o.complete(ctx, openai.UserMessage([]openai.ChatCompletionContentPartUnionParam{
		openai.TextContentPart(prompt),
		openai.ImageContentPart(openai.ChatCompletionContentPartImageImageURLParam{URL: dataURL}),
	}))
}

func (o *openAIClient) Generate(ctx context.Context, prompt, input string) (string, error) {
	return o.complete(ctx, openai.UserMessage(prompt+"\n\n"+input))
}

func (o *openAIClient) complete(ctx context.Context, msg openai.ChatCompletionMessageParamUnion) (string, error) {
	resp, err := o.client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model:    o.model,
		Messages: []openai.ChatCompletionMessageParamUnion{msg},
	})
	if err != nil {
		return "", err
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("model returned no choices")
	}
	return resp.Choices[0].Message.Content, nil
}
