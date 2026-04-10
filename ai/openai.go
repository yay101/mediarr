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
	Register("openai", newOpenAIProvider)
}

type openAIProvider struct {
	cfg     *config.AIConfig
	model   string
	baseURL string
}

type openAIRequest struct {
	Model    string `json:"model"`
	Messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
}

type openAIResponse struct {
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

func newOpenAIProvider(cfg *config.AIConfig) (Provider, error) {
	p := &openAIProvider{
		cfg:     cfg,
		model:   cfg.OpenAI.Model,
		baseURL: cfg.OpenAI.BaseURL,
	}
	if p.model == "" {
		p.model = "gpt-4o"
	}
	if p.baseURL == "" {
		p.baseURL = "https://api.openai.com/v1"
	}
	return p, nil
}

func (p *openAIProvider) Chat(ctx context.Context, messages []Message) (*Response, error) {
	reqBody := openAIRequest{Model: p.model}
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
	req.Header.Set("Authorization", "Bearer "+p.cfg.OpenAI.APIKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openai returned status %d: %s", resp.StatusCode, string(body))
	}

	var aiResp openAIResponse
	if err := json.Unmarshal(body, &aiResp); err != nil {
		return nil, fmt.Errorf("failed to parse openai response: %w", err)
	}

	if len(aiResp.Choices) == 0 {
		return nil, fmt.Errorf("no choices returned")
	}

	usage := aiResp.Usage
	return &Response{
		Content: aiResp.Choices[0].Message.Content,
		Usage: Usage{
			PromptTokens:     usage.PromptTokens,
			CompletionTokens: usage.CompletionTokens,
			TotalTokens:      usage.TotalTokens,
		},
	}, nil
}

func (p *openAIProvider) ChatJSON(ctx context.Context, messages []Message, schema any) (*Response, error) {
	resp, err := p.Chat(ctx, messages)
	if err != nil {
		return nil, err
	}
	resp.Content = CleanJSON(resp.Content)
	return resp, nil
}

func (p *openAIProvider) Name() string {
	return "openai"
}

func (p *openAIProvider) SupportsJSON() bool {
	return true
}
