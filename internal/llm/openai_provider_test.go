package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAIClientErrorsTaggedWithProvider(t *testing.T) {
	// Server returns invalid JSON to trigger the unmarshal error path.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("not-valid-json"))
	}))
	defer server.Close()

	cases := []struct {
		provider string
		want     string
	}{
		{"ollama", "ollama"},
		{"openai", "openai"},
		{"openai-compat", "openai-compat"},
	}
	for _, tc := range cases {
		t.Run(tc.provider, func(t *testing.T) {
			c := NewOpenAIClient(server.URL, "", "test-model", tc.provider)
			_, err := c.Complete(context.Background(), []Message{{Role: RoleUser, Content: "hi"}})
			if err == nil {
				t.Fatal("expected error from invalid JSON response")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not contain provider %q", err.Error(), tc.want)
			}
		})
	}
}

func TestOpenAIClientToleratesTrailingFrame(t *testing.T) {
	// Some Ollama versions emit a final {"done":true} frame after the
	// main response even when stream=false. The Decoder-based parser
	// should consume the first object and ignore the rest.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"hello"}}],"model":"test"}
{"done":true}`))
	}))
	defer server.Close()

	c := NewOpenAIClient(server.URL, "", "test-model", "ollama")
	resp, err := c.Complete(context.Background(), []Message{{Role: RoleUser, Content: "hi"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "hello" {
		t.Fatalf("got content %q, want %q", resp.Content, "hello")
	}
}

func TestOpenAIClientSendsStreamFalse(t *testing.T) {
	gotStream := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 4096)
		n, _ := r.Body.Read(buf)
		gotStream = string(buf[:n])
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer server.Close()

	c := NewOpenAIClient(server.URL, "", "m", "ollama")
	_, err := c.Complete(context.Background(), []Message{{Role: RoleUser, Content: "hi"}})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if !strings.Contains(gotStream, `"stream":false`) {
		t.Fatalf("request body should include stream:false, got: %s", gotStream)
	}
}
