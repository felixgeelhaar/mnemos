package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/felixgeelhaar/mnemos/internal/domain"
)

// RelationshipRepository persists claim → claim edges. The (id) is
// the dedup key; ON CONFLICT (id) DO UPDATE matches the SQLite
// upsert semantics.
type RelationshipRepository struct {
	db *sql.DB
	ns string
}

// Upsert satisfies the corresponding ports method.
func (r RelationshipRepository) Upsert(ctx context.Context, relationships []domain.Relationship) error {
	if len(relationships) == 0 {
		return nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin relationship upsert tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt := fmt.Sprintf(`
INSERT INTO %s (id, type, from_claim_id, to_claim_id, created_at, created_by)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (id) DO UPDATE SET
  type = EXCLUDED.type,
  from_claim_id = EXCLUDED.from_claim_id,
  to_claim_id = EXCLUDED.to_claim_id`, qualify(r.ns, "relationships"))
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

// ListByClaim satisfies the corresponding ports method.
func (r RelationshipRepository) ListByClaim(ctx context.Context, claimID string) ([]domain.Relationship, error) {
	rows, err := r.db.QueryContext(ctx, fmt.Sprintf(`
SELECT id, type, from_claim_id, to_claim_id, created_at, created_by
FROM %s WHERE from_claim_id = $1 OR to_claim_id = $1`, qualify(r.ns, "relationships")), claimID)
	if err != nil {
		return nil, fmt.Errorf("list relationships by claim: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return collectRelationshipRows(rows)
}

// ListByClaimIDs satisfies the corresponding ports method.
func (r RelationshipRepository) ListByClaimIDs(ctx context.Context, claimIDs []string) ([]domain.Relationship, error) {
	if len(claimIDs) == 0 {
		return []domain.Relationship{}, nil
	}
	rows, err := r.db.QueryContext(ctx, fmt.Sprintf(`
SELECT id, type, from_claim_id, to_claim_id, created_at, created_by
FROM %s WHERE from_claim_id = ANY($1) OR to_claim_id = ANY($1)`, qualify(r.ns, "relationships")), pgArray(claimIDs))
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
