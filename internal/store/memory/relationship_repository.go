package memory

import (
	"context"
	"fmt"

	"github.com/felixgeelhaar/mnemos/internal/domain"
)

// RelationshipRepository is the in-memory implementation of
// [ports.RelationshipRepository]. The (id) is the dedup key — re-
// upserting the same id replaces the existing row, matching SQLite's
// ON CONFLICT(id) DO UPDATE semantics.
type RelationshipRepository struct {
	state *state
}

// Upsert validates and stores the relationships. Self-references are
// rejected at the domain layer via Relationship.Validate, and that
// check is repeated here for storage-boundary safety.
func (r RelationshipRepository) Upsert(_ context.Context, relationships []domain.Relationship) error {
	if len(relationships) == 0 {
		return nil
	}
	r.state.mu.Lock()
	defer r.state.mu.Unlock()
	for _, rel := range relationships {
		if err := rel.Validate(); err != nil {
			return fmt.Errorf("invalid relationship %s: %w", rel.ID, err)
		}
		r.state.relationships[rel.ID] = storedRelationship{
			ID:          rel.ID,
			Type:        rel.Type,
			FromClaimID: rel.FromClaimID,
			ToClaimID:   rel.ToClaimID,
			CreatedAt:   rel.CreatedAt.UTC(),
			CreatedBy:   actorOr(rel.CreatedBy),
		}
	}
	return nil
}

// ListByClaim returns every relationship that touches the given claim
// (as source or target).
func (r RelationshipRepository) ListByClaim(_ context.Context, claimID string) ([]domain.Relationship, error) {
	r.state.mu.RLock()
	defer r.state.mu.RUnlock()
	out := make([]domain.Relationship, 0)
	for _, rel := range r.state.relationships {
		if rel.FromClaimID == claimID || rel.ToClaimID == claimID {
			out = append(out, rel.toDomain())
		}
	}
	return out, nil
}

// ListByClaimIDs returns every relationship touching any of the given
// claim ids. Used by hop-expansion in the query engine.
func (r RelationshipRepository) ListByClaimIDs(_ context.Context, claimIDs []string) ([]domain.Relationship, error) {
	if len(claimIDs) == 0 {
		return []domain.Relationship{}, nil
	}
	wanted := make(map[string]struct{}, len(claimIDs))
	for _, id := range claimIDs {
		wanted[id] = struct{}{}
	}
	r.state.mu.RLock()
	defer r.state.mu.RUnlock()
	out := make([]domain.Relationship, 0)
	for _, rel := range r.state.relationships {
		_, fromHit := wanted[rel.FromClaimID]
		_, toHit := wanted[rel.ToClaimID]
		if fromHit || toHit {
			out = append(out, rel.toDomain())
		}
	}
	return out, nil
}

func (s storedRelationship) toDomain() domain.Relationship {
	return domain.Relationship{
		ID:          s.ID,
		Type:        s.Type,
		FromClaimID: s.FromClaimID,
		ToClaimID:   s.ToClaimID,
		CreatedAt:   s.CreatedAt,
		CreatedBy:   s.CreatedBy,
	}
}
