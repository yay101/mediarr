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
	Register("deepseek", newDeepSeekProvider)
}

type deepSeekProvider struct {
	cfg   *config.AIConfig
	model string
}

type deepSeekRequest struct {
	Model    string `json:"model"`
	Messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
}

type deepSeekResponse struct {
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

func newDeepSeekProvider(cfg *config.AIConfig) (Provider, error) {
	p := &deepSeekProvider{
		cfg:   cfg,
		model: cfg.DeepSeek.Model,
	}
	if p.model == "" {
		p.model = "deepseek-chat"
	}
	return p, nil
}

func (p *deepSeekProvider) Chat(ctx context.Context, messages []Message) (*Response, error) {
	baseURL := p.cfg.DeepSeek.BaseURL
	if baseURL == "" {
		baseURL = "https://api.deepseek.com"
	}

	reqBody := deepSeekRequest{Model: p.model}
	for _, m := range messages {
		reqBody.Messages = append(reqBody.Messages, struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}{Role: m.Role, Content: m.Content})
	}

	data, _ := json.Marshal(reqBody)
	req, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/v1/chat/completions", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.cfg.DeepSeek.APIKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("deepseek request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("deepseek returned status %d: %s", resp.StatusCode, string(body))
	}

	var dsResp deepSeekResponse
	if err := json.Unmarshal(body, &dsResp); err != nil {
		return nil, fmt.Errorf("failed to parse deepseek response: %w", err)
	}

	if len(dsResp.Choices) == 0 {
		return nil, fmt.Errorf("no choices returned")
	}

	usage := dsResp.Usage
	return &Response{
		Content: dsResp.Choices[0].Message.Content,
		Usage: Usage{
			PromptTokens:     usage.PromptTokens,
			CompletionTokens: usage.CompletionTokens,
			TotalTokens:      usage.TotalTokens,
		},
	}, nil
}

func (p *deepSeekProvider) ChatJSON(ctx context.Context, messages []Message, schema any) (*Response, error) {
	baseURL := p.cfg.DeepSeek.BaseURL
	if baseURL == "" {
		baseURL = "https://api.deepseek.com"
	}

	reqBody := deepSeekRequest{Model: p.model}
	reqBody.Messages = append(reqBody.Messages, struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}{Role: "system", Content: BuildJSONPrompt(schema)})
	for _, m := range messages {
		reqBody.Messages = append(reqBody.Messages, struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}{Role: m.Role, Content: m.Content})
	}

	data, _ := json.Marshal(reqBody)
	req, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/v1/chat/completions", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.cfg.DeepSeek.APIKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("deepseek request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var dsResp deepSeekResponse
	if err := json.Unmarshal(body, &dsResp); err != nil {
		return nil, fmt.Errorf("failed to parse deepseek response: %w", err)
	}

	if len(dsResp.Choices) == 0 {
		return nil, fmt.Errorf("no choices returned")
	}

	usage := dsResp.Usage
	return &Response{
		Content: CleanJSON(dsResp.Choices[0].Message.Content),
		Usage: Usage{
			PromptTokens:     usage.PromptTokens,
			CompletionTokens: usage.CompletionTokens,
			TotalTokens:      usage.TotalTokens,
		},
	}, nil
}

func (p *deepSeekProvider) Name() string {
	return "deepseek"
}

func (p *deepSeekProvider) SupportsJSON() bool {
	return true
}
