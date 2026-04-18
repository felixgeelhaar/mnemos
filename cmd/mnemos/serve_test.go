package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/felixgeelhaar/mnemos/internal/store/sqlite"
)

func newServerTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "mnemos.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func seedEvent(t *testing.T, db *sql.DB, id, runID, content, srcInputID, metaJSON string, ts time.Time) {
	t.Helper()
	_, err := db.ExecContext(context.Background(),
		`INSERT INTO events (id, run_id, schema_version, content, source_input_id, timestamp, metadata_json, ingested_at) VALUES (?, ?, 'v1', ?, ?, ?, ?, ?)`,
		id, runID, content, srcInputID, ts.UTC().Format(time.RFC3339), metaJSON, ts.UTC().Format(time.RFC3339),
	)
	if err != nil {
		t.Fatalf("insert event: %v", err)
	}
}

func TestServe_WebRootReturnsHTML(t *testing.T) {
	srv := httptest.NewServer(newServerMux(newServerTestDB(t)))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if ct == "" || ct[:9] != "text/html" {
		t.Fatalf("Content-Type = %q, want text/html...", ct)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if len(body) < 100 {
		t.Fatalf("body suspiciously small: %d bytes", len(body))
	}
	if !strings.Contains(string(body), "Mnemos Registry") {
		t.Errorf("body missing expected title")
	}
}

func TestServe_WebRootRejectsNonGetWithoutCatchAll(t *testing.T) {
	srv := httptest.NewServer(newServerMux(newServerTestDB(t)))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/random-path-not-a-route")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (root handler shouldn't catch-all)", resp.StatusCode)
	}
}

func TestServe_HealthReturnsOK(t *testing.T) {
	srv := httptest.NewServer(newServerMux(newServerTestDB(t)))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body healthResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Status != "ok" {
		t.Fatalf("status = %q, want 'ok'", body.Status)
	}
}

func TestServe_ListEventsReturnsAndPaginates(t *testing.T) {
	db := newServerTestDB(t)
	base := time.Now().UTC()
	for i := 0; i < 5; i++ {
		seedEvent(t, db, "e"+string(rune('1'+i)), "r1", "claim text "+string(rune('A'+i)), "in"+string(rune('1'+i)), `{"source":"file"}`, base.Add(time.Duration(i)*time.Minute))
	}

	mux := newServerMux(db)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/v1/events?limit=2&offset=1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var body eventsResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Total != 5 {
		t.Errorf("total = %d, want 5", body.Total)
	}
	if body.Limit != 2 || body.Offset != 1 {
		t.Errorf("limit=%d offset=%d, want 2/1", body.Limit, body.Offset)
	}
	if len(body.Events) != 2 {
		t.Errorf("got %d events, want 2", len(body.Events))
	}
}

func TestServe_ListEventsCapsAtMax(t *testing.T) {
	db := newServerTestDB(t)
	mux := newServerMux(db)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/v1/events?limit=10000")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var body eventsResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Limit != maxServePageLimit {
		t.Errorf("limit = %d, want %d (capped)", body.Limit, maxServePageLimit)
	}
}

func TestServe_ListClaimsFiltersByType(t *testing.T) {
	db := newServerTestDB(t)
	now := time.Now().UTC()
	seedClaim(t, db, "c1", "fact one", "fact", "active", 0.8, now)
	seedClaim(t, db, "c2", "decision one", "decision", "active", 0.9, now)
	seedClaim(t, db, "c3", "decision two", "decision", "active", 0.85, now)

	mux := newServerMux(db)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/v1/claims?type=decision")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var body claimsResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Total != 2 || len(body.Claims) != 2 {
		t.Errorf("total=%d len=%d, want 2/2", body.Total, len(body.Claims))
	}
	for _, c := range body.Claims {
		if c.Type != "decision" {
			t.Errorf("got claim with type %q", c.Type)
		}
	}
}

func TestServe_ListClaimsRejectsInvalidType(t *testing.T) {
	srv := httptest.NewServer(newServerMux(newServerTestDB(t)))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/v1/claims?type=bogus")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestServe_ListRelationshipsFiltersByType(t *testing.T) {
	db := newServerTestDB(t)
	now := time.Now().UTC()
	seedClaim(t, db, "c1", "a", "fact", "active", 0.8, now)
	seedClaim(t, db, "c2", "b", "fact", "active", 0.8, now)
	seedClaim(t, db, "c3", "c", "fact", "active", 0.8, now)
	seedRelationship(t, db, "r1", "supports", "c1", "c2", now)
	seedRelationship(t, db, "r2", "contradicts", "c1", "c3", now)

	srv := httptest.NewServer(newServerMux(db))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/v1/relationships?type=contradicts")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var body relationshipsResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Total != 1 || len(body.Relationships) != 1 || body.Relationships[0].ID != "r1" && body.Relationships[0].Type != "contradicts" {
		t.Errorf("expected 1 contradiction, got %+v", body)
	}
}

func TestServe_MetricsCountsSchemaCorrectly(t *testing.T) {
	db := newServerTestDB(t)
	now := time.Now().UTC()
	seedEvent(t, db, "e1", "run-a", "x", "in1", `{}`, now)
	seedEvent(t, db, "e2", "run-b", "y", "in2", `{}`, now)
	seedClaim(t, db, "c1", "a", "fact", "active", 0.8, now)
	seedClaim(t, db, "c2", "b", "fact", "contested", 0.8, now)
	seedRelationship(t, db, "r1", "contradicts", "c1", "c2", now)

	srv := httptest.NewServer(newServerMux(db))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/v1/metrics")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var body metricsResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Runs != 2 || body.Events != 2 || body.Claims != 2 || body.ContestedClaims != 1 || body.Relationships != 1 || body.Contradictions != 1 {
		t.Errorf("metrics = %+v", body)
	}
}

func TestServe_UnsupportedMethodReturns405(t *testing.T) {
	srv := httptest.NewServer(newServerMux(newServerTestDB(t)))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/v1/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", resp.StatusCode)
	}
}

func TestServe_UnknownRouteReturns404(t *testing.T) {
	srv := httptest.NewServer(newServerMux(newServerTestDB(t)))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/v1/nope")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}
