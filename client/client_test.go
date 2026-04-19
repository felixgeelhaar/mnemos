package client_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/felixgeelhaar/mnemos/client"
)

// fakeRegistry stands in for `mnemos serve` end-to-end. We don't import
// the cmd/mnemos handlers directly because they're in package main; this
// keeps the client package's tests self-contained.
type fakeRegistry struct {
	events []client.Event
	claims []client.Claim
	rels   []client.Relationship
	embs   []client.Embedding
	token  string // if non-empty, write methods require Bearer matching this
}

func (f *fakeRegistry) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, client.HealthResponse{Status: "ok", Version: "test"})
	})
	mux.HandleFunc("/v1/metrics", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, client.MetricsResponse{Events: int64(len(f.events)), Claims: int64(len(f.claims))})
	})
	mux.HandleFunc("/v1/events", f.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, http.StatusOK, client.ListEventsResponse{Events: f.events, Total: len(f.events), Limit: 50, Offset: 0})
		case http.MethodPost:
			var body struct {
				Events []client.Event `json:"events"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, `{"error":"bad body"}`, http.StatusBadRequest)
				return
			}
			f.events = append(f.events, body.Events...)
			writeJSON(w, http.StatusCreated, client.AppendResponse{Accepted: len(body.Events)})
		default:
			http.Error(w, `{"error":"bad method"}`, http.StatusMethodNotAllowed)
		}
	}))
	mux.HandleFunc("/v1/claims", f.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, http.StatusOK, client.ListClaimsResponse{Claims: f.claims, Total: len(f.claims), Limit: 50, Offset: 0})
		case http.MethodPost:
			var body client.AppendClaimsBody
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, `{"error":"bad body"}`, http.StatusBadRequest)
				return
			}
			f.claims = append(f.claims, body.Claims...)
			writeJSON(w, http.StatusCreated, client.AppendResponse{Accepted: len(body.Claims)})
		default:
			http.Error(w, `{"error":"bad method"}`, http.StatusMethodNotAllowed)
		}
	}))
	mux.HandleFunc("/v1/relationships", f.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, http.StatusOK, client.ListRelationshipsResponse{Relationships: f.rels, Total: len(f.rels), Limit: 50, Offset: 0})
		case http.MethodPost:
			var body struct {
				Relationships []client.Relationship `json:"relationships"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, `{"error":"bad body"}`, http.StatusBadRequest)
				return
			}
			f.rels = append(f.rels, body.Relationships...)
			writeJSON(w, http.StatusCreated, client.AppendResponse{Accepted: len(body.Relationships)})
		default:
			http.Error(w, `{"error":"bad method"}`, http.StatusMethodNotAllowed)
		}
	}))
	mux.HandleFunc("/v1/embeddings", f.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, http.StatusOK, client.ListEmbeddingsResponse{Embeddings: f.embs, Total: len(f.embs), Limit: 50, Offset: 0})
		case http.MethodPost:
			var body struct {
				Embeddings []client.Embedding `json:"embeddings"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, `{"error":"bad body"}`, http.StatusBadRequest)
				return
			}
			f.embs = append(f.embs, body.Embeddings...)
			writeJSON(w, http.StatusCreated, client.AppendResponse{Accepted: len(body.Embeddings)})
		default:
			http.Error(w, `{"error":"bad method"}`, http.StatusMethodNotAllowed)
		}
	}))
	return mux
}

func (f *fakeRegistry) requireAuth(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || f.token == "" {
			h(w, r)
			return
		}
		got := r.Header.Get("Authorization")
		if got != "Bearer "+f.token {
			http.Error(w, `{"error":"missing or invalid bearer token"}`, http.StatusUnauthorized)
			return
		}
		h(w, r)
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func TestClient_Health(t *testing.T) {
	srv := httptest.NewServer((&fakeRegistry{}).handler())
	defer srv.Close()

	c := client.New(srv.URL)
	got, err := c.Health(context.Background())
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if got.Status != "ok" || got.Version != "test" {
		t.Errorf("got %+v, want {ok, test}", got)
	}
}

func TestClient_AppendAndListEvents(t *testing.T) {
	reg := &fakeRegistry{}
	srv := httptest.NewServer(reg.handler())
	defer srv.Close()

	c := client.New(srv.URL)
	ctx := context.Background()

	resp, err := c.AppendEvents(ctx, []client.Event{
		{ID: "ev_1", Content: "Test event", Timestamp: client.FormatTime(time.Now())},
	})
	if err != nil {
		t.Fatalf("AppendEvents: %v", err)
	}
	if resp.Accepted != 1 {
		t.Errorf("accepted = %d, want 1", resp.Accepted)
	}

	list, err := c.ListEvents(ctx, client.ListOptions{})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if list.Total != 1 || len(list.Events) != 1 || list.Events[0].ID != "ev_1" {
		t.Errorf("got %+v, want one event with ID ev_1", list)
	}
}

func TestClient_AppendClaimsWithEvidence(t *testing.T) {
	reg := &fakeRegistry{}
	srv := httptest.NewServer(reg.handler())
	defer srv.Close()

	c := client.New(srv.URL)
	resp, err := c.AppendClaims(context.Background(),
		[]client.Claim{{ID: "cl_1", Text: "test", Type: "fact", Status: "active", Confidence: 0.8}},
		[]client.EvidenceLink{{ClaimID: "cl_1", EventID: "ev_1"}},
	)
	if err != nil {
		t.Fatalf("AppendClaims: %v", err)
	}
	if resp.Accepted != 1 {
		t.Errorf("accepted = %d, want 1", resp.Accepted)
	}
}

func TestClient_AppendRelationshipsAndEmbeddings(t *testing.T) {
	reg := &fakeRegistry{}
	srv := httptest.NewServer(reg.handler())
	defer srv.Close()

	c := client.New(srv.URL)
	ctx := context.Background()

	if _, err := c.AppendRelationships(ctx, []client.Relationship{
		{ID: "r1", Type: "supports", FromClaimID: "cl_a", ToClaimID: "cl_b"},
	}); err != nil {
		t.Fatalf("AppendRelationships: %v", err)
	}

	if _, err := c.AppendEmbeddings(ctx, []client.Embedding{
		{EntityID: "ev_1", EntityType: "event", Vector: []float32{0.1, 0.2}, Model: "test"},
	}); err != nil {
		t.Fatalf("AppendEmbeddings: %v", err)
	}

	rels, _ := c.ListRelationships(ctx, client.ListOptions{Type: "supports"})
	if rels.Total != 1 {
		t.Errorf("relationships listed = %d, want 1", rels.Total)
	}
	embs, _ := c.ListEmbeddings(ctx, client.ListOptions{Type: "event"})
	if embs.Total != 1 || embs.Embeddings[0].Vector[0] != 0.1 {
		t.Errorf("embeddings listed = %+v, want vector preserved", embs)
	}
}

func TestClient_AuthHeaderPropagated(t *testing.T) {
	reg := &fakeRegistry{token: "shh-secret"}
	srv := httptest.NewServer(reg.handler())
	defer srv.Close()

	// Without a token: write should 401.
	noauth := client.New(srv.URL)
	_, err := noauth.AppendEvents(context.Background(), []client.Event{{ID: "ev_x"}})
	if err == nil {
		t.Fatal("expected 401 without token")
	}
	var apiErr *client.APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusUnauthorized {
		t.Fatalf("got %v, want APIError with 401", err)
	}

	// With the right token: write succeeds.
	authed := client.New(srv.URL, client.WithToken("shh-secret"))
	resp, err := authed.AppendEvents(context.Background(), []client.Event{{ID: "ev_x"}})
	if err != nil || resp.Accepted != 1 {
		t.Fatalf("authed write failed: resp=%+v err=%v", resp, err)
	}
}

func TestClient_APIErrorIsTyped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"oh no"}`))
	}))
	defer srv.Close()

	_, err := client.New(srv.URL).Health(context.Background())
	var apiErr *client.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("got %v, want *APIError", err)
	}
	if apiErr.Status != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", apiErr.Status)
	}
	if !strings.Contains(apiErr.Error(), "oh no") {
		t.Errorf("error message lost the server detail: %v", apiErr)
	}
}

