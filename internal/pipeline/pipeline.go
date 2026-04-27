// Package pipeline provides shared orchestration logic used by both the CLI and MCP server
// entrypoints: extraction engine setup, artifact persistence, and embedding generation.
package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/felixgeelhaar/fortify/retry"
	"github.com/felixgeelhaar/mnemos/internal/domain"
	"github.com/felixgeelhaar/mnemos/internal/embedding"
	"github.com/felixgeelhaar/mnemos/internal/extract"
	"github.com/felixgeelhaar/mnemos/internal/llm"
	"github.com/felixgeelhaar/mnemos/internal/ports"
	"github.com/felixgeelhaar/mnemos/internal/store"
	"github.com/felixgeelhaar/mnemos/internal/trust"
)

// Extractor wraps either the rule-based or LLM-powered extraction engine,
// presenting a uniform interface to command handlers. The entity map is
// keyed by claim id and may be nil when the rule-based fallback runs
// (rule-based extraction does not tag entities). Callers should treat a
// nil map as "no entities to materialise", not as an error.
type Extractor struct {
	ExtractFn func([]domain.Event) ([]domain.Claim, []domain.ClaimEvidence, map[string][]extract.ExtractedEntity, error)
}

// NewExtractor builds the appropriate extraction engine based on useLLM.
// When useLLM is true, it reads provider config from environment variables
// (MNEMOS_LLM_PROVIDER, MNEMOS_LLM_API_KEY, etc.) and falls back to the
// rule-based engine on LLM failure.
func NewExtractor(useLLM bool) (*Extractor, error) {
	if !useLLM {
		engine := extract.NewEngine()
		// Rule-based extraction doesn't tag entities — return nil for
		// the entity map so the pipeline knows there's nothing to
		// materialise.
		return &Extractor{ExtractFn: func(events []domain.Event) ([]domain.Claim, []domain.ClaimEvidence, map[string][]extract.ExtractedEntity, error) {
			c, l, err := engine.Extract(events)
			return c, l, nil, err
		}}, nil
	}

	cfg, err := llm.ConfigFromEnv()
	if err != nil {
		return nil, fmt.Errorf("LLM configuration error: %s\n  Set MNEMOS_LLM_PROVIDER and MNEMOS_LLM_API_KEY environment variables\n  Providers: anthropic, openai, gemini, ollama, openai-compat", err)
	}

	// Optional per-stage model override. Lets users pair a strong model
	// for extraction with a smaller/faster model elsewhere without
	// editing MNEMOS_LLM_MODEL. Falls back silently to the base config.
	if override := os.Getenv("MNEMOS_EXTRACT_MODEL"); override != "" {
		cfg.Model = override
	}

	client, err := llm.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create LLM client: %w", err)
	}

	engine := extract.NewLLMEngine(client)
	return &Extractor{ExtractFn: engine.ExtractWithEntities}, nil
}

