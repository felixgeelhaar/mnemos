package main

import (
	"context"
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
//     (sqlite://, sqlite3://, memory://, future postgres://, ...).
//     Used as-is without any path interpretation.
//  2. Otherwise: "sqlite://" + resolveDBPath(), which itself walks
//     MNEMOS_DB_PATH → nearest project .mnemos/mnemos.db → XDG
//     default.
//
// The legacy MNEMOS_DB_PATH variable continues to work unchanged
// because resolveDBPath still consults it. Operators who want a
// non-SQLite backend (or who want to be explicit) should set
// MNEMOS_DB_URL.
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
