package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
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
func (r RelationshipRepository) Upsert(ctx context.Context, relationships []domain.Relationship) error {
	if len(relationships) == 0 {
		return nil
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin relationship upsert tx: %w", err)
	}
	defer rollbackTx(tx)

	qtx := r.q.WithTx(tx)

	for _, rel := range relationships {
		err := qtx.UpsertRelationship(ctx, sqlcgen.UpsertRelationshipParams{
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
func (r RelationshipRepository) ListByClaim(ctx context.Context, claimID string) ([]domain.Relationship, error) {
	rows, err := r.q.ListRelationshipsByClaim(ctx, sqlcgen.ListRelationshipsByClaimParams{
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

// ListByClaimIDs returns every relationship that touches any of the given
// claim IDs (as source OR target). Used by hop-expansion in the query
// engine — N IDs in one round trip rather than N round trips.
func (r RelationshipRepository) ListByClaimIDs(ctx context.Context, claimIDs []string) ([]domain.Relationship, error) {
	if len(claimIDs) == 0 {
		return []domain.Relationship{}, nil
	}

	placeholders := make([]string, 0, len(claimIDs))
	args := make([]any, 0, len(claimIDs)*2)
	for _, id := range claimIDs {
		placeholders = append(placeholders, "?")
		args = append(args, id)
	}
	for _, id := range claimIDs {
		args = append(args, id)
	}
	in := strings.Join(placeholders, ",")

	//nolint:gosec // G201: placeholders are literal "?", IDs flow through ? bindings
	q := "SELECT id, type, from_claim_id, to_claim_id, created_at FROM relationships WHERE from_claim_id IN (" + in + ") OR to_claim_id IN (" + in + ")"
	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list relationships by claim ids: %w", err)
	}
	defer closeRows(rows)

	out := make([]domain.Relationship, 0)
	for rows.Next() {
		var (
			id, typ, from, to, createdStr string
		)
		if err := rows.Scan(&id, &typ, &from, &to, &createdStr); err != nil {
			return nil, fmt.Errorf("scan relationship row: %w", err)
		}
		t, err := time.Parse(time.RFC3339Nano, createdStr)
		if err != nil {
			return nil, fmt.Errorf("parse relationship created_at: %w", err)
		}
		out = append(out, domain.Relationship{
			ID:          id,
			Type:        domain.RelationshipType(typ),
			FromClaimID: from,
			ToClaimID:   to,
			CreatedAt:   t,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate relationship rows: %w", err)
	}
	return out, nil
}
