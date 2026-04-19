package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Client is a typed Go client for the Mnemos registry HTTP API. It is
// safe for concurrent use by multiple goroutines.
type Client struct {
	baseURL string
	token   string
	httpc   *http.Client
}

// Option configures a Client at construction time.
type Option func(*Client)

// WithToken sets the bearer token sent on write requests. Reads are open
// regardless. If the server has MNEMOS_REGISTRY_TOKEN set, this must
// match.
func WithToken(t string) Option {
	return func(c *Client) { c.token = t }
}

// WithHTTPClient swaps in a caller-provided *http.Client. Useful for
// tests, custom transports, or shared connection pools.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.httpc = h }
}

// WithTimeout sets the request timeout on the default HTTP client. Has
// no effect if WithHTTPClient was also passed — provide your own client
// with the timeout you want in that case.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) {
		if c.httpc == nil {
			c.httpc = &http.Client{Timeout: d}
			return
		}
		c.httpc.Timeout = d
	}
}

// New returns a Client pointing at the given Mnemos registry base URL
// (e.g. "http://localhost:7777"). The trailing slash, if any, is
// stripped. Default timeout is 30s; pass WithTimeout to change it.
func New(baseURL string, opts ...Option) *Client {
	c := &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpc:   &http.Client{Timeout: 30 * time.Second},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Health hits GET /health.
func (c *Client) Health(ctx context.Context) (*HealthResponse, error) {
	var out HealthResponse
	if err := c.do(ctx, http.MethodGet, "/health", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Metrics hits GET /v1/metrics.
func (c *Client) Metrics(ctx context.Context) (*MetricsResponse, error) {
	var out MetricsResponse
	if err := c.do(ctx, http.MethodGet, "/v1/metrics", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListEvents hits GET /v1/events. Pass an empty ListOptions for defaults.
func (c *Client) ListEvents(ctx context.Context, opts ListOptions) (*ListEventsResponse, error) {
	var out ListEventsResponse
	if err := c.do(ctx, http.MethodGet, "/v1/events"+queryString(opts, false, false), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// AppendEvents hits POST /v1/events. Returns the server's accepted count.
// Idempotent: events with IDs already in the registry are no-ops.
func (c *Client) AppendEvents(ctx context.Context, events []Event) (*AppendResponse, error) {
	var out AppendResponse
	body := struct {
		Events []Event `json:"events"`
	}{Events: events}
	if err := c.do(ctx, http.MethodPost, "/v1/events", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListClaims hits GET /v1/claims. ListOptions supports both Type
// (fact|hypothesis|decision) and Status filters; either may be empty.
func (c *Client) ListClaims(ctx context.Context, opts ListOptions) (*ListClaimsResponse, error) {
	var out ListClaimsResponse
	if err := c.do(ctx, http.MethodGet, "/v1/claims"+queryString(opts, true, true), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// AppendClaims hits POST /v1/claims. Pass evidence links to be inserted
// in the same request — they're FK-validated server-side, so the
// referenced events must already exist.
func (c *Client) AppendClaims(ctx context.Context, claims []Claim, evidence []EvidenceLink) (*AppendResponse, error) {
	var out AppendResponse
	body := AppendClaimsBody{Claims: claims, Evidence: evidence}
	if err := c.do(ctx, http.MethodPost, "/v1/claims", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListRelationships hits GET /v1/relationships. ListOptions.Type filters
// by supports|contradicts.
func (c *Client) ListRelationships(ctx context.Context, opts ListOptions) (*ListRelationshipsResponse, error) {
	var out ListRelationshipsResponse
	if err := c.do(ctx, http.MethodGet, "/v1/relationships"+queryString(opts, true, false), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// AppendRelationships hits POST /v1/relationships. The referenced
// from/to claims must already exist.
func (c *Client) AppendRelationships(ctx context.Context, rels []Relationship) (*AppendResponse, error) {
	var out AppendResponse
	body := struct {
		Relationships []Relationship `json:"relationships"`
	}{Relationships: rels}
	if err := c.do(ctx, http.MethodPost, "/v1/relationships", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListEmbeddings hits GET /v1/embeddings. ListOptions.Type filters by
// entity_type (event|claim).
func (c *Client) ListEmbeddings(ctx context.Context, opts ListOptions) (*ListEmbeddingsResponse, error) {
	// Embeddings use the param name entity_type, not type.
	q := url.Values{}
	if opts.Limit > 0 {
		q.Set("limit", strconv.Itoa(opts.Limit))
	}
	if opts.Offset > 0 {
		q.Set("offset", strconv.Itoa(opts.Offset))
	}
	if opts.Type != "" {
		q.Set("entity_type", opts.Type)
	}
	path := "/v1/embeddings"
	if encoded := q.Encode(); encoded != "" {
		path += "?" + encoded
	}
	var out ListEmbeddingsResponse
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// AppendEmbeddings hits POST /v1/embeddings. Vectors travel as JSON
// float arrays and round-trip bit-exact through the server's
// encode/decode cycle.
func (c *Client) AppendEmbeddings(ctx context.Context, embs []Embedding) (*AppendResponse, error) {
	var out AppendResponse
	body := struct {
		Embeddings []Embedding `json:"embeddings"`
	}{Embeddings: embs}
	if err := c.do(ctx, http.MethodPost, "/v1/embeddings", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// queryString builds the ?limit&offset&type&status portion. The two
// bool parameters control which of those filters are applicable to the
// endpoint being called.
func queryString(opts ListOptions, includeType, includeStatus bool) string {
	q := url.Values{}
	if opts.Limit > 0 {
		q.Set("limit", strconv.Itoa(opts.Limit))
	}
	if opts.Offset > 0 {
		q.Set("offset", strconv.Itoa(opts.Offset))
	}
	if includeType && opts.Type != "" {
		q.Set("type", opts.Type)
	}
	if includeStatus && opts.Status != "" {
		q.Set("status", opts.Status)
	}
	encoded := q.Encode()
	if encoded == "" {
		return ""
	}
	return "?" + encoded
}

// do is the single point that sends a request and decodes a response.
// All public methods funnel through here so logging, auth, and error
// handling are consistent.
func (c *Client) do(ctx context.Context, method, path string, body, dst any) error {
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
		reader = bytes.NewReader(buf)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	if reader != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpc.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode/100 != 2 {
		// Try to extract the server's error message; fall back to the
		// raw body if it isn't JSON-shaped.
		var errBody struct {
			Error string `json:"error"`
		}
		msg := strings.TrimSpace(string(respBody))
		if json.Unmarshal(respBody, &errBody) == nil && errBody.Error != "" {
			msg = errBody.Error
		}
		return &APIError{Status: resp.StatusCode, Message: fmt.Sprintf("%s %s: %d %s", method, path, resp.StatusCode, msg)}
	}

	if dst == nil {
		return nil
	}
	if err := json.Unmarshal(respBody, dst); err != nil {
		return fmt.Errorf("decode %s response: %w", path, err)
	}
	return nil
}
