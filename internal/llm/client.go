// Package llm provides a provider-agnostic interface for LLM completions.
//
// Supported providers: Anthropic, OpenAI (and compatible APIs like Groq,
// Together, Fireworks, Mistral, vLLM), Google Gemini, and Ollama.
package llm

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
)

// Role represents a message role in a conversation.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Message is a single turn in a conversation.
type Message struct {
	Role    Role
	Content string
}

// Response wraps an LLM completion result.
type Response struct {
	Content      string
	Model        string
	InputTokens  int
	OutputTokens int
}

// Client is the port for LLM completions. Implementations must be safe for
// concurrent use.
type Client interface {
	Complete(ctx context.Context, messages []Message) (Response, error)
}

// Provider identifies a supported LLM provider.
type Provider string

const (
	ProviderAnthropic    Provider = "anthropic"
	ProviderOpenAI       Provider = "openai"
	ProviderGemini       Provider = "gemini"
	ProviderOllama       Provider = "ollama"
	ProviderOpenAICompat Provider = "openai-compat"
)

// Config holds the parameters needed to construct a Client.
type Config struct {
	Provider Provider
	APIKey   string
	Model    string
	BaseURL  string
}

// DefaultModel returns the default model for a given provider.
func DefaultModel(p Provider) string {
	switch p {
	case ProviderAnthropic:
		return "claude-sonnet-4-20250514"
	case ProviderOpenAI:
		return "gpt-4o-mini"
	case ProviderGemini:
		return "gemini-2.0-flash"
	case ProviderOllama:
		return "llama3.2"
	default:
		return ""
	}
}

// DefaultBaseURL returns the default API base URL for a given provider.
func DefaultBaseURL(p Provider) string {
	switch p {
	case ProviderAnthropic:
		return "https://api.anthropic.com"
	case ProviderOpenAI:
		return "https://api.openai.com"
	case ProviderGemini:
		return "https://generativelanguage.googleapis.com"
	case ProviderOllama:
		return "http://localhost:11434"
	default:
		return ""
	}
}

// ConfigFromEnv reads LLM configuration from environment variables:
//
//	MNEMOS_LLM_PROVIDER  — anthropic, openai, gemini, ollama, openai-compat
//	MNEMOS_LLM_API_KEY   — API key (required for cloud providers)
//	MNEMOS_LLM_MODEL     — model override (optional, defaults per provider)
//	MNEMOS_LLM_BASE_URL  — endpoint override (required for openai-compat)
func ConfigFromEnv() (Config, error) {
	raw := strings.TrimSpace(os.Getenv("MNEMOS_LLM_PROVIDER"))
	if raw == "" {
		return Config{}, errors.New("MNEMOS_LLM_PROVIDER is not set")
	}

	p := Provider(strings.ToLower(raw))
	switch p {
	case ProviderAnthropic, ProviderOpenAI, ProviderGemini, ProviderOllama, ProviderOpenAICompat:
	default:
		return Config{}, fmt.Errorf("unsupported LLM provider %q (want anthropic, openai, gemini, ollama, or openai-compat)", raw)
	}

	cfg := Config{
		Provider: p,
		APIKey:   strings.TrimSpace(os.Getenv("MNEMOS_LLM_API_KEY")),
		Model:    strings.TrimSpace(os.Getenv("MNEMOS_LLM_MODEL")),
		BaseURL:  strings.TrimSpace(os.Getenv("MNEMOS_LLM_BASE_URL")),
	}

	// Apply defaults.
	if cfg.Model == "" {
		cfg.Model = DefaultModel(p)
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultBaseURL(p)
	}

	// Validate.
	switch p {
	case ProviderAnthropic, ProviderOpenAI, ProviderGemini:
		if cfg.APIKey == "" {
			return Config{}, fmt.Errorf("MNEMOS_LLM_API_KEY is required for provider %q", p)
		}
	case ProviderOpenAICompat:
		if cfg.BaseURL == "" {
			return Config{}, errors.New("MNEMOS_LLM_BASE_URL is required for openai-compat provider")
		}
		if cfg.Model == "" {
			return Config{}, errors.New("MNEMOS_LLM_MODEL is required for openai-compat provider")
		}
	}

	return cfg, nil
}

// NewClient constructs a Client from the given Config.
func NewClient(cfg Config) (Client, error) {
	switch cfg.Provider {
	case ProviderAnthropic:
		return NewAnthropicClient(cfg.BaseURL, cfg.APIKey, cfg.Model), nil
	case ProviderOpenAI, ProviderOllama, ProviderOpenAICompat:
		return NewOpenAIClient(cfg.BaseURL, cfg.APIKey, cfg.Model), nil
	case ProviderGemini:
		return NewGeminiClient(cfg.BaseURL, cfg.APIKey, cfg.Model), nil
	default:
		return nil, fmt.Errorf("unsupported provider %q", cfg.Provider)
	}
}
