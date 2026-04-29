package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/felixgeelhaar/mnemos/internal/domain"
	"github.com/felixgeelhaar/mnemos/internal/embedding"
)

// EmbeddingRepository persists vector embeddings as LONGBLOB using
// the same little-endian float32 encoding as SQLite/Postgres so
// federation push/pull stays byte-compatible across backends.
type EmbeddingRepository struct {
	db *sql.DB
}

// Upsert stores or replaces an embedding for (entityID, entityType).
func (r EmbeddingRepository) Upsert(ctx context.Context, entityID, entityType string, vector []float32, model, createdBy string) error {
	blob := embedding.EncodeVector(vector)
	_, err := r.db.ExecContext(ctx, `
INSERT INTO embeddings (entity_id, entity_type, vector, model, dimensions, created_at, created_by)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
  vector = VALUES(vector),
  model = VALUES(model),
  dimensions = VALUES(dimensions),
  created_at = VALUES(created_at)`,
		entityID, entityType, blob, model, len(vector),
		time.Now().UTC(), actorOr(createdBy),
	)
	if err != nil {
		return fmt.Errorf("upsert embedding: %w", err)
	}
	return nil
}

// Delete removes the embedding for (entityID, entityType). Idempotent.
func (r EmbeddingRepository) Delete(ctx context.Context, entityID, entityType string) error {
	if _, err := r.db.ExecContext(ctx,
		`DELETE FROM embeddings WHERE entity_id = ? AND entity_type = ?`,
		entityID, entityType,
	); err != nil {
		return fmt.Errorf("delete embedding (%s, %s): %w", entityID, entityType, err)
	}
	return nil
}

// ListByEntityType returns every embedding whose type matches.
func (r EmbeddingRepository) ListByEntityType(ctx context.Context, entityType string) ([]domain.EmbeddingRecord, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT entity_id, entity_type, vector, model, dimensions, created_at, created_by
FROM embeddings WHERE entity_type = ?`, entityType)
	if err != nil {
		return nil, fmt.Errorf("list embeddings by type: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return collectEmbeddingRows(rows)
}

// CountAll returns the total number of embedding rows stored.
func (r EmbeddingRepository) CountAll(ctx context.Context) (int64, error) {
	var n int64
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM embeddings`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count embeddings: %w", err)
	}
	return n, nil
}

// ListAll returns every embedding row ordered by created_at ascending.
func (r EmbeddingRepository) ListAll(ctx context.Context) ([]domain.EmbeddingRecord, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT entity_id, entity_type, vector, model, dimensions, created_at, created_by
FROM embeddings ORDER BY created_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("list all embeddings: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return collectEmbeddingRows(rows)
}

func collectEmbeddingRows(rows *sql.Rows) ([]domain.EmbeddingRecord, error) {
	out := make([]domain.EmbeddingRecord, 0)
	for rows.Next() {
		var rec domain.EmbeddingRecord
		var vec []byte
		var dims int64
		if err := rows.Scan(&rec.EntityID, &rec.EntityType, &vec, &rec.Model, &dims, &rec.CreatedAt, &rec.CreatedBy); err != nil {
			return nil, fmt.Errorf("scan embedding row: %w", err)
		}
		v, err := embedding.DecodeVector(vec)
		if err != nil {
			return nil, fmt.Errorf("decode embedding: %w", err)
		}
		rec.Vector = v
		rec.Dimensions = int(dims)
		out = append(out, rec)
	}
	return out, rows.Err()
}
