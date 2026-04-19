package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/felixgeelhaar/mnemos/internal/auth"
	"github.com/felixgeelhaar/mnemos/internal/domain"
	"github.com/felixgeelhaar/mnemos/internal/store/sqlite"
)

// setupJWTTestEnv wires a per-test JWT secret so newServerMux boots a
// verifier the test can also use to mint tokens. Each test gets its own
// tmpdir-scoped secret and a fresh project root, keeping tests isolated.
func setupJWTTestEnv(t *testing.T) (secret []byte, cleanupDir string) {
	t.Helper()
	secret = make([]byte, 32)
	for i := range secret {
		secret[i] = byte(i + 1)
	}
	t.Setenv("MNEMOS_JWT_SECRET", hex.EncodeToString(secret))
	t.Setenv("HOME", t.TempDir()) // contain DefaultSecretPath if it's consulted
	return secret, t.TempDir()
}

// issueTestToken mints a JWT for a known user id using the secret that
// setupJWTTestEnv injected. Returns the Authorization header value.
func issueTestToken(t *testing.T, secret []byte, userID string) string {
	t.Helper()
	user := domain.User{
		ID:        userID,
		Name:      userID,
		Email:     userID + "@test.local",
		Status:    domain.UserStatusActive,
		CreatedAt: time.Now().UTC(),
	}
	tok, _, err := auth.NewIssuer(secret).IssueUserToken(user, time.Hour)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	return "Bearer " + tok
}

