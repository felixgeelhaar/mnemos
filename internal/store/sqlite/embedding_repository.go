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
	q *sqlcgen.Queries
}

// NewEmbeddingRepository returns an EmbeddingRepository backed by the given database.
func NewEmbeddingRepository(db *sql.DB) EmbeddingRepository {
	return EmbeddingRepository{q: sqlcgen.New(db)}
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

func mapSQLEmbedding(row sqlcgen.Embedding) (domain.EmbeddingRecord, error) {
	vector, err := embedding.DecodeVector(row.Vector)
	if err != nil {
		return domain.EmbeddingRecord{}, fmt.Errorf("decode embedding vector: %w", err)
	}
	createdAt, err := time.Parse(time.RFC3339Nano, row.CreatedAt)
	if err != nil {
		return domain.EmbeddingRecord{}, fmt.Errorf("parse embedding created_at: %w", err)
	}
	_ = createdAt // stored for future use
	return domain.EmbeddingRecord{
		EntityID:   row.EntityID,
		EntityType: row.EntityType,
		Vector:     vector,
		Model:      row.Model,
		Dimensions: int(row.Dimensions),
		CreatedBy:  row.CreatedBy,
	}, nil
}
