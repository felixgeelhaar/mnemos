package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// OpenAIClient calls any OpenAI-compatible Chat Completions API.
// Works with OpenAI, Azure OpenAI, Ollama, Groq, Together, Fireworks,
// Mistral, vLLM, LM Studio, and any other compatible endpoint.
type OpenAIClient struct {
	baseURL string
	apiKey  string
	model   string
	http    *http.Client
}

// NewOpenAIClient creates a client for any OpenAI-compatible API.
func NewOpenAIClient(baseURL, apiKey, model string) *OpenAIClient {
	return &OpenAIClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		model:   model,
		http:    defaultLLMHTTPClient(),
	}
}

type openAIRequest struct {
	Model          string             `json:"model"`
	Messages       []openAIMessage    `json:"messages"`
	ResponseFormat *openAIResponseFmt `json:"response_format,omitempty"`
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// openAIResponseFmt enables JSON mode when the system prompt requests JSON.
type openAIResponseFmt struct {
	Type string `json:"type"`
}

type openAIResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Model string `json:"model"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// Complete sends a completion request to an OpenAI-compatible API.
func (c *OpenAIClient) Complete(ctx context.Context, messages []Message) (Response, error) {
	apiMsgs := make([]openAIMessage, 0, len(messages))
	for _, m := range messages {
		apiMsgs = append(apiMsgs, openAIMessage{
			Role:    string(m.Role),
			Content: m.Content,
		})
	}

	reqBody := openAIRequest{
		Model:    c.model,
		Messages: apiMsgs,
	}

	// Enable JSON mode when the system prompt asks for JSON output.
	for _, m := range messages {
		if m.Role == RoleSystem && strings.Contains(m.Content, "JSON") {
			reqBody.ResponseFormat = &openAIResponseFmt{Type: "json_object"}
			break
		}
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return Response{}, fmt.Errorf("marshal openai request: %w", err)
	}

	// Ollama uses /api/chat, standard OpenAI uses /v1/chat/completions.
	endpoint := c.baseURL + "/v1/chat/completions"
	if strings.Contains(c.baseURL, ":11434") {
		endpoint = c.baseURL + "/api/chat"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return Response{}, fmt.Errorf("create openai request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return Response{}, fmt.Errorf("openai request failed: %w", err)
	}
	defer closeBody(resp.Body)

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return Response{}, fmt.Errorf("read openai response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return Response{}, fmt.Errorf("openai-compatible API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var result openAIResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return Response{}, fmt.Errorf("unmarshal openai response: %w", err)
	}

	if result.Error != nil {
		return Response{}, fmt.Errorf("openai error: %s: %s", result.Error.Type, result.Error.Message)
	}

	if len(result.Choices) == 0 {
		return Response{}, fmt.Errorf("openai returned no choices")
	}

	return Response{
		Content:      result.Choices[0].Message.Content,
		Model:        result.Model,
		InputTokens:  result.Usage.PromptTokens,
		OutputTokens: result.Usage.CompletionTokens,
	}, nil
}
