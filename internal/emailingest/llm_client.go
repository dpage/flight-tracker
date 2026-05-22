package emailingest

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

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

// Complete sends prompt + any document attachments as a single user
// message and returns the model's first text content block, asking for
// JSON-formatted output. If the provider rejects documents
// (llm.ErrNotSupported, currently OpenAI and Ollama), Complete logs a
// warning and retries text-only so the email can still be partially
// processed.
func (r *RealLLM) Complete(ctx context.Context, prompt string, docs []Document) (string, error) {
	blocks := make([]llm.ContentBlock, 0, 1+len(docs))
	blocks = append(blocks, llm.TextBlock(prompt))
	for _, d := range docs {
		blocks = append(blocks, llm.DocumentBlock(d.Data, d.MediaType, d.Filename))
	}
	resp, err := r.Client.Chat(ctx, llm.ChatRequest{
		Messages:       []llm.Message{llm.UserBlocks(blocks...)},
		ResponseFormat: &llm.ResponseFormat{Type: llm.ResponseFormatJSON},
	})
	if errors.Is(err, llm.ErrNotSupported) && len(docs) > 0 {
		slog.Warn("emailingest: LLM provider rejected documents, retrying text-only",
			"docs", len(docs))
		return r.Complete(ctx, prompt, nil)
	}
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
