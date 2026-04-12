package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAnthropicClientComplete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "test-key" {
			t.Fatalf("missing api key header")
		}
		if r.Header.Get("anthropic-version") != "2023-06-01" {
			t.Fatalf("missing anthropic-version header")
		}

		resp := anthropicResponse{
			Content: []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}{{Type: "text", Text: "hello from claude"}},
			Model: "claude-sonnet-4-20250514",
		}
		resp.Usage.InputTokens = 10
		resp.Usage.OutputTokens = 5

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	}))
	defer server.Close()

	client := NewAnthropicClient(server.URL, "test-key", "claude-sonnet-4-20250514")
	resp, err := client.Complete(context.Background(), []Message{
		{Role: RoleSystem, Content: "you are helpful"},
		{Role: RoleUser, Content: "hi"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "hello from claude" {
		t.Fatalf("got content %q, want %q", resp.Content, "hello from claude")
	}
	if resp.InputTokens != 10 || resp.OutputTokens != 5 {
		t.Fatalf("token counts wrong: input=%d output=%d", resp.InputTokens, resp.OutputTokens)
	}
}

func TestOpenAIClientComplete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("missing auth header")
		}

		resp := openAIResponse{
			Choices: []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			}{{Message: struct {
				Content string `json:"content"`
			}{Content: "hello from openai"}}},
			Model: "gpt-4o-mini",
		}
		resp.Usage.PromptTokens = 8
		resp.Usage.CompletionTokens = 4

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	}))
	defer server.Close()

	client := NewOpenAIClient(server.URL, "test-key", "gpt-4o-mini")
	resp, err := client.Complete(context.Background(), []Message{
		{Role: RoleSystem, Content: "you are helpful"},
		{Role: RoleUser, Content: "hi"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "hello from openai" {
		t.Fatalf("got content %q, want %q", resp.Content, "hello from openai")
	}
}

func TestGeminiClientComplete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		// Path should include model name and key.
		if r.URL.Query().Get("key") != "test-key" {
			t.Fatalf("missing api key in query")
		}

		resp := geminiResponse{
			Candidates: []struct {
				Content struct {
					Parts []struct {
						Text string `json:"text"`
					} `json:"parts"`
				} `json:"content"`
			}{{Content: struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			}{Parts: []struct {
				Text string `json:"text"`
			}{{Text: "hello from gemini"}}}}},
		}
		resp.UsageMetadata.PromptTokenCount = 6
		resp.UsageMetadata.CandidatesTokenCount = 3

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	}))
	defer server.Close()

	client := NewGeminiClient(server.URL, "test-key", "gemini-2.0-flash")
	resp, err := client.Complete(context.Background(), []Message{
		{Role: RoleSystem, Content: "you are helpful"},
		{Role: RoleUser, Content: "hi"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "hello from gemini" {
		t.Fatalf("got content %q, want %q", resp.Content, "hello from gemini")
	}
}

func TestAnthropicClientErrorStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":{"type":"authentication_error","message":"invalid api key"}}`)) //nolint:errcheck
	}))
	defer server.Close()

	client := NewAnthropicClient(server.URL, "bad-key", "claude-sonnet-4-20250514")
	_, err := client.Complete(context.Background(), []Message{{Role: RoleUser, Content: "hi"}})
	if err == nil {
		t.Fatal("expected error for 401 status")
	}
}

func TestConfigFromEnv(t *testing.T) {
	t.Run("missing provider", func(t *testing.T) {
		t.Setenv("MNEMOS_LLM_PROVIDER", "")
		_, err := ConfigFromEnv()
		if err == nil {
			t.Fatal("expected error when provider is not set")
		}
	})

	t.Run("anthropic requires key", func(t *testing.T) {
		t.Setenv("MNEMOS_LLM_PROVIDER", "anthropic")
		t.Setenv("MNEMOS_LLM_API_KEY", "")
		_, err := ConfigFromEnv()
		if err == nil {
			t.Fatal("expected error when api key is missing for anthropic")
		}
	})

	t.Run("valid anthropic config", func(t *testing.T) {
		t.Setenv("MNEMOS_LLM_PROVIDER", "anthropic")
		t.Setenv("MNEMOS_LLM_API_KEY", "sk-test")
		t.Setenv("MNEMOS_LLM_MODEL", "")
		t.Setenv("MNEMOS_LLM_BASE_URL", "")
		cfg, err := ConfigFromEnv()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Provider != ProviderAnthropic {
			t.Fatalf("got provider %q", cfg.Provider)
		}
		if cfg.Model != "claude-sonnet-4-20250514" {
			t.Fatalf("got model %q", cfg.Model)
		}
	})

	t.Run("ollama no key needed", func(t *testing.T) {
		t.Setenv("MNEMOS_LLM_PROVIDER", "ollama")
		t.Setenv("MNEMOS_LLM_API_KEY", "")
		t.Setenv("MNEMOS_LLM_MODEL", "")
		t.Setenv("MNEMOS_LLM_BASE_URL", "")
		cfg, err := ConfigFromEnv()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.BaseURL != "http://localhost:11434" {
			t.Fatalf("got base url %q", cfg.BaseURL)
		}
	})
}
