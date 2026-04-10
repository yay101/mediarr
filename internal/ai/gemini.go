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
	Register("gemini", newGeminiProvider)
}

type geminiProvider struct {
	cfg   *config.AIConfig
	model string
}

type geminiRequest struct {
	Contents []struct {
		Role  string `json:"role"`
		Parts []struct {
			Text string `json:"text"`
		} `json:"parts"`
	} `json:"contents"`
}

type geminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
	UsageMetadata struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
		TotalTokenCount      int `json:"totalTokenCount"`
	} `json:"usageMetadata"`
}

func newGeminiProvider(cfg *config.AIConfig) (Provider, error) {
	p := &geminiProvider{
		cfg:   cfg,
		model: cfg.Gemini.Model,
	}
	if p.model == "" {
		p.model = "gemini-2.0-flash"
	}
	return p, nil
}

func (p *geminiProvider) Chat(ctx context.Context, messages []Message) (*Response, error) {
	reqBody := geminiRequest{}
	for _, m := range messages {
		if m.Role == "system" {
			m.Role = "user"
		}
		reqBody.Contents = append(reqBody.Contents, struct {
			Role  string `json:"role"`
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		}{Role: m.Role, Parts: []struct {
			Text string `json:"text"`
		}{{Text: m.Content}}})
	}

	data, _ := json.Marshal(reqBody)
	req, err := http.NewRequestWithContext(ctx, "POST",
		"https://generativelanguage.googleapis.com/v1beta/models/"+p.model+":generateContent?key="+p.cfg.Gemini.APIKey,
		bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gemini request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gemini returned status %d: %s", resp.StatusCode, string(body))
	}

	var gmResp geminiResponse
	if err := json.Unmarshal(body, &gmResp); err != nil {
		return nil, fmt.Errorf("failed to parse gemini response: %w", err)
	}

	if len(gmResp.Candidates) == 0 || len(gmResp.Candidates[0].Content.Parts) == 0 {
		return nil, fmt.Errorf("no content returned")
	}

	usage := gmResp.UsageMetadata
	return &Response{
		Content: gmResp.Candidates[0].Content.Parts[0].Text,
		Usage: Usage{
			PromptTokens:     usage.PromptTokenCount,
			CompletionTokens: usage.CandidatesTokenCount,
			TotalTokens:      usage.TotalTokenCount,
		},
	}, nil
}

func (p *geminiProvider) ChatJSON(ctx context.Context, messages []Message, schema any) (*Response, error) {
	resp, err := p.Chat(ctx, messages)
	if err != nil {
		return nil, err
	}
	resp.Content = CleanJSON(resp.Content)
	return resp, nil
}

func (p *geminiProvider) Name() string {
	return "gemini"
}

func (p *geminiProvider) SupportsJSON() bool {
	return true
}
