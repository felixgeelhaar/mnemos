package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/felixgeelhaar/mnemos/internal/domain"
	"github.com/felixgeelhaar/mnemos/internal/store/sqlite/sqlcgen"
)

// RelationshipRepository provides SQLite-backed storage for claim relationships.
type RelationshipRepository struct {
	db *sql.DB
	q  *sqlcgen.Queries
}

// NewRelationshipRepository returns a RelationshipRepository backed by the given database.
func NewRelationshipRepository(db *sql.DB) RelationshipRepository {
	return RelationshipRepository{db: db, q: sqlcgen.New(db)}
}

// Upsert inserts or updates the given relationships in a single transaction.
func (r RelationshipRepository) Upsert(relationships []domain.Relationship) error {
	if len(relationships) == 0 {
		return nil
	}

	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin relationship upsert tx: %w", err)
	}
	defer rollbackTx(tx)

	qtx := r.q.WithTx(tx)

	for _, rel := range relationships {
		err := qtx.UpsertRelationship(context.Background(), sqlcgen.UpsertRelationshipParams{
			ID:          rel.ID,
			Type:        string(rel.Type),
			FromClaimID: rel.FromClaimID,
			ToClaimID:   rel.ToClaimID,
			CreatedAt:   rel.CreatedAt.UTC().Format(time.RFC3339Nano),
		})
		if err != nil {
			return fmt.Errorf("upsert relationship %s: %w", rel.ID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit relationship upsert tx: %w", err)
	}

	return nil
}

// ListByClaim returns all relationships where the given claim is either the source or target.
func (r RelationshipRepository) ListByClaim(claimID string) ([]domain.Relationship, error) {
	rows, err := r.q.ListRelationshipsByClaim(context.Background(), sqlcgen.ListRelationshipsByClaimParams{
		FromClaimID: claimID,
		ToClaimID:   claimID,
	})
	if err != nil {
		return nil, fmt.Errorf("list relationships by claim: %w", err)
	}

	rels := make([]domain.Relationship, 0, len(rows))
	for _, row := range rows {
		t, err := time.Parse(time.RFC3339Nano, row.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("parse relationship created_at: %w", err)
		}
		rels = append(rels, domain.Relationship{
			ID:          row.ID,
			Type:        domain.RelationshipType(row.Type),
			FromClaimID: row.FromClaimID,
			ToClaimID:   row.ToClaimID,
			CreatedAt:   t,
		})
	}

	return rels, nil
}
