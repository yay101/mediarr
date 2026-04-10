package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/yay101/mediarr/config"
)

func init() {
	Register("anthropic", newAnthropicProvider)
}

type anthropicProvider struct {
	cfg   *config.AIConfig
	model string
}

type anthropicRequest struct {
	Model     string `json:"model"`
	MaxTokens int    `json:"max_tokens"`
	Messages  []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
}

type anthropicResponse struct {
	Content []struct {
		Text string `json:"text"`
	} `json:"content"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

func newAnthropicProvider(cfg *config.AIConfig) (Provider, error) {
	p := &anthropicProvider{
		cfg:   cfg,
		model: cfg.Anthropic.Model,
	}
	if p.model == "" {
		p.model = "claude-sonnet-4-20250514"
	}
	return p, nil
}

func (p *anthropicProvider) Chat(ctx context.Context, messages []Message) (*Response, error) {
	reqBody := anthropicRequest{
		Model:     p.model,
		MaxTokens: 4096,
	}

	for _, m := range messages {
		role := m.Role
		if role == "system" {
			role = "user"
		}
		reqBody.Messages = append(reqBody.Messages, struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}{Role: role, Content: m.Content})
	}

	data, _ := json.Marshal(reqBody)
	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.cfg.Anthropic.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("anthropic request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("anthropic returned status %d: %s", resp.StatusCode, string(body))
	}

	var anResp anthropicResponse
	if err := json.Unmarshal(body, &anResp); err != nil {
		return nil, fmt.Errorf("failed to parse anthropic response: %w", err)
	}

	if len(anResp.Content) == 0 {
		return nil, fmt.Errorf("no content returned")
	}

	usage := anResp.Usage
	return &Response{
		Content: anResp.Content[0].Text,
		Usage: Usage{
			PromptTokens:     usage.InputTokens,
			CompletionTokens: usage.OutputTokens,
			TotalTokens:      usage.InputTokens + usage.OutputTokens,
		},
	}, nil
}

func (p *anthropicProvider) ChatJSON(ctx context.Context, messages []Message, schema any) (*Response, error) {
	resp, err := p.Chat(ctx, messages)
	if err != nil {
		return nil, err
	}
	resp.Content = CleanJSON(resp.Content)
	return resp, nil
}

func (p *anthropicProvider) Name() string {
	return "anthropic"
}

func (p *anthropicProvider) SupportsJSON() bool {
	return true
}
