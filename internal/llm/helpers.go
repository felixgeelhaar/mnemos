package llm

import (
	"io"
	"log"
	"net/http"
	"time"
)

// closeBody closes an io.ReadCloser (typically http.Response.Body),
// logging any error rather than silently discarding it.
func closeBody(body io.ReadCloser) {
	if err := body.Close(); err != nil {
		log.Printf("close response body: %v", err)
	}
}

// defaultLLMTimeout bounds a single provider call. Long completions
// can be minute-scale (Claude/Opus with large context), so 120s is
// generous for legitimate cases but short enough to prevent a hung
// remote from stalling the pipeline forever. Callers that need a
// different budget should set their own timeout on the parent ctx
// passed to Complete — that takes precedence.
const defaultLLMTimeout = 120 * time.Second

// defaultLLMHTTPClient returns an http.Client with a sensible
// per-request timeout. Prior to this every client was constructed
// with &http.Client{} (no timeout) — a hung provider would block
// the caller indefinitely with no observable progress. The ctx
// passed through Complete still bounds the logical operation; this
// timeout is the transport-level floor.
func defaultLLMHTTPClient() *http.Client {
	return &http.Client{Timeout: defaultLLMTimeout}
}
