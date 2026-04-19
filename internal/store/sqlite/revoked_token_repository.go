package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/felixgeelhaar/mnemos/internal/domain"
)

// RevokedTokenRepository tracks JWTs that have been explicitly revoked
// before their natural expiry. Auth middleware checks this on every
// request; tokens past their expires_at can be safely purged.
type RevokedTokenRepository struct {
	db *sql.DB
}

// NewRevokedTokenRepository returns a RevokedTokenRepository backed by
// the given database.
func NewRevokedTokenRepository(db *sql.DB) RevokedTokenRepository {
	return RevokedTokenRepository{db: db}
}

// Add records a token as revoked. Idempotent — re-revoking the same JTI
// is a no-op (preserves the original revoked_at timestamp).
func (r RevokedTokenRepository) Add(ctx context.Context, t domain.RevokedToken) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO revoked_tokens (jti, revoked_at, expires_at) VALUES (?, ?, ?)
		 ON CONFLICT(jti) DO NOTHING`,
		t.JTI,
		t.RevokedAt.UTC().Format(time.RFC3339Nano),
		t.ExpiresAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("insert revoked token %s: %w", t.JTI, err)
	}
	return nil
}

// IsRevoked returns whether the given JTI is in the denylist.
func (r RevokedTokenRepository) IsRevoked(ctx context.Context, jti string) (bool, error) {
	var present int
	err := r.db.QueryRowContext(ctx,
		`SELECT 1 FROM revoked_tokens WHERE jti = ? LIMIT 1`, jti).Scan(&present)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check revoked token %s: %w", jti, err)
	}
	return true, nil
}

// PurgeExpired removes denylist entries whose expires_at is before the
// given cutoff. Returns the count removed. Safe to run periodically
// (e.g., on startup) to keep the table bounded.
func (r RevokedTokenRepository) PurgeExpired(ctx context.Context, before time.Time) (int, error) {
	res, err := r.db.ExecContext(ctx,
		`DELETE FROM revoked_tokens WHERE expires_at < ?`,
		before.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, fmt.Errorf("purge expired revoked tokens: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}
