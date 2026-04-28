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
func (r EmbeddingRepository) Upsert(ctx context.Context, entityID, entityType string, vector []float32, model string) error {
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
		time.Now().UTC(), domain.SystemUser,
	)
	if err != nil {
		return fmt.Errorf("upsert embedding: %w", err)
	}
	return nil
}

// ListByEntityType satisfies the corresponding ports method.
func (r EmbeddingRepository) ListByEntityType(ctx context.Context, entityType string) ([]domain.EmbeddingRecord, error) {
	rows, err := r.db.QueryContext(ctx, fmt.Sprintf(`
SELECT entity_id, entity_type, vector, model, dimensions, created_by
FROM %s WHERE entity_type = $1`, qualify(r.ns, "embeddings")), entityType)
	if err != nil {
		return nil, fmt.Errorf("list embeddings by type: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]domain.EmbeddingRecord, 0)
	for rows.Next() {
		var rec domain.EmbeddingRecord
		var vec []byte
		var dims int64
		if err := rows.Scan(&rec.EntityID, &rec.EntityType, &vec, &rec.Model, &dims, &rec.CreatedBy); err != nil {
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
