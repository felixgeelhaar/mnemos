package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/felixgeelhaar/mnemos/internal/store/sqlite"
)

func TestResolveRegistry_FlagWinsOverEnv(t *testing.T) {
	t.Setenv("MNEMOS_REGISTRY_URL", "https://from-env")
	t.Setenv("MNEMOS_REGISTRY_TOKEN", "env-token")
	regURL, token, err := resolveRegistry("https://from-flag", "flag-token")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if regURL != "https://from-flag" || token != "flag-token" {
		t.Fatalf("got url=%q token=%q, want from-flag/flag-token", regURL, token)
	}
}

func TestResolveRegistry_EnvWinsOverConfigFile(t *testing.T) {
	t.Setenv("MNEMOS_REGISTRY_URL", "https://from-env")
	t.Setenv("MNEMOS_REGISTRY_TOKEN", "")
	// No project config to fall back to in this test process — but env still wins.
	regURL, _, err := resolveRegistry("", "")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if regURL != "https://from-env" {
		t.Fatalf("url = %q, want from-env", regURL)
	}
}

func TestResolveRegistry_ErrorsWhenNothingConfigured(t *testing.T) {
	t.Setenv("MNEMOS_REGISTRY_URL", "")
	t.Setenv("MNEMOS_REGISTRY_TOKEN", "")
	t.Setenv("HOME", t.TempDir())
	t.Chdir(t.TempDir())
	if _, _, err := resolveRegistry("", ""); err == nil {
		t.Fatal("expected error when no source configures a URL")
	}
}

func TestResolveRegistry_TrimsTrailingSlash(t *testing.T) {
	t.Setenv("MNEMOS_REGISTRY_URL", "")
	regURL, _, err := resolveRegistry("https://example.com/", "")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if regURL != "https://example.com" {
		t.Fatalf("url = %q, want trimmed", regURL)
	}
}

func TestEventsToBatches_SplitsAtBoundary(t *testing.T) {
	events := make([]eventDTO, pushBatchSize+5)
	for i := range events {
		events[i] = eventDTO{ID: "e" + string(rune('A'+i%26))}
	}
	batches := eventsToBatches(events)
	if len(batches) != 2 {
		t.Fatalf("got %d batches, want 2", len(batches))
	}
	first, _ := batches[0]["events"].([]eventDTO)
	if len(first) != pushBatchSize {
		t.Errorf("first batch len = %d, want %d", len(first), pushBatchSize)
	}
	second, _ := batches[1]["events"].([]eventDTO)
	if len(second) != 5 {
		t.Errorf("second batch len = %d, want 5", len(second))
	}
}

func TestClaimsToBatches_AttachesEvidenceToFirstBatchOnly(t *testing.T) {
	claims := []claimDTO{{ID: "c1"}, {ID: "c2"}}
	evidence := []claimEvidenceItem{{ClaimID: "c1", EventID: "e1"}}
	batches := claimsToBatches(claims, evidence)
	if len(batches) != 1 {
		t.Fatalf("got %d batches, want 1", len(batches))
	}
	if _, ok := batches[0]["evidence"]; !ok {
		t.Error("first batch missing evidence")
	}
}

// roundTripWithFakeRegistry sets up a fake registry server and runs the
// push/pull cycle against it from a temp local DB. Returns the local DB
// post-pull plus the registry URL for assertions.
func newFakeRegistry(t *testing.T) (string, *http.Client, func()) {
	t.Helper()
	regDB, err := sqlite.Open(filepath.Join(t.TempDir(), "registry.db"))
	if err != nil {
		t.Fatalf("open registry db: %v", err)
	}
	srv := httptest.NewServer(newServerMux(regDB))
	closer := func() {
		srv.Close()
		_ = regDB.Close()
	}
	return srv.URL, srv.Client(), closer
}

