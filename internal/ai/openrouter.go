package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/yay101/mediarr/internal/config"
)

func init() {
	Register("openrouter", newOpenRouterProvider)
}

type openRouterProvider struct {
	cfg     *config.AIConfig
	aiKey   string
	model   string
	baseURL string
}

type openRouterRequest struct {
	Model    string `json:"model"`
	Messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
}

type openRouterResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

func newOpenRouterProvider(cfg *config.AIConfig) (Provider, error) {
	p := &openRouterProvider{
		cfg:     cfg,
		aiKey:   cfg.OpenAI.APIKey,
		model:   cfg.OpenAI.Model,
		baseURL: "https://openrouter.ai/api/v1",
	}
	if p.model == "" {
		p.model = "openai/gpt-4o"
	}
	return p, nil
}

func (p *openRouterProvider) Chat(ctx context.Context, messages []Message) (*Response, error) {
	reqBody := openRouterRequest{Model: p.model}
	for _, m := range messages {
		reqBody.Messages = append(reqBody.Messages, struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}{Role: m.Role, Content: m.Content})
	}

	data, _ := json.Marshal(reqBody)
	req, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/chat/completions", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.aiKey)
	req.Header.Set("HTTP-Referer", "https://mediarr.local")
	req.Header.Set("X-Title", "mediarr")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openrouter request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openrouter returned status %d: %s", resp.StatusCode, string(body))
	}

	var orResp openRouterResponse
	if err := json.Unmarshal(body, &orResp); err != nil {
		return nil, fmt.Errorf("failed to parse openrouter response: %w", err)
	}

	if len(orResp.Choices) == 0 {
		return nil, fmt.Errorf("no choices returned")
	}

	usage := orResp.Usage
	return &Response{
		Content: orResp.Choices[0].Message.Content,
		Usage: Usage{
			PromptTokens:     usage.PromptTokens,
			CompletionTokens: usage.CompletionTokens,
			TotalTokens:      usage.TotalTokens,
		},
	}, nil
}

func (p *openRouterProvider) ChatJSON(ctx context.Context, messages []Message, schema any) (*Response, error) {
	resp, err := p.Chat(ctx, messages)
	if err != nil {
		return nil, err
	}
	resp.Content = CleanJSON(resp.Content)
	return resp, nil
}

func (p *openRouterProvider) Name() string {
	return "openrouter"
}

func (p *openRouterProvider) SupportsJSON() bool {
	return true
}