// seedUser persists a user so tests can verify user_id → created_by.
func seedUser(t *testing.T, db *sql.DB, id string) {
	t.Helper()
	err := sqlite.NewUserRepository(db).Create(context.Background(), domain.User{
		ID:        id,
		Name:      id,
		Email:     id + "@test.local",
		Status:    domain.UserStatusActive,
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
}

// serveJWTBase holds the shared bits a test needs: a fresh DB with a
// per-test JWT secret wired in, and a ready Authorization header.
type serveJWTBase struct {
	DB    *sql.DB
	Srv   *httptest.Server
	Auth  map[string]string
	Actor string
}

func newServeJWTTest(t *testing.T) serveJWTBase {
	t.Helper()
	secret, _ := setupJWTTestEnv(t)
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "mnemos.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	seedUser(t, db, "usr_writer")
	srv := httptest.NewServer(newServerMux(db))
	t.Cleanup(srv.Close)
	return serveJWTBase{
		DB:    db,
		Srv:   srv,
		Auth:  map[string]string{"Authorization": issueTestToken(t, secret, "usr_writer")},
		Actor: "usr_writer",
	}
}

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
	st := newServeJWTTest(t)

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

	resp := postJSON(t, st.Srv.URL+"/v1/events", body, st.Auth)
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
	getResp, err := http.Get(st.Srv.URL + "/v1/events")
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

	// And verify the JWT subject landed on created_by.
	var createdBy string
	if err := st.DB.QueryRowContext(context.Background(), `SELECT created_by FROM events WHERE id = ?`, "ev_post_1").Scan(&createdBy); err != nil {
		t.Fatalf("read created_by: %v", err)
	}
	if createdBy != st.Actor {
		t.Errorf("created_by = %q, want %q", createdBy, st.Actor)
	}
}

func TestServe_AppendEvents_RejectsEmptyArray(t *testing.T) {
	st := newServeJWTTest(t)
	resp := postJSON(t, st.Srv.URL+"/v1/events", map[string]any{"events": []any{}}, st.Auth)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestServe_AppendEvents_RejectsBadTimestamp(t *testing.T) {
	st := newServeJWTTest(t)
	body := map[string]any{
		"events": []map[string]any{{"id": "ev_x", "content": "x", "timestamp": "yesterday"}},
	}
	resp := postJSON(t, st.Srv.URL+"/v1/events", body, st.Auth)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestServe_AppendClaims_PersistsAndCanBeListed(t *testing.T) {
	st := newServeJWTTest(t)

	now := time.Now().UTC()
	// Need an event for the evidence link FK to resolve.
	seedEvent(t, st.DB, "ev_for_claim", "r1", "context", "in_e", `{}`, now)

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
	resp := postJSON(t, st.Srv.URL+"/v1/claims", body, st.Auth)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		var msg errorResponse
		_ = json.NewDecoder(resp.Body).Decode(&msg)
		t.Fatalf("status = %d, want 201 (%v)", resp.StatusCode, msg.Error)
	}

	// Verify the evidence link landed.
	var n int
	if err := st.DB.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM claim_evidence WHERE claim_id = ? AND event_id = ?`, "cl_post_1", "ev_for_claim").Scan(&n); err != nil {
		t.Fatalf("count evidence: %v", err)
	}
	if n != 1 {
		t.Fatalf("evidence rows = %d, want 1", n)
	}

	// The JWT subject should have been stamped on the claim.
	var createdBy string
	if err := st.DB.QueryRowContext(context.Background(), `SELECT created_by FROM claims WHERE id = ?`, "cl_post_1").Scan(&createdBy); err != nil {
		t.Fatalf("read created_by: %v", err)
	}
	if createdBy != st.Actor {
		t.Errorf("created_by = %q, want %q", createdBy, st.Actor)
	}
}

func TestServe_AppendClaims_RejectsInvalidType(t *testing.T) {
	st := newServeJWTTest(t)
	body := map[string]any{
		"claims": []map[string]any{
			{"id": "cl_x", "text": "x", "type": "guess", "confidence": 0.5, "status": "active"},
		},
	}
	resp := postJSON(t, st.Srv.URL+"/v1/claims", body, st.Auth)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestServe_AppendRelationships_PersistsCorrectly(t *testing.T) {
	st := newServeJWTTest(t)

	now := time.Now().UTC()
	seedClaim(t, st.DB, "c-from", "a", "fact", "active", 0.8, now)
	seedClaim(t, st.DB, "c-to", "b", "fact", "active", 0.8, now)

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
	resp := postJSON(t, st.Srv.URL+"/v1/relationships", body, st.Auth)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		var msg errorResponse
		_ = json.NewDecoder(resp.Body).Decode(&msg)
		t.Fatalf("status = %d, want 201 (%v)", resp.StatusCode, msg.Error)
	}
}

func TestServe_AppendRelationships_RejectsBadType(t *testing.T) {
	st := newServeJWTTest(t)
	body := map[string]any{
		"relationships": []map[string]any{
			{"id": "r-x", "type": "neutralizes", "from_claim_id": "c1", "to_claim_id": "c2"},
		},
	}
	resp := postJSON(t, st.Srv.URL+"/v1/relationships", body, st.Auth)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestServe_Auth_RejectsMissingHeader(t *testing.T) {
	st := newServeJWTTest(t)
	resp := postJSON(t, st.Srv.URL+"/v1/events",
		map[string]any{"events": []map[string]any{{"id": "e1", "content": "x"}}}, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestServe_Auth_RejectsBadSignature(t *testing.T) {
	st := newServeJWTTest(t)
	// Mint a token against a different secret; verifier should reject.
	other := make([]byte, 32)
	for i := range other {
		other[i] = 0xAB
	}
	user := domain.User{ID: "usr_x", Name: "x", Email: "x@test.local", Status: domain.UserStatusActive, CreatedAt: time.Now().UTC()}
	bad, _, err := auth.NewIssuer(other).IssueUserToken(user, time.Hour)
	if err != nil {
		t.Fatalf("issue bad token: %v", err)
	}

	resp := postJSON(t, st.Srv.URL+"/v1/events",
		map[string]any{"events": []map[string]any{{"id": "e1", "content": "x"}}},
		map[string]string{"Authorization": "Bearer " + bad})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestServe_Auth_RejectsRevokedJTI(t *testing.T) {
	secret, _ := setupJWTTestEnv(t)
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "mnemos.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	seedUser(t, db, "usr_revoked")

	user := domain.User{ID: "usr_revoked", Name: "r", Email: "r@test.local", Status: domain.UserStatusActive, CreatedAt: time.Now().UTC()}
	tok, jti, err := auth.NewIssuer(secret).IssueUserToken(user, time.Hour)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	// Drop the JTI on the denylist before the mux consults it.
	if err := sqlite.NewRevokedTokenRepository(db).Add(context.Background(), domain.RevokedToken{
		JTI: jti, RevokedAt: time.Now().UTC(), ExpiresAt: time.Now().Add(24 * time.Hour),
	}); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	srv := httptest.NewServer(newServerMux(db))
	defer srv.Close()

	resp := postJSON(t, srv.URL+"/v1/events",
		map[string]any{"events": []map[string]any{{"id": "e1", "content": "x"}}},
		map[string]string{"Authorization": "Bearer " + tok})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (token revoked)", resp.StatusCode)
	}
}

func TestServe_Auth_ReadsAlwaysAllowed(t *testing.T) {
	st := newServeJWTTest(t)
	// No Authorization header — reads still return 200.
	resp, err := http.Get(st.Srv.URL + "/v1/events")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (reads open)", resp.StatusCode)
	}
}
