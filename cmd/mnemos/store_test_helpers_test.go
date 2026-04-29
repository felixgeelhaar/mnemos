package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/felixgeelhaar/mnemos/internal/domain"
	"github.com/felixgeelhaar/mnemos/internal/store"
	"github.com/felixgeelhaar/mnemos/internal/store/sqlite"
)

// openTestStore opens a fresh SQLite-backed Conn at a temp path,
// returning the *sql.DB raw handle alongside so tests that still
// issue raw SQL can do so without separately opening the file.
//
// The Conn's Close() runs through the registry and tears down both
// the Conn and its underlying *sql.DB, so callers should defer
// conn.Close() (registered via t.Cleanup here) instead of closing the
// DB themselves.
func openTestStore(t *testing.T) (*sql.DB, *store.Conn) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "mnemos.db")
	conn, err := store.Open(context.Background(), "sqlite://"+dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	db, ok := conn.Raw.(*sql.DB)
	if !ok || db == nil {
		t.Fatal("sqlite Conn missing *sql.DB raw handle")
	}
	return db, conn
}

// newServerTestStore_conn returns just the *store.Conn for tests that
// don't need a raw db handle to seed fixtures.
func newServerTestStore_conn(t *testing.T) *store.Conn {
	t.Helper()
	_, conn := openTestStore(t)
	return conn
}

// connFromDB wraps an existing *sql.DB into a *store.Conn so tests
// that seeded fixtures directly through the *sql.DB can still pass a
// Conn to handler factories that take *store.Conn. Uses the sqlite
// repositories directly — the underlying db is shared so seeded data
// is visible.
//
// The returned Conn does not close the db; the caller (or the
// helper that opened it) retains ownership.
func connFromDB(_ *testing.T, db *sql.DB) *store.Conn {
	return &store.Conn{
		Events:        sqlite.NewEventRepository(db),
		Claims:        sqlite.NewClaimRepository(db),
		Relationships: sqlite.NewRelationshipRepository(db),
		Embeddings:    sqlite.NewEmbeddingRepository(db),
		Users:         sqlite.NewUserRepository(db),
		RevokedTokens: sqlite.NewRevokedTokenRepository(db),
		Agents:        sqlite.NewAgentRepository(db),
		Entities:      sqlite.NewEntityRepository(db),
		Jobs:          sqlite.NewCompilationJobRepository(db),
		Raw:           db,
		Closer:        func() error { return nil },
	}
}

// seedEventConn inserts an event through the port interface.
func seedEventConn(t *testing.T, conn *store.Conn, id, runID, content, srcInputID, metaJSON string, ts time.Time) {
	t.Helper()
	seedEventConnAs(t, conn, id, runID, content, srcInputID, metaJSON, ts, domain.SystemUser)
}

// seedEventConnAs inserts an event through the port interface with a
// specific created_by actor.
func seedEventConnAs(t *testing.T, conn *store.Conn, id, runID, content, srcInputID, metaJSON string, ts time.Time, createdBy string) {
	t.Helper()
	var meta map[string]string
	if metaJSON != "" {
		if err := json.Unmarshal([]byte(metaJSON), &meta); err != nil {
			t.Fatalf("unmarshal metadata: %v", err)
		}
	}
	evt := domain.Event{
		ID:            id,
		RunID:         runID,
		SchemaVersion: "v1",
		Content:       content,
		SourceInputID: srcInputID,
		Timestamp:     ts,
		Metadata:      meta,
		IngestedAt:    ts,
		CreatedBy:     createdBy,
	}
	if err := conn.Events.Append(context.Background(), evt); err != nil {
		t.Fatalf("append event: %v", err)
	}
}

// seedClaimConn inserts a claim through the port interface.
func seedClaimConn(t *testing.T, conn *store.Conn, id, text, ctype, status string, confidence float64, createdAt time.Time) {
	t.Helper()
	seedClaimConnAs(t, conn, id, text, ctype, status, confidence, createdAt, domain.SystemUser)
}

// seedClaimConnAs inserts a claim through the port interface with a
// specific created_by actor.
func seedClaimConnAs(t *testing.T, conn *store.Conn, id, text, ctype, status string, confidence float64, createdAt time.Time, createdBy string) {
	t.Helper()
	claim := domain.Claim{
		ID:         id,
		Text:       text,
		Type:       domain.ClaimType(ctype),
		Status:     domain.ClaimStatus(status),
		Confidence: confidence,
		CreatedAt:  createdAt,
		CreatedBy:  createdBy,
	}
	if err := conn.Claims.Upsert(context.Background(), []domain.Claim{claim}); err != nil {
		t.Fatalf("upsert claim: %v", err)
	}
}

// seedRelationshipConn inserts a relationship through the port interface.
func seedRelationshipConn(t *testing.T, conn *store.Conn, id, rtype, from, to string, createdAt time.Time) {
	t.Helper()
	seedRelationshipConnAs(t, conn, id, rtype, from, to, createdAt, domain.SystemUser)
}

// seedRelationshipConnAs inserts a relationship through the port
// interface with a specific created_by actor.
func seedRelationshipConnAs(t *testing.T, conn *store.Conn, id, rtype, from, to string, createdAt time.Time, createdBy string) {
	t.Helper()
	rel := domain.Relationship{
		ID:          id,
		Type:        domain.RelationshipType(rtype),
		FromClaimID: from,
		ToClaimID:   to,
		CreatedAt:   createdAt,
		CreatedBy:   createdBy,
	}
	if err := conn.Relationships.Upsert(context.Background(), []domain.Relationship{rel}); err != nil {
		t.Fatalf("upsert relationship: %v", err)
	}
}
