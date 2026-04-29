package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/felixgeelhaar/mnemos/internal/domain"
	"github.com/felixgeelhaar/mnemos/internal/embedding"
	"github.com/felixgeelhaar/mnemos/internal/store/sqlite/sqlcgen"
)

// EmbeddingRepository provides SQLite-backed storage for vector embeddings.
type EmbeddingRepository struct {
	db *sql.DB
	q  *sqlcgen.Queries
}

// NewEmbeddingRepository returns an EmbeddingRepository backed by the given database.
func NewEmbeddingRepository(db *sql.DB) EmbeddingRepository {
	return EmbeddingRepository{db: db, q: sqlcgen.New(db)}
}

// Upsert stores or updates a vector embedding for the given entity.
// An empty createdBy is recorded as domain.SystemUser via actorOr.
func (r EmbeddingRepository) Upsert(ctx context.Context, entityID, entityType string, vector []float32, model, createdBy string) error {
	blob := embedding.EncodeVector(vector)
	return r.q.UpsertEmbedding(ctx, sqlcgen.UpsertEmbeddingParams{
		EntityID:   entityID,
		EntityType: entityType,
		Vector:     blob,
		Model:      model,
		Dimensions: int64(len(vector)),
		CreatedAt:  time.Now().UTC().Format(time.RFC3339Nano),
		CreatedBy:  actorOr(createdBy),
	})
}

// GetByEntityID retrieves a single embedding by entity ID and type.
func (r EmbeddingRepository) GetByEntityID(ctx context.Context, entityID, entityType string) (domain.EmbeddingRecord, error) {
	row, err := r.q.GetEmbeddingByEntityID(ctx, sqlcgen.GetEmbeddingByEntityIDParams{
		EntityID:   entityID,
		EntityType: entityType,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.EmbeddingRecord{}, fmt.Errorf("embedding not found for %s/%s", entityID, entityType)
		}
		return domain.EmbeddingRecord{}, fmt.Errorf("get embedding: %w", err)
	}
	return mapSQLEmbedding(row)
}

// Delete removes the embedding for (entityID, entityType). Idempotent.
func (r EmbeddingRepository) Delete(ctx context.Context, entityID, entityType string) error {
	return r.q.DeleteEmbeddingByEntity(ctx, sqlcgen.DeleteEmbeddingByEntityParams{
		EntityID:   entityID,
		EntityType: entityType,
	})
}

// ListByEntityType returns all embeddings of the given type (e.g. "event").
func (r EmbeddingRepository) ListByEntityType(ctx context.Context, entityType string) ([]domain.EmbeddingRecord, error) {
	rows, err := r.q.ListEmbeddingsByEntityType(ctx, entityType)
	if err != nil {
		return nil, fmt.Errorf("list embeddings by type: %w", err)
	}

	records := make([]domain.EmbeddingRecord, 0, len(rows))
	for _, row := range rows {
		rec, err := mapSQLEmbedding(row)
		if err != nil {
			return nil, err
		}
		records = append(records, rec)
	}
	return records, nil
}

// CountAll returns the total number of embedding rows stored.
func (r EmbeddingRepository) CountAll(ctx context.Context) (int64, error) {
	var n int64
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM embeddings`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count embeddings: %w", err)
	}
	return n, nil
}

// DeleteAll wipes the embeddings table.
func (r EmbeddingRepository) DeleteAll(ctx context.Context) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM embeddings`); err != nil {
		return fmt.Errorf("delete all embeddings: %w", err)
	}
	return nil
}

// ListAll returns every embedding row, ordered by created_at ascending.
func (r EmbeddingRepository) ListAll(ctx context.Context) ([]domain.EmbeddingRecord, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT entity_id, entity_type, vector, model, dimensions, created_at, created_by
		 FROM embeddings
		 ORDER BY created_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("list all embeddings: %w", err)
	}
	defer closeRows(rows)

	out := make([]domain.EmbeddingRecord, 0)
	for rows.Next() {
		var (
			entityID, entityType, model, createdAt, createdBy string
			blob                                              []byte
			dims                                              int64
		)
		if err := rows.Scan(&entityID, &entityType, &blob, &model, &dims, &createdAt, &createdBy); err != nil {
			return nil, fmt.Errorf("scan embedding row: %w", err)
		}
		vec, err := embedding.DecodeVector(blob)
		if err != nil {
			return nil, fmt.Errorf("decode embedding vector for %s/%s: %w", entityID, entityType, err)
		}
		t, err := time.Parse(time.RFC3339Nano, createdAt)
		if err != nil {
			return nil, fmt.Errorf("parse embedding created_at: %w", err)
		}
		out = append(out, domain.EmbeddingRecord{
			EntityID:   entityID,
			EntityType: entityType,
			Vector:     vec,
			Model:      model,
			Dimensions: int(dims),
			CreatedAt:  t,
			CreatedBy:  createdBy,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate embedding rows: %w", err)
	}
	return out, nil
}

func mapSQLEmbedding(row sqlcgen.Embedding) (domain.EmbeddingRecord, error) {
	vector, err := embedding.DecodeVector(row.Vector)
	if err != nil {
		return domain.EmbeddingRecord{}, fmt.Errorf("decode embedding vector: %w", err)
	}
	createdAt, err := time.Parse(time.RFC3339Nano, row.CreatedAt)
	if err != nil {
		return domain.EmbeddingRecord{}, fmt.Errorf("parse embedding created_at: %w", err)
	}
	return domain.EmbeddingRecord{
		EntityID:   row.EntityID,
		EntityType: row.EntityType,
		Vector:     vector,
		Model:      row.Model,
		Dimensions: int(row.Dimensions),
		CreatedAt:  createdAt,
		CreatedBy:  row.CreatedBy,
	}, nil
}
