package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/felixgeelhaar/mnemos/internal/domain"
)

// EntityRelationshipRepository persists polymorphic cross-entity
// edges (action_of, outcome_of, validates, refutes, derived_from,
// causes between non-claim endpoints).
type EntityRelationshipRepository struct {
	db *sql.DB
}

// NewEntityRelationshipRepository returns a repository backed by db.
func NewEntityRelationshipRepository(db *sql.DB) EntityRelationshipRepository {
	return EntityRelationshipRepository{db: db}
}

// Upsert writes edges idempotently on the unique (kind, from_type,
// from_id, to_type, to_id) tuple. Re-emitting the same edge is a
// no-op.
func (r EntityRelationshipRepository) Upsert(ctx context.Context, edges []domain.EntityRelationship) error {
	if len(edges) == 0 {
		return nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin entity_relationships tx: %w", err)
	}
	defer rollbackTx(tx)
	for _, e := range edges {
		if err := e.Validate(); err != nil {
			return fmt.Errorf("invalid entity_relationship: %w", err)
		}
		createdAt := e.CreatedAt
		if createdAt.IsZero() {
			createdAt = time.Now().UTC()
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO entity_relationships (id, kind, from_id, from_type, to_id, to_type, created_at, created_by)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(kind, from_type, from_id, to_type, to_id) DO NOTHING`,
			e.ID, string(e.Kind), e.FromID, e.FromType, e.ToID, e.ToType,
			createdAt.UTC().Format(time.RFC3339Nano),
			actorOr(e.CreatedBy),
		); err != nil {
			return fmt.Errorf("insert entity_relationship %s: %w", e.ID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit entity_relationships tx: %w", err)
	}
	return nil
}

// ListByEntity returns edges where the given (id, type) is either the
// from or the to endpoint, oldest first.
func (r EntityRelationshipRepository) ListByEntity(ctx context.Context, entityID, entityType string) ([]domain.EntityRelationship, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT id, kind, from_id, from_type, to_id, to_type, created_at, created_by
FROM entity_relationships
WHERE (from_id = ? AND from_type = ?) OR (to_id = ? AND to_type = ?)
ORDER BY created_at ASC`,
		entityID, entityType, entityID, entityType,
	)
	if err != nil {
		return nil, fmt.Errorf("list entity_relationships by entity: %w", err)
	}
	defer closeRows(rows)
	return collectEntityRelationshipRows(rows)
}

// ListByKind returns edges with the given kind, oldest first.
func (r EntityRelationshipRepository) ListByKind(ctx context.Context, kind string) ([]domain.EntityRelationship, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT id, kind, from_id, from_type, to_id, to_type, created_at, created_by
FROM entity_relationships WHERE kind = ? ORDER BY created_at ASC`, kind)
	if err != nil {
		return nil, fmt.Errorf("list entity_relationships by kind: %w", err)
	}
	defer closeRows(rows)
	return collectEntityRelationshipRows(rows)
}

// ListAll returns every edge.
func (r EntityRelationshipRepository) ListAll(ctx context.Context) ([]domain.EntityRelationship, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT id, kind, from_id, from_type, to_id, to_type, created_at, created_by
FROM entity_relationships ORDER BY created_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("list entity_relationships: %w", err)
	}
	defer closeRows(rows)
	return collectEntityRelationshipRows(rows)
}

// CountAll returns the total number of edges.
func (r EntityRelationshipRepository) CountAll(ctx context.Context) (int64, error) {
	var n int64
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM entity_relationships`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count entity_relationships: %w", err)
	}
	return n, nil
}

// DeleteAll wipes every entity_relationships row.
func (r EntityRelationshipRepository) DeleteAll(ctx context.Context) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM entity_relationships`); err != nil {
		return fmt.Errorf("delete all entity_relationships: %w", err)
	}
	return nil
}

func collectEntityRelationshipRows(rows *sql.Rows) ([]domain.EntityRelationship, error) {
	out := make([]domain.EntityRelationship, 0)
	for rows.Next() {
		var e domain.EntityRelationship
		var kind, createdAt string
		if err := rows.Scan(&e.ID, &kind, &e.FromID, &e.FromType, &e.ToID, &e.ToType, &createdAt, &e.CreatedBy); err != nil {
			return nil, err
		}
		e.Kind = domain.RelationshipType(kind)
		if t, perr := time.Parse(time.RFC3339Nano, createdAt); perr == nil {
			e.CreatedAt = t
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

var _ = strings.ToLower // keep strings import non-empty for future filters
