package llm

import (
	"net/http"
	"sync"
	"time"
)

var (
	ollamaOnce     sync.Once
	ollamaDetected bool
)

// OllamaAvailable returns true if an Ollama instance is reachable at
// the default local endpoint. The result is cached after the first check.
func OllamaAvailable() bool {
	ollamaOnce.Do(func() {
		client := &http.Client{Timeout: 500 * time.Millisecond}
		resp, err := client.Get("http://localhost:11434/api/tags")
		if err != nil {
			return
		}
		_ = resp.Body.Close()
		ollamaDetected = resp.StatusCode == http.StatusOK
	})
	return ollamaDetected
}
