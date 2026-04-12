package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/felixgeelhaar/mnemos/internal/embedding"
	"github.com/felixgeelhaar/mnemos/internal/ports"
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
func (r EmbeddingRepository) Upsert(entityID, entityType string, vector []float32, model string) error {
	blob := embedding.EncodeVector(vector)
	return r.q.UpsertEmbedding(context.Background(), sqlcgen.UpsertEmbeddingParams{
		EntityID:   entityID,
		EntityType: entityType,
		Vector:     blob,
		Model:      model,
		Dimensions: int64(len(vector)),
		CreatedAt:  time.Now().UTC().Format(time.RFC3339Nano),
	})
}

// GetByEntityID retrieves a single embedding by entity ID and type.
func (r EmbeddingRepository) GetByEntityID(entityID, entityType string) (ports.EmbeddingRecord, error) {
	row, err := r.q.GetEmbeddingByEntityID(context.Background(), sqlcgen.GetEmbeddingByEntityIDParams{
		EntityID:   entityID,
		EntityType: entityType,
	})
	if err != nil {
		if err == sql.ErrNoRows {
			return ports.EmbeddingRecord{}, fmt.Errorf("embedding not found for %s/%s", entityID, entityType)
		}
		return ports.EmbeddingRecord{}, fmt.Errorf("get embedding: %w", err)
	}
	return mapSQLEmbedding(row)
}

// ListByEntityType returns all embeddings of the given type (e.g. "event").
func (r EmbeddingRepository) ListByEntityType(entityType string) ([]ports.EmbeddingRecord, error) {
	rows, err := r.q.ListEmbeddingsByEntityType(context.Background(), entityType)
	if err != nil {
		return nil, fmt.Errorf("list embeddings by type: %w", err)
	}

	records := make([]ports.EmbeddingRecord, 0, len(rows))
	for _, row := range rows {
		rec, err := mapSQLEmbedding(row)
		if err != nil {
			return nil, err
		}
		records = append(records, rec)
	}
	return records, nil
}

func mapSQLEmbedding(row sqlcgen.Embedding) (ports.EmbeddingRecord, error) {
	vector, err := embedding.DecodeVector(row.Vector)
	if err != nil {
		return ports.EmbeddingRecord{}, fmt.Errorf("decode embedding vector: %w", err)
	}
	createdAt, err := time.Parse(time.RFC3339Nano, row.CreatedAt)
	if err != nil {
		return ports.EmbeddingRecord{}, fmt.Errorf("parse embedding created_at: %w", err)
	}
	_ = createdAt // stored for future use
	return ports.EmbeddingRecord{
		EntityID:   row.EntityID,
		EntityType: row.EntityType,
		Vector:     vector,
		Model:      row.Model,
		Dimensions: int(row.Dimensions),
	}, nil
}