func TestPushPull_RoundTripsAllResources(t *testing.T) {
	regURL, _, closeReg := newFakeRegistry(t)
	defer closeReg()

	// Local DB seeded with 1 event, 1 claim, 1 evidence link, 1 relationship.
	localDB, err := sqlite.Open(filepath.Join(t.TempDir(), "local.db"))
	if err != nil {
		t.Fatalf("open local: %v", err)
	}
	defer func() { _ = localDB.Close() }()

	now := time.Now().UTC()
	seedEvent(t, localDB, "ev_1", "r1", "Local fact about caching.", "in_1", `{"source":"file"}`, now)
	seedClaim(t, localDB, "cl_1", "Caching cuts latency by 40%.", "fact", "active", 0.85, now)
	seedClaim(t, localDB, "cl_2", "We chose Redis over Memcached.", "decision", "active", 0.9, now)
	if _, err := localDB.Exec(`INSERT INTO claim_evidence VALUES ('cl_1', 'ev_1')`); err != nil {
		t.Fatalf("seed evidence: %v", err)
	}
	seedRelationship(t, localDB, "rel_1", "supports", "cl_2", "cl_1", now)

	ctx := context.Background()
	client := &http.Client{Timeout: 10 * time.Second}

	// === push ===
	events, _ := loadAllEventsForPush(ctx, localDB)
	claims, evidence, _ := loadAllClaimsForPush(ctx, localDB)
	rels, _ := loadAllRelationshipsForPush(ctx, localDB)

	if n, err := pushBatched(ctx, client, regURL+"/v1/events", "", "events", eventsToBatches(events)); err != nil || n != 1 {
		t.Fatalf("push events n=%d err=%v", n, err)
	}
	if n, err := pushBatched(ctx, client, regURL+"/v1/claims", "", "claims", claimsToBatches(claims, evidence)); err != nil || n != 2 {
		t.Fatalf("push claims n=%d err=%v", n, err)
	}
	if n, err := pushBatched(ctx, client, regURL+"/v1/relationships", "", "relationships", relsToBatches(rels)); err != nil || n != 1 {
		t.Fatalf("push relationships n=%d err=%v", n, err)
	}

	// === pull (into a fresh local DB to verify round-trip) ===
	pullDB, err := sqlite.Open(filepath.Join(t.TempDir(), "pull.db"))
	if err != nil {
		t.Fatalf("open pull db: %v", err)
	}
	defer func() { _ = pullDB.Close() }()

	pulledEvents, err := pullEvents(ctx, client, regURL, "")
	if err != nil {
		t.Fatalf("pull events: %v", err)
	}
	pulledClaims, err := pullClaims(ctx, client, regURL, "")
	if err != nil {
		t.Fatalf("pull claims: %v", err)
	}
	pulledRels, err := pullRelationships(ctx, client, regURL, "")
	if err != nil {
		t.Fatalf("pull relationships: %v", err)
	}

	if n, _ := persistPulledEvents(ctx, pullDB, pulledEvents); n != 1 {
		t.Errorf("inserted events = %d, want 1", n)
	}
	if n, _ := persistPulledClaims(ctx, pullDB, pulledClaims); n != 2 {
		t.Errorf("inserted claims = %d, want 2", n)
	}
	if n, _ := persistPulledRelationships(ctx, pullDB, pulledRels); n != 1 {
		t.Errorf("inserted relationships = %d, want 1", n)
	}

	// Second pull is a no-op (idempotent).
	if n, _ := persistPulledEvents(ctx, pullDB, pulledEvents); n != 0 {
		t.Errorf("second pull inserted events = %d, want 0 (idempotent)", n)
	}
}

func TestPushBatched_FailsOnServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	}))
	defer srv.Close()

	client := srv.Client()
	_, err := pushBatched(context.Background(), client, srv.URL, "", "events",
		[]map[string]any{{"events": []eventDTO{{ID: "e"}}}})
	if err == nil {
		t.Fatal("expected error on 500 response")
	}
}

func TestPushBatched_PassesAuthHeader(t *testing.T) {
	gotAuth := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"accepted":1,"skipped":0}`))
	}))
	defer srv.Close()

	if _, err := pushBatched(context.Background(), srv.Client(), srv.URL, "topsecret", "events",
		[]map[string]any{{"events": []eventDTO{{ID: "e"}}}}); err != nil {
		t.Fatalf("push: %v", err)
	}
	if gotAuth != "Bearer topsecret" {
		t.Fatalf("Authorization header = %q, want 'Bearer topsecret'", gotAuth)
	}
}

func TestPullEvents_PaginatesUntilExhausted(t *testing.T) {
	regDB, err := sqlite.Open(filepath.Join(t.TempDir(), "registry.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = regDB.Close() }()
	now := time.Now().UTC()
	for i := 0; i < pullPageSize+30; i++ {
		seedEvent(t, regDB, "e"+string(rune('a'+i%26))+string(rune('0'+i/26)), "r", "x", "in"+string(rune('a'+i%26)), `{}`, now)
	}

	srv := httptest.NewServer(newServerMux(regDB))
	defer srv.Close()

	got, err := pullEvents(context.Background(), srv.Client(), srv.URL, "")
	if err != nil {
		t.Fatalf("pull: %v", err)
	}
	if len(got) != pullPageSize+30 {
		t.Fatalf("got %d events, want %d", len(got), pullPageSize+30)
	}
}