// PersistArtifacts writes events, claims, evidence links, and
// relationships through the port-typed repositories on conn.
//
// Backend-agnostic. The previous SQLite-specific implementation
// wrapped every write in a single cross-table transaction; that
// guarantee is replaced by per-repository idempotent upserts so
// memory:// and future Postgres backends share the same code path.
// A retry of a partially-applied batch converges because every
// underlying write is idempotent on its identity key (events are
// INSERT … ON CONFLICT DO NOTHING; claims, evidence, relationships
// are UPSERTs).
//
// claim status_history attribution is preserved by grouping claims
// by CreatedBy so each transition row carries the right changed_by.
//
// Trust scoring runs only when the backend's ClaimRepository also
// implements ports.TrustScorer (SQLite does; in-memory test fakes
// and minimal embeds may not). Failure here remains non-fatal in
// spirit but is surfaced to the caller for visibility.
func PersistArtifacts(ctx context.Context, conn *store.Conn, events []domain.Event, claims []domain.Claim, links []domain.ClaimEvidence, relationships []domain.Relationship) error {
	if conn == nil {
		return fmt.Errorf("persist artifacts: nil conn")
	}

	for _, event := range events {
		if err := conn.Events.Append(ctx, event); err != nil {
			return fmt.Errorf("append event %s: %w", event.ID, err)
		}
	}

	// Pre-compute earliest evidence-event timestamp per claim. The
	// claim's valid_from reflects when the *fact was first observed*
	// in the source — the earliest evidence event — not when we
	// happened to extract it. For backfill / out-of-order ingest this
	// is the only defensible default.
	eventTS := make(map[string]time.Time, len(events))
	for _, ev := range events {
		eventTS[ev.ID] = ev.Timestamp
	}
	earliestEvidence := make(map[string]time.Time, len(claims))
	for _, link := range links {
		ts, ok := eventTS[link.EventID]
		if !ok {
			continue
		}
		cur, seen := earliestEvidence[link.ClaimID]
		if !seen || ts.Before(cur) {
			earliestEvidence[link.ClaimID] = ts
		}
	}

	// Apply ValidFrom inference to claim copies so we don't mutate
	// caller-owned slice elements.
	enriched := make([]domain.Claim, len(claims))
	for i, c := range claims {
		if c.ValidFrom.IsZero() {
			if ts, ok := earliestEvidence[c.ID]; ok {
				c.ValidFrom = ts
			} else {
				c.ValidFrom = c.CreatedAt
			}
		}
		enriched[i] = c
	}

	// Group by CreatedBy so each batch's status_history rows carry
	// the right changed_by — the original implementation attributed
	// per-claim, and most pipelines do produce homogeneous batches
	// (one user, one agent), so this is usually a single group.
	groups := groupClaimsByCreatedBy(enriched)
	for actor, group := range groups {
		if err := conn.Claims.UpsertWithReasonAs(ctx, group, "pipeline", actor); err != nil {
			return fmt.Errorf("upsert claims (created_by=%s): %w", actor, err)
		}
	}

	for _, link := range links {
		if err := link.Validate(); err != nil {
			return fmt.Errorf("invalid claim evidence: %w", err)
		}
	}
	if err := conn.Claims.UpsertEvidence(ctx, links); err != nil {
		return fmt.Errorf("upsert claim evidence: %w", err)
	}

	if err := conn.Relationships.Upsert(ctx, relationships); err != nil {
		return fmt.Errorf("upsert relationships: %w", err)
	}

	// Trust scoring is optional on a ClaimRepository — backends that
	// don't track trust skip silently. SQLite implements TrustScorer;
	// memory:// also does (added in Phase 2a) so this engages there too.
	if scorer, ok := conn.Claims.(ports.TrustScorer); ok {
		if _, err := scorer.RecomputeTrust(ctx, defaultTrustScorer()); err != nil {
			return fmt.Errorf("recompute trust: %w", err)
		}
	}
	return nil
}

// groupClaimsByCreatedBy buckets claims by their CreatedBy actor so
// UpsertWithReasonAs can stamp each transition row with the correct
// changed_by. Empty CreatedBy folds into domain.SystemUser to match
// the actorOr fallback used at the storage layer.
func groupClaimsByCreatedBy(claims []domain.Claim) map[string][]domain.Claim {
	groups := map[string][]domain.Claim{}
	for _, c := range claims {
		actor := c.CreatedBy
		if actor == "" {
			actor = domain.SystemUser
		}
		groups[actor] = append(groups[actor], c)
	}
	return groups
}

// defaultTrustScorer wraps internal/trust.Score with a real wall
// clock. Defined here (rather than inlined) so tests can swap in a
// fixed clock if/when we add an integration test for the persist
// → trust pipeline.
func defaultTrustScorer() func(confidence float64, evidenceCount int, latestEvidence time.Time) float64 {
	return func(confidence float64, evidenceCount int, latestEvidence time.Time) float64 {
		return trust.Score(confidence, evidenceCount, latestEvidence, time.Now().UTC())
	}
}

// MaterializeEntities walks the per-claim entity tags produced by the
// extractor and writes them through the port-typed EntityRepository.
// Idempotent: re-running over the same input is a no-op via
// FindOrCreate (entities) and the (claim, entity, role) dedup
// contract on LinkClaim.
//
// Runs after PersistArtifacts so the linked claim_id rows already
// exist. Failure here is reported to the caller; current cmd/mnemos
// callers treat it as a warning rather than aborting the whole job
// — the claims are persisted and a future `mnemos extract-entities`
// can backfill what didn't land.
func MaterializeEntities(ctx context.Context, conn *store.Conn, entities map[string][]extract.ExtractedEntity, createdBy string) (int, error) {
	if len(entities) == 0 {
		return 0, nil
	}
	if conn == nil || conn.Entities == nil {
		return 0, fmt.Errorf("materialize entities: conn missing Entities repository")
	}
	linked := 0
	for claimID, ents := range entities {
		for _, ent := range ents {
			etype := domain.EntityType(ent.Type)
			e, err := conn.Entities.FindOrCreate(ctx, ent.Name, etype, createdBy)
			if err != nil {
				return linked, fmt.Errorf("find-or-create entity %q: %w", ent.Name, err)
			}
			if err := conn.Entities.LinkClaim(ctx, claimID, e.ID, ent.Role); err != nil {
				return linked, fmt.Errorf("link claim %s -> entity %s: %w", claimID, e.ID, err)
			}
			linked++
		}
	}
	return linked, nil
}

