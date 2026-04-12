package ports

import "github.com/felixgeelhaar/mnemos/internal/domain"

// EventRepository persists and retrieves domain events.
type EventRepository interface {
	Append(domain.Event) error
	GetByID(string) (domain.Event, error)
	ListByIDs([]string) ([]domain.Event, error)
}

// ClaimRepository persists and retrieves extracted claims.
type ClaimRepository interface {
	Upsert([]domain.Claim) error
	ListByEventIDs([]string) ([]domain.Claim, error)
}

// RelationshipRepository persists and retrieves relationships between claims.
type RelationshipRepository interface {
	Upsert([]domain.Relationship) error
	ListByClaim(string) ([]domain.Relationship, error)
}

// ExtractionEngine extracts structured claims from domain events.
type ExtractionEngine interface {
	ExtractClaims([]domain.Event) ([]domain.Claim, error)
}

// QueryEngine answers natural-language queries against the knowledge base.
type QueryEngine interface {
	Answer(query string) (domain.Answer, error)
}

// EmbeddingRepository persists and retrieves vector embeddings.
type EmbeddingRepository interface {
	Upsert(entityID, entityType string, vector []float32, model string) error
	ListByEntityType(entityType string) ([]EmbeddingRecord, error)
}

// EmbeddingRecord holds a stored embedding with its metadata.
type EmbeddingRecord struct {
	EntityID   string
	EntityType string
	Vector     []float32
	Model      string
	Dimensions int
}
