package ports

import (
	"context"
	"time"

	"github.com/felixgeelhaar/mnemos/internal/domain"
)

// EventRepository persists and retrieves domain events.
type EventRepository interface {
	Append(ctx context.Context, event domain.Event) error
	GetByID(ctx context.Context, id string) (domain.Event, error)
	ListByIDs(ctx context.Context, ids []string) ([]domain.Event, error)
}

// ClaimRepository persists and retrieves extracted claims.
type ClaimRepository interface {
	Upsert(ctx context.Context, claims []domain.Claim) error
	ListByEventIDs(ctx context.Context, eventIDs []string) ([]domain.Claim, error)
	ListEvidenceByClaimIDs(ctx context.Context, claimIDs []string) ([]domain.ClaimEvidence, error)
	ListByIDs(ctx context.Context, claimIDs []string) ([]domain.Claim, error)
	ListStatusHistoryByClaimID(ctx context.Context, claimID string) ([]domain.ClaimStatusTransition, error)
}

// RelationshipRepository persists and retrieves relationships between claims.
type RelationshipRepository interface {
	Upsert(ctx context.Context, relationships []domain.Relationship) error
	ListByClaim(ctx context.Context, claimID string) ([]domain.Relationship, error)
	ListByClaimIDs(ctx context.Context, claimIDs []string) ([]domain.Relationship, error)
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
	Upsert(ctx context.Context, entityID, entityType string, vector []float32, model string) error
	ListByEntityType(ctx context.Context, entityType string) ([]domain.EmbeddingRecord, error)
}

// TextHit is one row of a keyword search: the matched row's id and a
// positive relevance score (higher is better). Returned by
// TextSearcher implementations so the query engine can rank without
// caring whether the underlying index is FTS5, Lucene, or anything
// else.
type TextHit struct {
	ID    string
	Score float64
}

// TextSearcher exposes a keyword (BM25-style) search index over a
// table of text rows. Optional capability: the query engine type-
// asserts on this and falls back to cosine + token-overlap when the
// repository doesn't implement it (older test doubles, in-memory
// fakes, etc.).
type TextSearcher interface {
	SearchByText(ctx context.Context, query string, limit int) ([]TextHit, error)
}

// UserRepository persists and retrieves user identities.
type UserRepository interface {
	Create(ctx context.Context, user domain.User) error
	GetByID(ctx context.Context, id string) (domain.User, error)
	GetByEmail(ctx context.Context, email string) (domain.User, error)
	List(ctx context.Context) ([]domain.User, error)
	UpdateStatus(ctx context.Context, id string, status domain.UserStatus) error
	UpdateScopes(ctx context.Context, id string, scopes []string) error
}

// RevokedTokenRepository persists and queries the JWT denylist.
type RevokedTokenRepository interface {
	Add(ctx context.Context, token domain.RevokedToken) error
	IsRevoked(ctx context.Context, jti string) (bool, error)
	PurgeExpired(ctx context.Context, before time.Time) (int, error)
}

// AgentRepository persists and retrieves non-human principals.
type AgentRepository interface {
	Create(ctx context.Context, agent domain.Agent) error
	GetByID(ctx context.Context, id string) (domain.Agent, error)
	List(ctx context.Context) ([]domain.Agent, error)
	UpdateStatus(ctx context.Context, id string, status domain.AgentStatus) error
	UpdateScopes(ctx context.Context, id string, scopes []string) error
	UpdateAllowedRuns(ctx context.Context, id string, runs []string) error
}