func TestClient_TrailingSlashTrimmed(t *testing.T) {
	srv := httptest.NewServer((&fakeRegistry{}).handler())
	defer srv.Close()

	// Constructor must accept trailing slash without producing //paths.
	c := client.New(srv.URL + "/")
	if _, err := c.Health(context.Background()); err != nil {
		t.Fatalf("Health with trailing slash: %v", err)
	}
}

func TestParseTimeAndFormatTime_RoundTrip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	formatted := client.FormatTime(now)
	parsed, err := client.ParseTime(formatted)
	if err != nil {
		t.Fatalf("ParseTime: %v", err)
	}
	if !parsed.Equal(now) {
		t.Errorf("round-trip mismatch: got %v, want %v", parsed, now)
	}
}

// Example_basic is a runnable example that compiles into the package
// docs. Shows the most common shape: create a client, write some claims,
// read them back.
func Example_basic() {
	ctx := context.Background()
	c := client.New("http://localhost:7777", client.WithToken("optional-token"))

	// Write
	if _, err := c.AppendEvents(ctx, []client.Event{
		{
			ID:            "ev_demo",
			RunID:         "demo-run",
			SchemaVersion: "v1",
			Content:       "We chose Postgres for the new service",
			SourceInputID: "in_demo",
			Timestamp:     client.FormatTime(time.Now()),
		},
	}); err != nil {
		panic(err)
	}

	// Read
	list, err := c.ListClaims(ctx, client.ListOptions{Type: "decision", Limit: 10})
	if err != nil {
		panic(err)
	}
	for _, claim := range list.Claims {
		fmt.Printf("[%s] %s\n", claim.Type, claim.Text)
	}
}
