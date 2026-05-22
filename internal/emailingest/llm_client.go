package emailingest

import (
	"context"
	"fmt"

	"github.com/pgEdge/pgedge-go-llm-lib/llm"
	_ "github.com/pgEdge/pgedge-go-llm-lib/llm/all" // register all providers
)

// RealLLM wraps an llm.Client and satisfies the LLM interface used by Extractor.
type RealLLM struct {
	Client llm.Client
}

// NewRealLLM constructs an LLM client via pgedge-go-llm-lib for the given
// provider ("anthropic", "openai", "gemini", "ollama") and model.
func NewRealLLM(provider, model, apiKey string) (*RealLLM, error) {
	c, err := llm.NewClient(provider, llm.Options{APIKey: apiKey, Model: model})
	if err != nil {
		return nil, fmt.Errorf("llm client (%s): %w", provider, err)
	}
	return &RealLLM{Client: c}, nil
}

// Complete sends prompt as a single user message and returns the model's
// first text content block, asking for JSON-formatted output.
func (r *RealLLM) Complete(ctx context.Context, prompt string) (string, error) {
	resp, err := r.Client.Chat(ctx, llm.ChatRequest{
		Messages:       []llm.Message{llm.UserText(prompt)},
		ResponseFormat: &llm.ResponseFormat{Type: llm.ResponseFormatJSON},
	})
	if err != nil {
		return "", err
	}
	for _, b := range resp.Content {
		if b.Type == llm.BlockText {
			return b.Text, nil
		}
	}
	return "", fmt.Errorf("llm response had no text block")
}
