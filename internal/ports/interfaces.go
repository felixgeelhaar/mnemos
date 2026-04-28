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
	ListAll(ctx context.Context) ([]domain.Event, error)
	ListByRunID(ctx context.Context, runID string) ([]domain.Event, error)
}

// ClaimRepository persists and retrieves extracted claims.
//
// The interface is the union of methods callers across cmd/mnemos and
// internal/pipeline reach for. Optional capabilities (full-text search,
// trust scoring) are factored into separate interfaces below so a
// backend that lacks them is still a valid ClaimRepository.
type ClaimRepository interface {
	Upsert(ctx context.Context, claims []domain.Claim) error
	UpsertWithReason(ctx context.Context, claims []domain.Claim, reason string) error
	UpsertWithReasonAs(ctx context.Context, claims []domain.Claim, reason, changedBy string) error
	UpsertEvidence(ctx context.Context, links []domain.ClaimEvidence) error
	ListByEventIDs(ctx context.Context, eventIDs []string) ([]domain.Claim, error)
	ListEvidenceByClaimIDs(ctx context.Context, claimIDs []string) ([]domain.ClaimEvidence, error)
	ListByIDs(ctx context.Context, claimIDs []string) ([]domain.Claim, error)
	ListAll(ctx context.Context) ([]domain.Claim, error)
	ListStatusHistoryByClaimID(ctx context.Context, claimID string) ([]domain.ClaimStatusTransition, error)
	SetValidity(ctx context.Context, claimID string, validTo time.Time) error

	// RepointEvidence rewrites every claim_evidence row pointing at
	// fromClaimID to point at toClaimID instead, then deletes the
	// original rows. Idempotent on the (claim_id, event_id) dedup
	// key — duplicate evidence collapses silently. Used by
	// pipeline.ApplySemanticDedupe.
	RepointEvidence(ctx context.Context, fromClaimID, toClaimID string) error

	// DeleteCascade removes a claim and its dependent rows that are
	// owned by the claim alone (claim_evidence by claim_id,
	// claim_status_history by claim_id, the claim row itself). Rows
	// owned by other entities (relationships, embeddings, claim
	// entity links) must be cleaned up by the caller via the
	// relevant repositories — DeleteCascade does not reach across
	// repository boundaries.
	DeleteCascade(ctx context.Context, claimID string) error
}

// TrustScorer is the optional capability to recompute and aggregate
// trust scores. Backends that don't track trust (e.g. a thin in-memory
// fixture) are still valid ClaimRepositories — callers type-assert
// before invoking these methods.
type TrustScorer interface {
	RecomputeTrust(ctx context.Context, score func(confidence float64, evidenceCount int, latestEvidence time.Time) float64) (int, error)
	AverageTrust(ctx context.Context) (float64, error)
	CountClaimsBelowTrust(ctx context.Context, threshold float64) (int64, error)
}

// RelationshipRepository persists and retrieves relationships between claims.
type RelationshipRepository interface {
	Upsert(ctx context.Context, relationships []domain.Relationship) error
	ListByClaim(ctx context.Context, claimID string) ([]domain.Relationship, error)
	ListByClaimIDs(ctx context.Context, claimIDs []string) ([]domain.Relationship, error)

	// RepointEndpoint rewrites every relationship whose from_claim_id
	// or to_claim_id equals oldID so that endpoint becomes newID.
	// Self-loops created by the rewrite (newID = newID) are dropped,
	// and unique-edge conflicts collapse silently — Mnemos doesn't
	// distinguish duplicate edges. Used by ApplySemanticDedupe to
	// fold a duplicate claim's edges onto its winner.
	RepointEndpoint(ctx context.Context, oldID, newID string) error

	// DeleteByClaim removes every relationship that touches the
	// given claim (as source OR target). Used to clean up a claim's
	// edges before the claim itself is deleted.
	DeleteByClaim(ctx context.Context, claimID string) error
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
	// Upsert stores or replaces a vector for (entityID, entityType).
	// createdBy stamps the row's actor; pass "" to fall back to
	// domain.SystemUser at the storage boundary.
	Upsert(ctx context.Context, entityID, entityType string, vector []float32, model, createdBy string) error
	ListByEntityType(ctx context.Context, entityType string) ([]domain.EmbeddingRecord, error)

	// Delete removes the embedding row for the given entity. Idempotent
	// — deleting a non-existent embedding is a no-op. Used by
	// pipeline.ApplySemanticDedupe to drop a duplicate claim's
	// vector before deleting the claim itself.
	Delete(ctx context.Context, entityID, entityType string) error
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

// CompilationJobRepository persists workflow job state. The runner
// in internal/workflow drives the lifecycle; this interface is just
// the storage seam.
type CompilationJobRepository interface {
	Upsert(ctx context.Context, job domain.CompilationJob) error
	GetByID(ctx context.Context, id string) (domain.CompilationJob, error)
}

// EntityRepository persists canonicalised entities and the
// claim_entities link table. The interface mirrors the SQLite
// implementation's public surface so cmd/mnemos and internal/pipeline
// can drop their named SQLite import.
//
// Implementations must enforce the UNIQUE(normalized_name, type)
// dedup contract: FindOrCreate is the only sanctioned write path
// for new entities and is expected to be idempotent under contention.
type EntityRepository interface {
	FindOrCreate(ctx context.Context, name string, etype domain.EntityType, createdBy string) (domain.Entity, error)
	LinkClaim(ctx context.Context, claimID, entityID, role string) error
	List(ctx context.Context) ([]domain.Entity, error)
	ListByType(ctx context.Context, etype domain.EntityType) ([]domain.Entity, error)
	FindByName(ctx context.Context, name string) (domain.Entity, bool, error)
	ListClaimsForEntity(ctx context.Context, entityID string) ([]domain.Claim, error)
	ListEntitiesForClaim(ctx context.Context, claimID string) ([]domain.Entity, []string, error)
	Merge(ctx context.Context, winnerID, loserID string) error
	Count(ctx context.Context) (int64, error)
	ClaimIDsMissingEntityLinks(ctx context.Context) ([]string, error)
}
