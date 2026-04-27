package memory

import (
	"context"
	"time"

	"github.com/felixgeelhaar/mnemos/internal/domain"
)

// EmbeddingRepository is the in-memory implementation of
// [ports.EmbeddingRepository]. Vectors are stored as []float32 — no
// little-endian BLOB encoding needed because we never round-trip
// through SQL.
type EmbeddingRepository struct {
	state *state
}

// Upsert stores or replaces the embedding for (entityID, entityType).
// Falls back to SystemUser as creator since [ports.EmbeddingRepository]
// does not surface an actor parameter.
func (r EmbeddingRepository) Upsert(_ context.Context, entityID, entityType string, vector []float32, model string) error {
	r.state.mu.Lock()
	defer r.state.mu.Unlock()
	r.state.embeddings[embeddingKey{EntityID: entityID, EntityType: entityType}] = storedEmbedding{
		EntityID:   entityID,
		EntityType: entityType,
		Vector:     copyFloat32Slice(vector),
		Model:      model,
		Dimensions: len(vector),
		CreatedAt:  time.Now().UTC(),
		CreatedBy:  domain.SystemUser,
	}
	return nil
}

// ListByEntityType returns every stored embedding whose entity type
// matches.
func (r EmbeddingRepository) ListByEntityType(_ context.Context, entityType string) ([]domain.EmbeddingRecord, error) {
	r.state.mu.RLock()
	defer r.state.mu.RUnlock()
	out := make([]domain.EmbeddingRecord, 0)
	for k, v := range r.state.embeddings {
		if k.EntityType != entityType {
			continue
		}
		out = append(out, domain.EmbeddingRecord{
			EntityID:   v.EntityID,
			EntityType: v.EntityType,
			Vector:     copyFloat32Slice(v.Vector),
			Model:      v.Model,
			Dimensions: v.Dimensions,
			CreatedBy:  v.CreatedBy,
		})
	}
	return out, nil
}
