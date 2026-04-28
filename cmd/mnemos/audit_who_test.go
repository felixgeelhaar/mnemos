package main

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/felixgeelhaar/mnemos/internal/store/sqlite"
)

// seedAuditFixture writes a small mixed history attributed to two
// distinct principals so the filter tests have something to discriminate.
func seedAuditFixture(t *testing.T, db *sql.DB, now time.Time) {
	t.Helper()
	ctx := context.Background()
	rfc := func(o time.Duration) string { return now.Add(o).UTC().Format(time.RFC3339Nano) }

	mustExec := func(q string, args ...any) {
		if _, err := db.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("exec %q: %v", q, err)
		}
	}

	// Two events: alice owns one, bob owns one.
	mustExec(`INSERT INTO events (id, run_id, schema_version, content, source_input_id, timestamp, metadata_json, ingested_at, created_by) VALUES (?, ?, 'v1', ?, ?, ?, '{}', ?, ?)`,
		"ev_alice", "r", "alice's event", "in1", rfc(0), rfc(0), "usr_alice")
	mustExec(`INSERT INTO events (id, run_id, schema_version, content, source_input_id, timestamp, metadata_json, ingested_at, created_by) VALUES (?, ?, 'v1', ?, ?, ?, '{}', ?, ?)`,
		"ev_bob", "r", "bob's event", "in2", rfc(time.Minute), rfc(time.Minute), "usr_bob")

	// Two claims, same split.
	mustExec(`INSERT INTO claims (id, text, type, confidence, status, created_at, created_by) VALUES (?, ?, 'fact', 0.8, 'active', ?, ?)`,
		"cl_alice", "alice's claim", rfc(0), "usr_alice")
	mustExec(`INSERT INTO claims (id, text, type, confidence, status, created_at, created_by) VALUES (?, ?, 'fact', 0.8, 'active', ?, ?)`,
		"cl_bob", "bob's claim", rfc(time.Minute), "usr_bob")

	// One relationship from alice.
	mustExec(`INSERT INTO relationships (id, type, from_claim_id, to_claim_id, created_at, created_by) VALUES (?, 'supports', ?, ?, ?, ?)`,
		"rel_alice", "cl_alice", "cl_bob", rfc(2*time.Minute), "usr_alice")

	// One embedding from bob.
	if err := sqlite.NewEmbeddingRepository(db).Upsert(ctx, "ev_bob", "event", []float32{0.1, 0.2}, "test-model", "usr_bob"); err != nil {
		t.Fatalf("upsert embedding: %v", err)
	}

	// One status transition by alice — record directly to avoid going
	// through the upsert path.
	mustExec(`INSERT INTO claim_status_history (claim_id, from_status, to_status, changed_at, reason, changed_by) VALUES (?, 'active', 'contested', ?, 'test', ?)`,
		"cl_alice", rfc(3*time.Minute), "usr_alice")
}

func TestBuildAuditWhoExport_FiltersByPrincipal(t *testing.T) {
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "audit.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	seedAuditFixture(t, db, time.Now().UTC().Add(-time.Hour))

	alice, err := buildAuditWhoExport(context.Background(), db, "usr_alice", time.Time{})
	if err != nil {
		t.Fatalf("alice: %v", err)
	}
	if alice.Counts.Events != 1 || alice.Counts.Claims != 1 ||
		alice.Counts.Relationships != 1 || alice.Counts.Embeddings != 0 ||
		alice.Counts.Transitions != 1 {
		t.Errorf("alice counts wrong: %+v", alice.Counts)
	}
	if len(alice.Events) != 1 || alice.Events[0].ID != "ev_alice" {
		t.Errorf("alice event content wrong: %+v", alice.Events)
	}

	bob, err := buildAuditWhoExport(context.Background(), db, "usr_bob", time.Time{})
	if err != nil {
		t.Fatalf("bob: %v", err)
	}
	if bob.Counts.Events != 1 || bob.Counts.Claims != 1 ||
		bob.Counts.Relationships != 0 || bob.Counts.Embeddings != 1 ||
		bob.Counts.Transitions != 0 {
		t.Errorf("bob counts wrong: %+v", bob.Counts)
	}
}

func TestBuildAuditWhoExport_SinceFilterPrunesOlderRows(t *testing.T) {
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "audit.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	old := time.Now().UTC().Add(-48 * time.Hour)
	seedAuditFixture(t, db, old)

	// since=24h ago → all the seeded rows are older than that, so
	// alice's report should be empty (zero counts) but well-formed.
	since := time.Now().UTC().Add(-24 * time.Hour)
	exp, err := buildAuditWhoExport(context.Background(), db, "usr_alice", since)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if exp.Counts.Events != 0 || exp.Counts.Claims != 0 ||
		exp.Counts.Relationships != 0 || exp.Counts.Transitions != 0 {
		t.Errorf("since filter didn't prune everything: %+v", exp.Counts)
	}
	if exp.Since == "" {
		t.Errorf("Since timestamp should be set on export")
	}
	// Conversely, no since filter returns the full alice slice.
	full, err := buildAuditWhoExport(context.Background(), db, "usr_alice", time.Time{})
	if err != nil {
		t.Fatalf("full: %v", err)
	}
	if full.Counts.Events != 1 || full.Counts.Claims != 1 || full.Counts.Transitions != 1 {
		t.Errorf("no-since alice counts wrong: %+v", full.Counts)
	}
}

func TestBuildAuditWhoExport_UnknownPrincipalIsEmpty(t *testing.T) {
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "audit.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	exp, err := buildAuditWhoExport(context.Background(), db, "usr_nonexistent", time.Time{})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if exp.Counts.Events != 0 || exp.Counts.Claims != 0 ||
		exp.Counts.Relationships != 0 || exp.Counts.Embeddings != 0 ||
		exp.Counts.Transitions != 0 {
		t.Errorf("expected empty report for unknown principal, got %+v", exp.Counts)
	}
	if exp.Principal != "usr_nonexistent" {
		t.Errorf("Principal field not echoed back: %q", exp.Principal)
	}
}

func TestBuildAuditWhoExport_SystemSentinelMatches(t *testing.T) {
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "audit.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// Seed a row with the explicit <system> created_by — pipeline
	// writes use this when no real actor is configured.
	now := time.Now().UTC()
	if _, err := db.Exec(`INSERT INTO events (id, run_id, schema_version, content, source_input_id, timestamp, metadata_json, ingested_at, created_by) VALUES (?, ?, 'v1', ?, ?, ?, '{}', ?, '<system>')`,
		"ev_sys", "r", "system event", "in", now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("seed: %v", err)
	}

	exp, err := buildAuditWhoExport(context.Background(), db, "<system>", time.Time{})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if exp.Counts.Events != 1 {
		t.Errorf("expected 1 system event, got %d", exp.Counts.Events)
	}
}
