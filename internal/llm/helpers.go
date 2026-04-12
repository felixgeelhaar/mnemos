package llm

import (
	"io"
	"log"
)

// closeBody closes an io.ReadCloser (typically http.Response.Body),
// logging any error rather than silently discarding it.
func closeBody(body io.ReadCloser) {
	if err := body.Close(); err != nil {
		log.Printf("close response body: %v", err)
	}
}
