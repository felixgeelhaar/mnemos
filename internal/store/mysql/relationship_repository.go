package mysql

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/felixgeelhaar/mnemos/internal/domain"
)

// RelationshipRepository implements ports.RelationshipRepository.
type RelationshipRepository struct {
	db *sql.DB
}

// Upsert inserts or replaces relationships keyed by id.
func (r RelationshipRepository) Upsert(ctx context.Context, relationships []domain.Relationship) error {
	if len(relationships) == 0 {
		return nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin relationship upsert tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt := `
INSERT INTO relationships (id, type, from_claim_id, to_claim_id, created_at, created_by)
VALUES (?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
  type = VALUES(type),
  from_claim_id = VALUES(from_claim_id),
  to_claim_id = VALUES(to_claim_id)`
	for _, rel := range relationships {
		if err := rel.Validate(); err != nil {
			return fmt.Errorf("invalid relationship %s: %w", rel.ID, err)
		}
		if _, err := tx.ExecContext(ctx, stmt,
			rel.ID, string(rel.Type), rel.FromClaimID, rel.ToClaimID,
			rel.CreatedAt.UTC(), actorOr(rel.CreatedBy),
		); err != nil {
			return fmt.Errorf("upsert relationship %s: %w", rel.ID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit relationship upsert tx: %w", err)
	}
	return nil
}

// ListByClaim returns relationships touching the given claim.
func (r RelationshipRepository) ListByClaim(ctx context.Context, claimID string) ([]domain.Relationship, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT id, type, from_claim_id, to_claim_id, created_at, created_by
FROM relationships WHERE from_claim_id = ? OR to_claim_id = ?`, claimID, claimID)
	if err != nil {
		return nil, fmt.Errorf("list relationships by claim: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return collectRelationshipRows(rows)
}

// ListByClaimIDs returns relationships touching any of the given claims.
func (r RelationshipRepository) ListByClaimIDs(ctx context.Context, claimIDs []string) ([]domain.Relationship, error) {
	if len(claimIDs) == 0 {
		return []domain.Relationship{}, nil
	}
	placeholders, args := inPlaceholders(claimIDs)
	// Same args twice for from_claim_id and to_claim_id IN clauses.
	args2 := append(append([]any{}, args...), args...)
	//nolint:gosec // G202: placeholders are literal "?" tokens, not user input
	q := `
SELECT id, type, from_claim_id, to_claim_id, created_at, created_by
FROM relationships
WHERE from_claim_id IN (` + placeholders + `) OR to_claim_id IN (` + placeholders + `)`
	rows, err := r.db.QueryContext(ctx, q, args2...)
	if err != nil {
		return nil, fmt.Errorf("list relationships by claim ids: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return collectRelationshipRows(rows)
}

func collectRelationshipRows(rows *sql.Rows) ([]domain.Relationship, error) {
	out := make([]domain.Relationship, 0)
	for rows.Next() {
		var rel domain.Relationship
		var typ string
		if err := rows.Scan(&rel.ID, &typ, &rel.FromClaimID, &rel.ToClaimID, &rel.CreatedAt, &rel.CreatedBy); err != nil {
			return nil, fmt.Errorf("scan relationship row: %w", err)
		}
		rel.Type = domain.RelationshipType(typ)
		out = append(out, rel)
	}
	return out, rows.Err()
}