// GenerateEmbeddings creates vector embeddings for the given events
// and stores them through conn.Embeddings. Returns the number of
// embeddings created. Failure of the embedding provider is fatal to
// the call; storage failures abort partway through (so callers see
// the count of successfully written rows on a partial-failure path).
func GenerateEmbeddings(ctx context.Context, conn *store.Conn, events []domain.Event) (int, error) {
	if conn == nil || conn.Embeddings == nil {
		return 0, fmt.Errorf("generate embeddings: conn missing Embeddings repository")
	}
	cfg, err := embedding.ConfigFromEnv()
	if err != nil {
		return 0, fmt.Errorf("embedding config: %w", err)
	}

	client, err := embedding.NewClient(cfg)
	if err != nil {
		return 0, fmt.Errorf("create embedding client: %w", err)
	}

	texts := make([]string, 0, len(events))
	for _, ev := range events {
		texts = append(texts, ev.Content)
	}

	retrier := retry.New[[][]float32](retry.Config{
		MaxAttempts:   3,
		InitialDelay:  200 * time.Millisecond,
		MaxDelay:      time.Second,
		BackoffPolicy: retry.BackoffExponential,
		Jitter:        true,
		Logger:        slog.New(slog.NewJSONHandler(os.Stderr, nil)),
	})

	vectors, err := retrier.Do(ctx, func(ctx context.Context) ([][]float32, error) {
		return client.Embed(ctx, texts)
	})
	if err != nil {
		return 0, fmt.Errorf("embed events: %w", err)
	}

	model := cfg.Model
	for i, ev := range events {
		if i >= len(vectors) {
			break
		}
		if err := conn.Embeddings.Upsert(ctx, ev.ID, "event", vectors[i], model); err != nil {
			return 0, fmt.Errorf("store embedding for event %s: %w", ev.ID, err)
		}
	}

	return len(vectors), nil
}

// GenerateClaimEmbeddings creates vector embeddings for the given
// claims and stores them through conn.Embeddings with
// entity_type="claim". Returns the number of embeddings created.
func GenerateClaimEmbeddings(ctx context.Context, conn *store.Conn, claims []domain.Claim) (int, error) {
	if len(claims) == 0 {
		return 0, nil
	}
	if conn == nil || conn.Embeddings == nil {
		return 0, fmt.Errorf("generate claim embeddings: conn missing Embeddings repository")
	}

	cfg, err := embedding.ConfigFromEnv()
	if err != nil {
		return 0, fmt.Errorf("embedding config: %w", err)
	}

	client, err := embedding.NewClient(cfg)
	if err != nil {
		return 0, fmt.Errorf("create embedding client: %w", err)
	}

	texts := make([]string, 0, len(claims))
	for _, cl := range claims {
		texts = append(texts, cl.Text)
	}

	retrier := retry.New[[][]float32](retry.Config{
		MaxAttempts:   3,
		InitialDelay:  200 * time.Millisecond,
		MaxDelay:      time.Second,
		BackoffPolicy: retry.BackoffExponential,
		Jitter:        true,
		Logger:        slog.New(slog.NewJSONHandler(os.Stderr, nil)),
	})

	vectors, err := retrier.Do(ctx, func(ctx context.Context) ([][]float32, error) {
		return client.Embed(ctx, texts)
	})
	if err != nil {
		return 0, fmt.Errorf("embed claims: %w", err)
	}

	model := cfg.Model
	for i, cl := range claims {
		if i >= len(vectors) {
			break
		}
		if err := conn.Embeddings.Upsert(ctx, cl.ID, "claim", vectors[i], model); err != nil {
			return 0, fmt.Errorf("store embedding for claim %s: %w", cl.ID, err)
		}
	}

	return len(vectors), nil
}
