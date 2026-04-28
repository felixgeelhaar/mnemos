package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"

	"github.com/felixgeelhaar/mnemos/internal/store"
)

// resolveDSN returns the canonical store DSN for this process. It is
// the single point that translates the environment + on-disk
// conventions into a value the store registry can dispatch on.
//
// Precedence:
//
//  1. MNEMOS_DB_URL — explicit, takes any registered scheme
//     (sqlite://, sqlite3://, memory://, postgres://, mysql://,
//     libsql://). Used as-is without any path interpretation.
//  2. Otherwise: "sqlite://" + resolveDBPath(), which walks the
//     working directory looking for .mnemos/mnemos.db, then falls
//     back to the XDG global default.
//
// Operators who want a non-SQLite backend set MNEMOS_DB_URL
// explicitly.
func resolveDSN() string {
	if u := os.Getenv("MNEMOS_DB_URL"); u != "" {
		return u
	}
	return "sqlite://" + resolveDBPath()
}

// openConn opens a store connection through the registry using the
// resolved DSN. Callers that previously did
//
//	db, err := sqlite.Open(resolveDBPath())
//
// can migrate by switching to:
//
//	conn, err := openConn(ctx)
//
// and reaching repositories via the port-typed fields on Conn.
// Provider-specific access (e.g. the *sql.DB needed for SQLite-only
// helpers like the deep write probe) is available through Conn.Raw
// during the migration window.
func openConn(ctx context.Context) (*store.Conn, error) {
	return store.Open(ctx, resolveDSN())
}

// openDB is the migration-window helper for cmd/mnemos call sites
// that need *sql.DB directly — entity & compilation_job repositories
// haven't been lifted into ports yet, and several CLI surfaces
// (browse, audit, mcp) still issue raw SQL. It opens the configured
// backend through the registry, then type-asserts Conn.Raw to a
// *sql.DB.
//
// Returns a clear error when MNEMOS_DB_URL points at a non-SQLite
// backend (e.g. memory://) — those call sites genuinely need SQLite
// today, and a loud error here beats a nil-deref later.
//
// The caller owns the *store.Conn lifecycle: defer conn.Close()
// (which runs the provider's Closer and in turn closes the *sql.DB).
// Do not also close the *sql.DB explicitly — that would double-close.
func openDB(ctx context.Context) (*sql.DB, *store.Conn, error) {
	conn, err := openConn(ctx)
	if err != nil {
		return nil, nil, err
	}
	db, ok := conn.Raw.(*sql.DB)
	if !ok || db == nil {
		dsn := resolveDSN()
		_ = conn.Close()
		return nil, nil, fmt.Errorf("backend at %q does not expose *sql.DB; this command requires a SQLite-compatible DSN (sqlite://, sqlite3://) until entity/job repositories are lifted into ports", dsn)
	}
	return db, conn, nil
}

// closeConn closes a *store.Conn, logging any error.
// Use as: defer closeConn(conn)
func closeConn(conn *store.Conn) {
	if err := conn.Close(); err != nil {
		log.Printf("close store conn: %v", err)
	}
}
