package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/yay101/mediarr/config"
)

func init() {
	Register("ollama", newOllamaProvider)
}

type ollamaProvider struct {
	cfg   *config.AIConfig
	model string
	url   string
}

type ollamaRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
}

type ollamaResponse struct {
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
	Done  bool `json:"done"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

func newOllamaProvider(cfg *config.AIConfig) (Provider, error) {
	p := &ollamaProvider{
		cfg:   cfg,
		model: cfg.Ollama.Model,
		url:   cfg.Ollama.Host + "/api/chat",
	}
	if p.model == "" {
		p.model = "llama3"
	}
	return p, nil
}

func (p *ollamaProvider) Chat(ctx context.Context, messages []Message) (*Response, error) {
	reqBody := ollamaRequest{
		Model:    p.model,
		Messages: messages,
		Stream:   false,
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.url, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama returned status %d", resp.StatusCode)
	}

	var ollamaResp ollamaResponse
	if err := json.NewDecoder(resp.Body).Decode(&ollamaResp); err != nil {
		return nil, err
	}

	return &Response{
		Content: ollamaResp.Message.Content,
		Usage: Usage{
			PromptTokens:     ollamaResp.Usage.PromptTokens,
			CompletionTokens: ollamaResp.Usage.CompletionTokens,
			TotalTokens:      ollamaResp.Usage.TotalTokens,
		},
	}, nil
}

func (p *ollamaProvider) ChatJSON(ctx context.Context, messages []Message, schema any) (*Response, error) {
	enhancedMessages := make([]Message, len(messages)+1)
	enhancedMessages[0] = Message{Role: "system", Content: BuildJSONPrompt(schema)}
	copy(enhancedMessages[1:], messages)

	resp, err := p.Chat(ctx, enhancedMessages)
	if err != nil {
		return nil, err
	}

	resp.Content = CleanJSON(resp.Content)
	return resp, nil
}

func (p *ollamaProvider) Name() string {
	return "ollama"
}

func (p *ollamaProvider) SupportsJSON() bool {
	return false
}
