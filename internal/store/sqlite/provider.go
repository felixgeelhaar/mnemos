package sqlite

import (
	"context"
	"fmt"
	"strings"

	"github.com/felixgeelhaar/mnemos/internal/store"
)

// Register the SQLite provider with the top-level store factory. Both
// "sqlite" and "sqlite3" are accepted so DSNs from different ecosystems
// (database/sql driver name vs. URL convention) work uniformly.
//
// Existing call sites that use [Open] directly are unaffected; this
// init() only adds a parallel entry point reachable through
// [store.Open].
func init() {
	store.Register("sqlite", openProvider)
	store.Register("sqlite3", openProvider)
}

// openProvider parses a sqlite[3]:// DSN, opens the underlying database
// via the existing [open] function, and bundles every port-typed
// repository into a [store.Conn].
func openProvider(_ context.Context, dsn string) (*store.Conn, error) {
	path, err := pathFromDSN(dsn)
	if err != nil {
		return nil, err
	}
	db, err := open(path)
	if err != nil {
		return nil, err
	}
	return &store.Conn{
		Events:        NewEventRepository(db),
		Claims:        NewClaimRepository(db),
		Relationships: NewRelationshipRepository(db),
		Embeddings:    NewEmbeddingRepository(db),
		Users:         NewUserRepository(db),
		RevokedTokens: NewRevokedTokenRepository(db),
		Agents:        NewAgentRepository(db),
		Entities:      NewEntityRepository(db),
		Jobs:          NewCompilationJobRepository(db),
		Raw:           db,
		Closer:        db.Close,
	}, nil
}

// pathFromDSN extracts the filesystem path from a sqlite:// or
// sqlite3:// DSN. The canonical form is sqlite:///<absolute-path>;
// relative paths are accepted as sqlite://<rel> for ergonomics.
//
// Query parameters (e.g. ?namespace=foo) are stripped here in
// Phase 1a — the SQLite namespace contract (distinct file or
// ATTACH DATABASE) ships in a later phase. Callers that pass query
// strings will see them ignored, not an error, so we don't break
// future-compatible DSNs.
func pathFromDSN(dsn string) (string, error) {
	const (
		prefix2 = "sqlite://"
		prefix3 = "sqlite3://"
	)
	var rest string
	switch {
	case strings.HasPrefix(dsn, prefix2):
		rest = strings.TrimPrefix(dsn, prefix2)
	case strings.HasPrefix(dsn, prefix3):
		rest = strings.TrimPrefix(dsn, prefix3)
	default:
		return "", fmt.Errorf("sqlite: not a sqlite dsn: %q", dsn)
	}
	if i := strings.Index(rest, "?"); i >= 0 {
		rest = rest[:i]
	}
	if rest == "" {
		return "", fmt.Errorf("sqlite: dsn %q has no path", dsn)
	}
	return rest, nil
}
