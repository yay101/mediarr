package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type Message struct {
	Role    string
	Content string
}

type Response struct {
	Content string
	Usage
}

type Usage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

type Provider interface {
	Chat(ctx context.Context, messages []Message) (*Response, error)
	ChatJSON(ctx context.Context, messages []Message, schema any) (*Response, error)
	Name() string
	SupportsJSON() bool
}

var ErrUnsupported = &unsupportedError{}

type unsupportedError struct{}

func (e *unsupportedError) Error() string {
	return "provider does not support structured output"
}

func BuildJSONPrompt(schema any) string {
	schemaJSON, _ := json.MarshalIndent(schema, "", "  ")
	return fmt.Sprintf(`You are a JSON response generator. Respond ONLY with valid JSON matching this schema. No additional text.
Schema:
%s

Rules:
- Output must be valid JSON
- Include all required fields
- Use proper types (string, number, boolean, array, object)
- No explanations or commentary`, string(schemaJSON))
}

func CleanJSON(s string) string {
	s = strings.TrimSpace(s)

	start := strings.Index(s, "{")
	if start == -1 {
		start = strings.Index(s, "[")
	}
	if start == -1 {
		return s
	}

	end := strings.LastIndex(s, "}")
	if end == -1 {
		end = strings.LastIndex(s, "]")
	}
	if end == -1 {
		return s
	}

	return s[start : end+1]
}
