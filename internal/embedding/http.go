package embedding

import (
	"net/http"
	"time"
)

// defaultEmbeddingTimeout bounds a single embedding call. Embeddings
// are typically sub-second even on large batches, but a misbehaving
// provider (or stalled network) previously left the caller hanging
// forever because NewEmbedder constructed &http.Client{} with no
// timeout. 30s is a generous but finite floor; callers with tighter
// budgets should pass a deadline via the ctx, which takes precedence.
const defaultEmbeddingTimeout = 30 * time.Second

// defaultEmbeddingHTTPClient returns an http.Client with the default
// per-request timeout baked in. Use from every provider
// constructor so no embedder ever runs with the zero-value timeout.
func defaultEmbeddingHTTPClient() *http.Client {
	return &http.Client{Timeout: defaultEmbeddingTimeout}
}
