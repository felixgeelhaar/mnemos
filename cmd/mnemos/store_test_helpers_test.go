package main

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/felixgeelhaar/mnemos/internal/store"
	_ "github.com/felixgeelhaar/mnemos/internal/store/sqlite"
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
