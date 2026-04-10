package ai

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/yay101/mediarr/internal/config"
)

func getEnvOrSkip(t *testing.T, key string) string {
	val := os.Getenv(key)
	if val == "" {
		t.Skipf("Skipping test: %s not set", key)
	}
	return val
}

func newTestConfig() *config.AIConfig {
	return &config.AIConfig{
		Enabled: true,
	}
}

func TestOllamaProvider(t *testing.T) {
	cfg := newTestConfig()
	cfg.Provider = "ollama"
	cfg.Ollama.Host = "http://localhost:11434"
	cfg.Ollama.Model = "llama3"

	provider, err := Create(cfg)
	if err != nil {
		t.Fatalf("Failed to create ollama provider: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := provider.Chat(ctx, []Message{
		{Role: "user", Content: "Say 'hello' in one word"},
	})
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	t.Logf("Ollama response: %s", resp.Content)
	t.Logf("Tokens: prompt=%d, completion=%d, total=%d",
		resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Usage.TotalTokens)
}

func TestDeepSeekProvider(t *testing.T) {
	apiKey := getEnvOrSkip(t, "DEEPSEEK_API_KEY")

	cfg := newTestConfig()
	cfg.Provider = "deepseek"
	cfg.DeepSeek.APIKey = apiKey
	cfg.DeepSeek.Model = "deepseek-chat"

	provider, err := Create(cfg)
	if err != nil {
		t.Fatalf("Failed to create deepseek provider: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	t.Run("Chat", func(t *testing.T) {
		resp, err := provider.Chat(ctx, []Message{
			{Role: "user", Content: "Say 'hello' in one word"},
		})
		if err != nil {
			t.Fatalf("Chat failed: %v", err)
		}

		t.Logf("DeepSeek response: %s", resp.Content)
	})

	t.Run("ChatJSON", func(t *testing.T) {
		schema := map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"greeting": map[string]string{"type": "string"},
				"language": map[string]string{"type": "string"},
			},
			"required": []string{"greeting", "language"},
		}

		resp, err := provider.ChatJSON(ctx, []Message{
			{Role: "user", Content: "Give me a greeting in a random language"},
		}, schema)
		if err != nil {
			t.Fatalf("ChatJSON failed: %v", err)
		}

		t.Logf("DeepSeek JSON response: %s", resp.Content)
	})
}

func TestGeminiProvider(t *testing.T) {
	apiKey := getEnvOrSkip(t, "GEMINI_API_KEY")

	cfg := newTestConfig()
	cfg.Provider = "gemini"
	cfg.Gemini.APIKey = apiKey
	cfg.Gemini.Model = "gemini-2.0-flash"

	provider, err := Create(cfg)
	if err != nil {
		t.Fatalf("Failed to create gemini provider: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	t.Run("Chat", func(t *testing.T) {
		resp, err := provider.Chat(ctx, []Message{
			{Role: "user", Content: "Say 'hello' in one word"},
		})
		if err != nil {
			t.Fatalf("Chat failed: %v", err)
		}

		t.Logf("Gemini response: %s", resp.Content)
	})
}

func TestOpenAIProvider(t *testing.T) {
	apiKey := getEnvOrSkip(t, "OPENAI_API_KEY")

	cfg := newTestConfig()
	cfg.Provider = "openai"
	cfg.OpenAI.APIKey = apiKey
	cfg.OpenAI.Model = "gpt-4o-mini"

	provider, err := Create(cfg)
	if err != nil {
		t.Fatalf("Failed to create openai provider: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	t.Run("Chat", func(t *testing.T) {
		resp, err := provider.Chat(ctx, []Message{
			{Role: "user", Content: "Say 'hello' in one word"},
		})
		if err != nil {
			t.Fatalf("Chat failed: %v", err)
		}

		t.Logf("OpenAI response: %s", resp.Content)
	})
}

func TestAnthropicProvider(t *testing.T) {
	apiKey := getEnvOrSkip(t, "ANTHROPIC_API_KEY")

	cfg := newTestConfig()
	cfg.Provider = "anthropic"
	cfg.Anthropic.APIKey = apiKey
	cfg.Anthropic.Model = "claude-sonnet-4-20250514"

	provider, err := Create(cfg)
	if err != nil {
		t.Fatalf("Failed to create anthropic provider: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	t.Run("Chat", func(t *testing.T) {
		resp, err := provider.Chat(ctx, []Message{
			{Role: "user", Content: "Say 'hello' in one word"},
		})
		if err != nil {
			t.Fatalf("Chat failed: %v", err)
		}

		t.Logf("Anthropic response: %s", resp.Content)
	})
}

func TestZAIProvider(t *testing.T) {
	apiKey := getEnvOrSkip(t, "ZAI_API_KEY")

	cfg := newTestConfig()
	cfg.Provider = "zai"
	cfg.ZAI.APIKey = apiKey
	cfg.ZAI.Model = "glm-4"

	provider, err := Create(cfg)
	if err != nil {
		t.Fatalf("Failed to create zai provider: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	t.Run("Chat", func(t *testing.T) {
		resp, err := provider.Chat(ctx, []Message{
			{Role: "user", Content: "Say 'hello' in one word"},
		})
		if err != nil {
			t.Fatalf("Chat failed: %v", err)
		}

		t.Logf("ZAI response: %s", resp.Content)
	})
}

func TestServiceSearchRefine(t *testing.T) {
	apiKey := getEnvOrSkip(t, "DEEPSEEK_API_KEY")

	cfg := &config.AIConfig{
		Enabled:  true,
		Provider: "deepseek",
		DeepSeek: config.DeepSeekProviderConfig{
			APIKey: apiKey,
			Model:  "deepseek-chat",
		},
	}

	svc, err := NewService(cfg)
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	result, err := svc.SearchRefine(ctx, SearchRefineInput{
		Query:     "The.Matrix.1999.1080p",
		MediaType: "movie",
		Results: []SearchResult{
			{Title: "The.Matrix.1999.1080p.BluRay.x264-SPARKS", Size: 10737418240, Seeders: 150, Quality: "1080p"},
			{Title: "The.Matrix.1999.720p.WEB-DL.x264", Size: 2147483648, Seeders: 300, Quality: "720p"},
			{Title: "The.Matrix.1999.2160p.UHD.BluRay.x265", Size: 21474836480, Seeders: 50, Quality: "4k"},
		},
	})
	if err != nil {
		t.Fatalf("SearchRefine failed: %v", err)
	}

	t.Logf("Selected: %d, Reason: %s, Confidence: %.2f", result.Selected, result.Reason, result.Confidence)
}

func TestServiceNaturalSearch(t *testing.T) {
	apiKey := getEnvOrSkip(t, "GEMINI_API_KEY")

	cfg := &config.AIConfig{
		Enabled:  true,
		Provider: "gemini",
		Gemini: config.GeminiProviderConfig{
			APIKey: apiKey,
			Model:  "gemini-2.0-flash",
		},
	}

	svc, err := NewService(cfg)
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	result, err := svc.NaturalSearch(ctx, NaturalSearchInput{
		Query: "I want to watch the latest marvel movie in 4k",
	})
	if err != nil {
		t.Fatalf("NaturalSearch failed: %v", err)
	}

	t.Logf("SearchQuery: %s, MediaType: %s, Quality: %s", result.SearchQuery, result.MediaType, result.Quality)
	if result.DidYouMean != "" {
		t.Logf("DidYouMean: %s", result.DidYouMean)
	}
}
