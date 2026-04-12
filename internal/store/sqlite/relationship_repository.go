package sqlite

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/felixgeelhaar/mnemos/internal/domain"
)

type RelationshipRepository struct {
	db *sql.DB
}

func NewRelationshipRepository(db *sql.DB) RelationshipRepository {
	return RelationshipRepository{db: db}
}

func (r RelationshipRepository) Upsert(relationships []domain.Relationship) error {
	if len(relationships) == 0 {
		return nil
	}

	const upsert = `
INSERT INTO relationships (id, type, from_claim_id, to_claim_id, created_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(type, from_claim_id, to_claim_id) DO UPDATE SET
	created_at = excluded.created_at`

	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin relationship upsert tx: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(upsert)
	if err != nil {
		return fmt.Errorf("prepare relationship upsert: %w", err)
	}
	defer stmt.Close()

	for _, rel := range relationships {
		_, err := stmt.Exec(
			rel.ID,
			string(rel.Type),
			rel.FromClaimID,
			rel.ToClaimID,
			rel.CreatedAt.UTC().Format(time.RFC3339Nano),
		)
		if err != nil {
			return fmt.Errorf("upsert relationship %s: %w", rel.ID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit relationship upsert tx: %w", err)
	}

	return nil
}

func (r RelationshipRepository) ListByClaim(claimID string) ([]domain.Relationship, error) {
	const query = `
SELECT id, type, from_claim_id, to_claim_id, created_at
FROM relationships
WHERE from_claim_id = ? OR to_claim_id = ?
ORDER BY created_at ASC`

	rows, err := r.db.Query(query, claimID, claimID)
	if err != nil {
		return nil, fmt.Errorf("list relationships by claim: %w", err)
	}
	defer rows.Close()

	rels := make([]domain.Relationship, 0)
	for rows.Next() {
		var (
			rel       domain.Relationship
			relType   string
			createdAt string
		)
		if err := rows.Scan(&rel.ID, &relType, &rel.FromClaimID, &rel.ToClaimID, &createdAt); err != nil {
			return nil, fmt.Errorf("scan relationship: %w", err)
		}
		rel.Type = domain.RelationshipType(relType)
		t, err := time.Parse(time.RFC3339Nano, createdAt)
		if err != nil {
			return nil, fmt.Errorf("parse relationship created_at: %w", err)
		}
		rel.CreatedAt = t
		rels = append(rels, rel)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate relationship rows: %w", err)
	}

	return rels, nil
}
