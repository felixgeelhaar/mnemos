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

// connDB returns the underlying *sql.DB on a Conn for the small
// number of admin/entity command paths that haven't been lifted to
// ports yet. Returns a clear error when the backend doesn't expose
// a *sql.DB (e.g. memory://) so the failure mode is "this command
// requires a SQL backend" rather than a silent nil deref.
//
// New call sites should reach for ports on Conn directly. This
// helper exists only as a migration shim.
func connDB(conn *store.Conn) (*sql.DB, error) {
	db, ok := conn.Raw.(*sql.DB)
	if !ok || db == nil {
		return nil, fmt.Errorf("backend at %q does not expose *sql.DB; this command requires a SQL-backed DSN (sqlite://, postgres://, mysql://, libsql://)", resolveDSN())
	}
	return db, nil
}

// closeConn closes a *store.Conn, logging any error.
// Use as: defer closeConn(conn)
func closeConn(conn *store.Conn) {
	if err := conn.Close(); err != nil {
		log.Printf("close store conn: %v", err)
	}
}
