package main

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

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
// that opened raw SQLite (e.g. via sqlite.Open) and seeded fixtures
// directly through the *sql.DB can still pass a Conn to handler
// factories that take *store.Conn. Uses the sqlite repositories
// directly — the underlying db is shared so seeded data is visible.
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
