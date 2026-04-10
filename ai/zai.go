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
	Register("zai", newZAIProvider)
}

type zaiProvider struct {
	cfg   *config.AIConfig
	model string
}

type zaiRequest struct {
	Model    string `json:"model"`
	Messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
}

type zaiResponse struct {
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

func newZAIProvider(cfg *config.AIConfig) (Provider, error) {
	p := &zaiProvider{
		cfg:   cfg,
		model: cfg.ZAI.Model,
	}
	if p.model == "" {
		p.model = "glm-4"
	}
	return p, nil
}

func (p *zaiProvider) Chat(ctx context.Context, messages []Message) (*Response, error) {
	baseURL := p.cfg.ZAI.BaseURL
	if baseURL == "" {
		baseURL = "https://api.z.ai/api/paas/v4"
	}

	reqBody := zaiRequest{Model: p.model}
	for _, m := range messages {
		reqBody.Messages = append(reqBody.Messages, struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}{Role: m.Role, Content: m.Content})
	}

	data, _ := json.Marshal(reqBody)
	req, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/chat/completions", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.cfg.ZAI.APIKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("zai request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("zai returned status %d: %s", resp.StatusCode, string(body))
	}

	var zResp zaiResponse
	if err := json.Unmarshal(body, &zResp); err != nil {
		return nil, fmt.Errorf("failed to parse zai response: %w", err)
	}

	if len(zResp.Choices) == 0 {
		return nil, fmt.Errorf("no choices returned")
	}

	usage := zResp.Usage
	return &Response{
		Content: zResp.Choices[0].Message.Content,
		Usage: Usage{
			PromptTokens:     usage.PromptTokens,
			CompletionTokens: usage.CompletionTokens,
			TotalTokens:      usage.TotalTokens,
		},
	}, nil
}

func (p *zaiProvider) ChatJSON(ctx context.Context, messages []Message, schema any) (*Response, error) {
	resp, err := p.Chat(ctx, messages)
	if err != nil {
		return nil, err
	}
	resp.Content = CleanJSON(resp.Content)
	return resp, nil
}

func (p *zaiProvider) Name() string {
	return "zai"
}

func (p *zaiProvider) SupportsJSON() bool {
	return true
}
