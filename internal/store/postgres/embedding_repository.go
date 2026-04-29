package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/felixgeelhaar/mnemos/internal/domain"
	"github.com/felixgeelhaar/mnemos/internal/embedding"
)

// EmbeddingRepository persists vector embeddings as bytea, using the
// same little-endian float32 encoding as the SQLite backend
// (internal/embedding.EncodeVector / DecodeVector). This keeps
// federation push/pull byte-compatible across providers.
type EmbeddingRepository struct {
	db *sql.DB
	ns string
}

// Upsert satisfies the corresponding ports method.
func (r EmbeddingRepository) Upsert(ctx context.Context, entityID, entityType string, vector []float32, model, createdBy string) error {
	blob := embedding.EncodeVector(vector)
	_, err := r.db.ExecContext(ctx, fmt.Sprintf(`
INSERT INTO %s (entity_id, entity_type, vector, model, dimensions, created_at, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (entity_id, entity_type) DO UPDATE SET
  vector = EXCLUDED.vector,
  model = EXCLUDED.model,
  dimensions = EXCLUDED.dimensions,
  created_at = EXCLUDED.created_at`, qualify(r.ns, "embeddings")),
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
		fmt.Sprintf(`DELETE FROM %s WHERE entity_id = $1 AND entity_type = $2`, qualify(r.ns, "embeddings")),
		entityID, entityType,
	); err != nil {
		return fmt.Errorf("delete embedding (%s, %s): %w", entityID, entityType, err)
	}
	return nil
}

// ListByEntityType satisfies the corresponding ports method.
func (r EmbeddingRepository) ListByEntityType(ctx context.Context, entityType string) ([]domain.EmbeddingRecord, error) {
	rows, err := r.db.QueryContext(ctx, fmt.Sprintf(`
SELECT entity_id, entity_type, vector, model, dimensions, created_at, created_by
FROM %s WHERE entity_type = $1`, qualify(r.ns, "embeddings")), entityType)
	if err != nil {
		return nil, fmt.Errorf("list embeddings by type: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return collectEmbeddingRows(rows)
}

// CountAll satisfies the corresponding ports method.
func (r EmbeddingRepository) CountAll(ctx context.Context) (int64, error) {
	var n int64
	if err := r.db.QueryRowContext(ctx, fmt.Sprintf(
		`SELECT COUNT(*) FROM %s`, qualify(r.ns, "embeddings"),
	)).Scan(&n); err != nil {
		return 0, fmt.Errorf("count embeddings: %w", err)
	}
	return n, nil
}

// DeleteAll satisfies the corresponding ports method.
func (r EmbeddingRepository) DeleteAll(ctx context.Context) error {
	if _, err := r.db.ExecContext(ctx, fmt.Sprintf(
		`DELETE FROM %s`, qualify(r.ns, "embeddings"),
	)); err != nil {
		return fmt.Errorf("delete all embeddings: %w", err)
	}
	return nil
}

// ListAll satisfies the corresponding ports method.
func (r EmbeddingRepository) ListAll(ctx context.Context) ([]domain.EmbeddingRecord, error) {
	rows, err := r.db.QueryContext(ctx, fmt.Sprintf(`
SELECT entity_id, entity_type, vector, model, dimensions, created_at, created_by
FROM %s ORDER BY created_at ASC`, qualify(r.ns, "embeddings")))
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
