package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func postJSON(t *testing.T, url string, body any, headers map[string]string) *http.Response {
	t.Helper()
	buf, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	return resp
}

func TestServe_AppendEvents_PersistsAndCanBeListed(t *testing.T) {
	db := newServerTestDB(t)
	srv := httptest.NewServer(newServerMux(db))
	defer srv.Close()

	ts := time.Now().UTC().Format(time.RFC3339)
	body := map[string]any{
		"events": []map[string]any{
			{
				"id":              "ev_post_1",
				"run_id":          "r-post",
				"schema_version":  "v1",
				"content":         "We adopted gRPC for service-to-service calls.",
				"source_input_id": "in_post_1",
				"timestamp":       ts,
				"metadata":        map[string]string{"source": "raw_text"},
			},
		},
	}

	resp := postJSON(t, srv.URL+"/v1/events", body, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		var msg errorResponse
		_ = json.NewDecoder(resp.Body).Decode(&msg)
		t.Fatalf("status = %d, want 201 (%v)", resp.StatusCode, msg.Error)
	}
	var got appendResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Accepted != 1 {
		t.Fatalf("accepted = %d, want 1", got.Accepted)
	}

	// Verify by listing.
	getResp, err := http.Get(srv.URL + "/v1/events")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer func() { _ = getResp.Body.Close() }()
	var list eventsResponse
	if err := json.NewDecoder(getResp.Body).Decode(&list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if list.Total != 1 || len(list.Events) != 1 || list.Events[0].ID != "ev_post_1" {
		t.Fatalf("listed events = %+v", list)
	}
}

func TestServe_AppendEvents_RejectsEmptyArray(t *testing.T) {
	srv := httptest.NewServer(newServerMux(newServerTestDB(t)))
	defer srv.Close()

	resp := postJSON(t, srv.URL+"/v1/events", map[string]any{"events": []any{}}, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestServe_AppendEvents_RejectsBadTimestamp(t *testing.T) {
	srv := httptest.NewServer(newServerMux(newServerTestDB(t)))
	defer srv.Close()

	body := map[string]any{
		"events": []map[string]any{{"id": "ev_x", "content": "x", "timestamp": "yesterday"}},
	}
	resp := postJSON(t, srv.URL+"/v1/events", body, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestServe_AppendClaims_PersistsAndCanBeListed(t *testing.T) {
	db := newServerTestDB(t)
	srv := httptest.NewServer(newServerMux(db))
	defer srv.Close()

	now := time.Now().UTC()
	// Need an event for the evidence link FK to resolve.
	seedEvent(t, db, "ev_for_claim", "r1", "context", "in_e", `{}`, now)

	body := map[string]any{
		"claims": []map[string]any{
			{
				"id":         "cl_post_1",
				"text":       "Authentication uses OAuth2.",
				"type":       "fact",
				"confidence": 0.9,
				"status":     "active",
				"created_at": now.Format(time.RFC3339),
			},
		},
		"evidence": []map[string]string{
			{"claim_id": "cl_post_1", "event_id": "ev_for_claim"},
		},
	}
	resp := postJSON(t, srv.URL+"/v1/claims", body, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		var msg errorResponse
		_ = json.NewDecoder(resp.Body).Decode(&msg)
		t.Fatalf("status = %d, want 201 (%v)", resp.StatusCode, msg.Error)
	}

	// Verify the evidence link landed.
	var n int
	if err := db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM claim_evidence WHERE claim_id = ? AND event_id = ?`, "cl_post_1", "ev_for_claim").Scan(&n); err != nil {
		t.Fatalf("count evidence: %v", err)
	}
	if n != 1 {
		t.Fatalf("evidence rows = %d, want 1", n)
	}
}

func TestServe_AppendClaims_RejectsInvalidType(t *testing.T) {
	srv := httptest.NewServer(newServerMux(newServerTestDB(t)))
	defer srv.Close()

	body := map[string]any{
		"claims": []map[string]any{
			{"id": "cl_x", "text": "x", "type": "guess", "confidence": 0.5, "status": "active"},
		},
	}
	resp := postJSON(t, srv.URL+"/v1/claims", body, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestServe_AppendRelationships_PersistsCorrectly(t *testing.T) {
	db := newServerTestDB(t)
	srv := httptest.NewServer(newServerMux(db))
	defer srv.Close()

	now := time.Now().UTC()
	seedClaim(t, db, "c-from", "a", "fact", "active", 0.8, now)
	seedClaim(t, db, "c-to", "b", "fact", "active", 0.8, now)

	body := map[string]any{
		"relationships": []map[string]any{
			{
				"id":            "r_post_1",
				"type":          "supports",
				"from_claim_id": "c-from",
				"to_claim_id":   "c-to",
			},
		},
	}
	resp := postJSON(t, srv.URL+"/v1/relationships", body, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		var msg errorResponse
		_ = json.NewDecoder(resp.Body).Decode(&msg)
		t.Fatalf("status = %d, want 201 (%v)", resp.StatusCode, msg.Error)
	}
}

func TestServe_AppendRelationships_RejectsBadType(t *testing.T) {
	srv := httptest.NewServer(newServerMux(newServerTestDB(t)))
	defer srv.Close()

	body := map[string]any{
		"relationships": []map[string]any{
			{"id": "r-x", "type": "neutralizes", "from_claim_id": "c1", "to_claim_id": "c2"},
		},
	}
	resp := postJSON(t, srv.URL+"/v1/relationships", body, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestServe_Auth_NoTokenAllowsWritesByDefault(t *testing.T) {
	t.Setenv("MNEMOS_REGISTRY_TOKEN", "")
	srv := httptest.NewServer(newServerMux(newServerTestDB(t)))
	defer srv.Close()

	resp := postJSON(t, srv.URL+"/v1/events",
		map[string]any{"events": []map[string]any{{"id": "e1", "content": "x"}}}, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (open by default)", resp.StatusCode)
	}
}

func TestServe_Auth_WithTokenRejectsMissingHeader(t *testing.T) {
	t.Setenv("MNEMOS_REGISTRY_TOKEN", "secret-token-1234")
	srv := httptest.NewServer(newServerMux(newServerTestDB(t)))
	defer srv.Close()

	resp := postJSON(t, srv.URL+"/v1/events",
		map[string]any{"events": []map[string]any{{"id": "e1", "content": "x"}}}, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestServe_Auth_WithTokenRejectsWrongValue(t *testing.T) {
	t.Setenv("MNEMOS_REGISTRY_TOKEN", "secret-token-1234")
	srv := httptest.NewServer(newServerMux(newServerTestDB(t)))
	defer srv.Close()

	resp := postJSON(t, srv.URL+"/v1/events",
		map[string]any{"events": []map[string]any{{"id": "e1", "content": "x"}}},
		map[string]string{"Authorization": "Bearer wrong-token"})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestServe_Auth_WithTokenAllowsCorrectValue(t *testing.T) {
	t.Setenv("MNEMOS_REGISTRY_TOKEN", "secret-token-1234")
	srv := httptest.NewServer(newServerMux(newServerTestDB(t)))
	defer srv.Close()

	body := map[string]any{
		"events": []map[string]any{{
			"id": "ev_authed", "run_id": "r", "schema_version": "v1",
			"content": "x", "source_input_id": "in",
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		}},
	}
	resp := postJSON(t, srv.URL+"/v1/events", body,
		map[string]string{"Authorization": "Bearer secret-token-1234"})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		var msg errorResponse
		_ = json.NewDecoder(resp.Body).Decode(&msg)
		t.Fatalf("status = %d, want 201 (%v)", resp.StatusCode, msg.Error)
	}
}

func TestServe_Auth_ReadsAlwaysAllowed(t *testing.T) {
	t.Setenv("MNEMOS_REGISTRY_TOKEN", "secret-token-1234")
	srv := httptest.NewServer(newServerMux(newServerTestDB(t)))
	defer srv.Close()

	// No Authorization header — should still get 200.
	resp, err := http.Get(srv.URL + "/v1/events")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (reads open even with token set)", resp.StatusCode)
	}
}
