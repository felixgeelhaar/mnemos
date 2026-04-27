// Package postgres implements a [store] provider backed by Postgres.
//
// Status: scaffold. The provider registers itself for the
// `postgres://` and `postgresql://` schemes, parses DSNs (including
// the `?namespace=` parameter from ADR 0001), and exposes a Conn
// builder. Repository implementations are pending — Open currently
// returns ErrNotImplemented so callers see a clear, actionable error
// instead of a misleading "no driver" message.
//
// Roadmap (tracked in roady as the Postgres-provider feature):
//
//  1. Bring in github.com/jackc/pgx/v5/stdlib for database/sql
//     compatibility.
//  2. Run schema.sql at Open time, then SET search_path to the
//     resolved namespace.
//  3. Implement every repository in this package, mirroring the
//     SQLite shape (sqlcgen → equivalent Postgres SQL).
//  4. Optional capabilities via build tags: pgvector for
//     ports.VectorSearcher, tsvector for ports.TextSearcher.
//  5. CI Postgres job (docker-compose) gated on TEST_POSTGRES_DSN.
//
// Until those land, callers depending on `postgres://` should
// either point at `sqlite://` for local use or `memory://` for
// tests.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/felixgeelhaar/mnemos/internal/store"
)

// ErrNotImplemented is returned by Open while the Postgres provider
// is in scaffold mode. Wrapped errors include the resolved DSN so
// operators see exactly what was attempted.
var ErrNotImplemented = errors.New("postgres provider: not yet implemented (see ADR 0001 roadmap)")

// Register the postgres provider for both common scheme aliases.
// Both `postgres://` and `postgresql://` are accepted because both
// appear in the wild (libpq, JDBC, Go's pq driver, pgx).
func init() {
	store.Register("postgres", openProvider)
	store.Register("postgresql", openProvider)
}

// DSN holds the parsed components of a Postgres DSN. Exposed for
// future repository wiring and tests.
type DSN struct {
	// Raw is the original DSN string the operator supplied.
	Raw string

	// LibpqDSN is the Raw DSN minus the namespace query parameter.
	// Pass this to pgx/pq once the database/sql driver is wired.
	LibpqDSN string

	// Namespace is the value of ?namespace= (or the default
	// "mnemos"), validated against `^[a-z][a-z0-9_]{0,62}$` per the
	// ADR. Used as the Postgres schema name (CREATE SCHEMA …).
	Namespace string
}

// namespaceRE mirrors the contract from ADR 0001 §3: lowercase,
// alphanumeric+underscore, must start with a letter, max 63 bytes
// (Postgres identifier limit).
var namespaceRE = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)

const defaultNamespace = "mnemos"

// ParseDSN extracts the namespace and produces a libpq-compatible
// DSN with the namespace query parameter stripped (Postgres drivers
// would otherwise reject the unknown key).
func ParseDSN(dsn string) (DSN, error) {
	if !strings.HasPrefix(dsn, "postgres://") && !strings.HasPrefix(dsn, "postgresql://") {
		return DSN{}, fmt.Errorf("postgres: not a postgres dsn: %q", dsn)
	}
	u, err := url.Parse(dsn)
	if err != nil {
		return DSN{}, fmt.Errorf("postgres: parse dsn: %w", err)
	}

	q := u.Query()
	ns := q.Get("namespace")
	if ns == "" {
		ns = defaultNamespace
	}
	if !namespaceRE.MatchString(ns) {
		return DSN{}, fmt.Errorf("postgres: invalid namespace %q (want %s)", ns, namespaceRE.String())
	}
	q.Del("namespace")
	u.RawQuery = q.Encode()

	return DSN{
		Raw:       dsn,
		LibpqDSN:  u.String(),
		Namespace: ns,
	}, nil
}

// openProvider parses the DSN and would normally open a Postgres
// connection. While the provider is in scaffold mode it returns
// ErrNotImplemented so misconfigured deployments fail loudly with a
// pointer to the roadmap.
func openProvider(_ context.Context, dsn string) (*store.Conn, error) {
	parsed, err := ParseDSN(dsn)
	if err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("%w (dsn=%s namespace=%s)", ErrNotImplemented, parsed.LibpqDSN, parsed.Namespace)
}
