// Package pipeline provides shared orchestration logic used by both the CLI and MCP server
// entrypoints: extraction engine setup, artifact persistence, and embedding generation.
package pipeline

import (
	"context"
	"database/sql"
	"encoding/json"
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
)

// Extractor wraps either the rule-based or LLM-powered extraction engine,
// presenting a uniform interface to command handlers.
type Extractor struct {
	ExtractFn func([]domain.Event) ([]domain.Claim, []domain.ClaimEvidence, error)
}

// NewExtractor builds the appropriate extraction engine based on useLLM.
// When useLLM is true, it reads provider config from environment variables
// (MNEMOS_LLM_PROVIDER, MNEMOS_LLM_API_KEY, etc.) and falls back to the
// rule-based engine on LLM failure.
func NewExtractor(useLLM bool) (*Extractor, error) {
	if !useLLM {
		engine := extract.NewEngine()
		return &Extractor{ExtractFn: engine.Extract}, nil
	}

	cfg, err := llm.ConfigFromEnv()
	if err != nil {
		return nil, fmt.Errorf("LLM configuration error: %s\n  Set MNEMOS_LLM_PROVIDER and MNEMOS_LLM_API_KEY environment variables\n  Providers: anthropic, openai, gemini, ollama, openai-compat", err)
	}

	client, err := llm.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create LLM client: %w", err)
	}

	engine := extract.NewLLMEngine(client)
	return &Extractor{ExtractFn: engine.Extract}, nil
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
		err = q.CreateEvent(ctx, sqlcgen.CreateEventParams{
			ID:            event.ID,
			RunID:         event.RunID,
			SchemaVersion: event.SchemaVersion,
			Content:       event.Content,
			SourceInputID: event.SourceInputID,
			Timestamp:     event.Timestamp.UTC().Format(time.RFC3339Nano),
			MetadataJson:  string(metadata),
			IngestedAt:    event.IngestedAt.UTC().Format(time.RFC3339Nano),
		})
		if err != nil {
			return fmt.Errorf("insert event %s: %w", event.ID, err)
		}
	}

	for _, claim := range claims {
		if err := claim.Validate(); err != nil {
			return fmt.Errorf("invalid claim %s: %w", claim.ID, err)
		}
		err = q.UpsertClaim(ctx, sqlcgen.UpsertClaimParams{
			ID:         claim.ID,
			Text:       claim.Text,
			Type:       string(claim.Type),
			Confidence: claim.Confidence,
			Status:     string(claim.Status),
			CreatedAt:  claim.CreatedAt.UTC().Format(time.RFC3339Nano),
		})
		if err != nil {
			return fmt.Errorf("upsert claim %s: %w", claim.ID, err)
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
		err = q.UpsertRelationship(ctx, sqlcgen.UpsertRelationshipParams{
			ID:          rel.ID,
			Type:        string(rel.Type),
			FromClaimID: rel.FromClaimID,
			ToClaimID:   rel.ToClaimID,
			CreatedAt:   rel.CreatedAt.UTC().Format(time.RFC3339Nano),
		})
		if err != nil {
			return fmt.Errorf("upsert relationship %s: %w", rel.ID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
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
