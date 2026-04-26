// Package pipeline provides shared orchestration logic used by both the CLI and MCP server
// entrypoints: extraction engine setup, artifact persistence, and embedding generation.
package pipeline

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/felixgeelhaar/fortify/retry"
	"github.com/felixgeelhaar/mnemos/internal/domain"
	"github.com/felixgeelhaar/mnemos/internal/embedding"
	"github.com/felixgeelhaar/mnemos/internal/extract"
	"github.com/felixgeelhaar/mnemos/internal/llm"
	"github.com/felixgeelhaar/mnemos/internal/store/sqlite"
	"github.com/felixgeelhaar/mnemos/internal/store/sqlite/sqlcgen"
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

// PersistArtifacts writes events, claims, evidence links, and relationships
// to the database in a single transaction.
func PersistArtifacts(ctx context.Context, db *sql.DB, events []domain.Event, claims []domain.Claim, links []domain.ClaimEvidence, relationships []domain.Relationship) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	q := sqlcgen.New(tx)

	for _, event := range events {
		metadata, err := json.Marshal(event.Metadata)
		if err != nil {
			return fmt.Errorf("marshal event metadata: %w", err)
		}
		createdBy := event.CreatedBy
		if createdBy == "" {
			createdBy = domain.SystemUser
		}
		err = q.CreateEvent(ctx, sqlcgen.CreateEventParams{
			ID:            event.ID,
			RunID:         event.RunID,
			SchemaVersion: event.SchemaVersion,
			Content:       event.Content,
			SourceInputID: event.SourceInputID,
			Timestamp:     event.Timestamp.UTC().Format(time.RFC3339Nano),
			MetadataJson:  string(metadata),
			IngestedAt:    event.IngestedAt.UTC().Format(time.RFC3339Nano),
			CreatedBy:     createdBy,
		})
		if err != nil {
			return fmt.Errorf("insert event %s: %w", event.ID, err)
		}
	}

	// Index events by id and pre-compute earliest evidence-event
	// timestamp per claim from `links`. The claim's valid_from
	// reflects when the *fact was first observed in the source* —
	// the earliest evidence event — not when we happened to extract
	// it. For backfill / out-of-order ingest this is the only
	// defensible default.
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

	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, claim := range claims {
		if err := claim.Validate(); err != nil {
			return fmt.Errorf("invalid claim %s: %w", claim.ID, err)
		}

		var priorStatus string
		if err := tx.QueryRowContext(ctx, `SELECT status FROM claims WHERE id = ?`, claim.ID).Scan(&priorStatus); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("look up prior status for %s: %w", claim.ID, err)
		}

		createdBy := claim.CreatedBy
		if createdBy == "" {
			createdBy = domain.SystemUser
		}
		validFrom := claim.ValidFrom
		if validFrom.IsZero() {
			if ts, ok := earliestEvidence[claim.ID]; ok {
				validFrom = ts
			} else {
				validFrom = claim.CreatedAt
			}
		}
		err = q.UpsertClaim(ctx, sqlcgen.UpsertClaimParams{
			ID:         claim.ID,
			Text:       claim.Text,
			Type:       string(claim.Type),
			Confidence: claim.Confidence,
			Status:     string(claim.Status),
			CreatedAt:  claim.CreatedAt.UTC().Format(time.RFC3339Nano),
			CreatedBy:  createdBy,
			ValidFrom:  validFrom.UTC().Format(time.RFC3339Nano),
		})
		if err != nil {
			return fmt.Errorf("upsert claim %s: %w", claim.ID, err)
		}

		if priorStatus != string(claim.Status) {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO claim_status_history (claim_id, from_status, to_status, changed_at, reason, changed_by) VALUES (?, ?, ?, ?, ?, ?)`,
				claim.ID, priorStatus, string(claim.Status), now, "pipeline", createdBy,
			); err != nil {
				return fmt.Errorf("record status transition for %s: %w", claim.ID, err)
			}
		}
	}

	for _, link := range links {
		if err := link.Validate(); err != nil {
			return fmt.Errorf("invalid claim evidence: %w", err)
		}
		err = q.UpsertClaimEvidence(ctx, sqlcgen.UpsertClaimEvidenceParams{ClaimID: link.ClaimID, EventID: link.EventID})
		if err != nil {
			return fmt.Errorf("upsert claim evidence (%s,%s): %w", link.ClaimID, link.EventID, err)
		}
	}

	for _, rel := range relationships {
		createdBy := rel.CreatedBy
		if createdBy == "" {
			createdBy = domain.SystemUser
		}
		err = q.UpsertRelationship(ctx, sqlcgen.UpsertRelationshipParams{
			ID:          rel.ID,
			Type:        string(rel.Type),
			FromClaimID: rel.FromClaimID,
			ToClaimID:   rel.ToClaimID,
			CreatedAt:   rel.CreatedAt.UTC().Format(time.RFC3339Nano),
			CreatedBy:   createdBy,
		})
		if err != nil {
			return fmt.Errorf("upsert relationship %s: %w", rel.ID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	// Trust scoring runs after the artifact transaction commits so the
	// trust query can see the just-inserted evidence rows. Doing it
	// inside the same tx would race against itself: the LEFT JOIN we
	// use to count evidence sees uncommitted rows on the same conn,
	// but the recompute path uses a fresh tx for safety. We accept the
	// "trust briefly stale between commit and recompute" window —
	// callers querying this DB right at that instant just see the
	// previous trust_score. Failure of the trust pass is non-fatal:
	// the artifacts are already persisted and a future
	// `mnemos recompute-trust` will fix any drift.
	repo := sqlite.NewClaimRepository(db)
	if _, err := repo.RecomputeTrust(ctx, defaultTrustScorer()); err != nil {
		return fmt.Errorf("recompute trust: %w", err)
	}
	return nil
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
// extractor and writes them to the entities + claim_entities tables.
// Idempotent: re-running over the same input is a no-op courtesy of
// FindOrCreate (entities) and INSERT OR IGNORE (claim_entities).
//
// Runs after PersistArtifacts so the linked claim_id rows already
// exist. Failure here is reported to the caller; current cmd/mnemos
// callers treat it as a warning rather than aborting the whole job
// — the claims are persisted and a future `mnemos extract-entities`
// can backfill what didn't land.
func MaterializeEntities(ctx context.Context, db *sql.DB, entities map[string][]extract.ExtractedEntity, createdBy string) (int, error) {
	if len(entities) == 0 {
		return 0, nil
	}
	repo := sqlite.NewEntityRepository(db)
	linked := 0
	for claimID, ents := range entities {
		for _, ent := range ents {
			etype := domain.EntityType(ent.Type)
			e, err := repo.FindOrCreate(ctx, ent.Name, etype, createdBy)
			if err != nil {
				return linked, fmt.Errorf("find-or-create entity %q: %w", ent.Name, err)
			}
			if err := repo.LinkClaim(ctx, claimID, e.ID, ent.Role); err != nil {
				return linked, fmt.Errorf("link claim %s -> entity %s: %w", claimID, e.ID, err)
			}
			linked++
		}
	}
	return linked, nil
}

// GenerateEmbeddings creates vector embeddings for the given events and stores
// them in the database. Returns the number of embeddings created.
func GenerateEmbeddings(ctx context.Context, db *sql.DB, events []domain.Event) (int, error) {
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

	repo := sqlite.NewEmbeddingRepository(db)
	model := cfg.Model
	for i, ev := range events {
		if i >= len(vectors) {
			break
		}
		if err := repo.Upsert(ctx, ev.ID, "event", vectors[i], model); err != nil {
			return 0, fmt.Errorf("store embedding for event %s: %w", ev.ID, err)
		}
	}

	return len(vectors), nil
}

// GenerateClaimEmbeddings creates vector embeddings for the given claims and
// stores them in the database with entity_type="claim". Returns the number of
// embeddings created.
func GenerateClaimEmbeddings(ctx context.Context, db *sql.DB, claims []domain.Claim) (int, error) {
	if len(claims) == 0 {
		return 0, nil
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

	repo := sqlite.NewEmbeddingRepository(db)
	model := cfg.Model
	for i, cl := range claims {
		if i >= len(vectors) {
			break
		}
		if err := repo.Upsert(ctx, cl.ID, "claim", vectors[i], model); err != nil {
			return 0, fmt.Errorf("store embedding for claim %s: %w", cl.ID, err)
		}
	}

	return len(vectors), nil
}
