package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/felixgeelhaar/bolt"
	"github.com/felixgeelhaar/fortify/retry"
	mcp "github.com/felixgeelhaar/mcp-go"
	"github.com/felixgeelhaar/mnemos/internal/domain"
	"github.com/felixgeelhaar/mnemos/internal/embedding"
	"github.com/felixgeelhaar/mnemos/internal/extract"
	"github.com/felixgeelhaar/mnemos/internal/ingest"
	"github.com/felixgeelhaar/mnemos/internal/llm"
	"github.com/felixgeelhaar/mnemos/internal/parser"
	"github.com/felixgeelhaar/mnemos/internal/query"
	"github.com/felixgeelhaar/mnemos/internal/relate"
	"github.com/felixgeelhaar/mnemos/internal/store/sqlite"
	"github.com/felixgeelhaar/mnemos/internal/store/sqlite/sqlcgen"
	"github.com/felixgeelhaar/mnemos/internal/workflow"
)

const defaultDBPath = "data/mnemos.db"

type boltLogger struct {
	logger *bolt.Logger
}

func (l boltLogger) Info(msg string, fields ...mcp.LogField) { l.log(l.logger.Info(), msg, fields...) }
func (l boltLogger) Error(msg string, fields ...mcp.LogField) {
	l.log(l.logger.Error(), msg, fields...)
}
func (l boltLogger) Debug(msg string, fields ...mcp.LogField) {
	l.log(l.logger.Debug(), msg, fields...)
}
func (l boltLogger) Warn(msg string, fields ...mcp.LogField) { l.log(l.logger.Warn(), msg, fields...) }

func (l boltLogger) log(event *bolt.Event, msg string, fields ...mcp.LogField) {
	for _, field := range fields {
		event = event.Any(field.Key, field.Value)
	}
	event.Msg(msg)
}

type queryInput struct {
	Question string `json:"question" jsonschema:"required,description=Natural language question to ask Mnemos"`
	RunID    string `json:"runId,omitempty" jsonschema:"description=Optional run ID to scope the query"`
}

type queryOutput struct {
	Answer         string                `json:"answer"`
	Claims         []domain.Claim        `json:"claims"`
	Contradictions []domain.Relationship `json:"contradictions"`
	Timeline       []string              `json:"timeline"`
}

type processTextInput struct {
	Text          string `json:"text" jsonschema:"required,description=Raw text to ingest and process"`
	UseLLM        bool   `json:"useLlm,omitempty" jsonschema:"description=Use configured LLM extraction provider"`
	UseEmbeddings bool   `json:"useEmbeddings,omitempty" jsonschema:"description=Generate embeddings after processing"`
}

type processTextOutput struct {
	RunID          string `json:"runId"`
	Events         int    `json:"events"`
	Claims         int    `json:"claims"`
	Relationships  int    `json:"relationships"`
	Embeddings     int    `json:"embeddings"`
	UsedLLM        bool   `json:"usedLlm"`
	UsedEmbeddings bool   `json:"usedEmbeddings"`
}

func main() {
	logger := bolt.New(bolt.NewJSONHandler(os.Stderr))
	srv := mcp.NewServer(mcp.ServerInfo{
		Name:    "mnemos",
		Version: version,
		Capabilities: mcp.Capabilities{
			Tools: true,
		},
	},
		mcp.WithTitle("Mnemos MCP Server"),
		mcp.WithDescription("Query and update evidence-backed local knowledge with Mnemos."),
		mcp.WithWebsiteURL("https://github.com/felixgeelhaar/mnemos"),
		mcp.WithBuildInfo(commit, buildDate),
		mcp.WithInstructions("Use query_knowledge to read the knowledge base and process_text to ingest raw text into Mnemos. Prefer process_text before querying when no knowledge exists yet."),
	)

	srv.Tool("query_knowledge").
		Description("Query the Mnemos knowledge base and return evidence-backed results.").
		OutputSchema(queryOutput{}).
		ValidateInput().
		Handler(func(ctx context.Context, input queryInput) (queryOutput, error) {
			return runQuery(ctx, input)
		})

	srv.Tool("process_text").
		Description("Ingest raw text, extract claims, detect relationships, and optionally generate embeddings.").
		OutputSchema(processTextOutput{}).
		ValidateInput().
		Handler(func(ctx context.Context, input processTextInput) (processTextOutput, error) {
			return runProcessText(ctx, input)
		})

	if err := mcp.ServeStdio(context.Background(), srv, mcp.WithMiddleware(mcp.DefaultMiddlewareWithTimeout(boltLogger{logger: logger}, 30*time.Second)...)); err != nil {
		log.Fatal(err)
	}
}

func runQuery(_ context.Context, input queryInput) (queryOutput, error) {
	db, err := sqlite.Open(defaultDBPath)
	if err != nil {
		return queryOutput{}, err
	}
	defer func() { _ = db.Close() }()

	engine := query.NewEngine(
		sqlite.NewEventRepository(db),
		sqlite.NewClaimRepository(db),
		sqlite.NewRelationshipRepository(db),
	)

	var answer domain.Answer
	if strings.TrimSpace(input.RunID) != "" {
		answer, err = engine.AnswerForRun(strings.TrimSpace(input.Question), strings.TrimSpace(input.RunID))
	} else {
		answer, err = engine.Answer(strings.TrimSpace(input.Question))
	}
	if err != nil {
		return queryOutput{}, err
	}

	return queryOutput{
		Answer:         answer.AnswerText,
		Claims:         answer.Claims,
		Contradictions: answer.Contradictions,
		Timeline:       answer.TimelineEventIDs,
	}, nil
}

func runProcessText(ctx context.Context, input processTextInput) (processTextOutput, error) {
	service := ingest.NewService()
	normalizer := parser.NewNormalizer()
	progress := mcp.ProgressFromContext(ctx)

	db, err := sqlite.Open(defaultDBPath)
	if err != nil {
		return processTextOutput{}, err
	}
	defer func() { _ = db.Close() }()

	runner := workflow.NewRunner(sqlite.NewCompilationJobRepository(db))
	runner.Timeout = 30 * time.Second
	runner.MaxRetries = 1

	var result processTextOutput
	err = runner.Run("process", map[string]string{"source": "raw_text", "mcp": "true"}, func(ctx context.Context, job *workflow.Job) error {
		total := 5.0
		_ = progress.Report(0, &total)

		if err := job.SetStatus("loading", ""); err != nil {
			return err
		}
		raw := strings.TrimSpace(input.Text)
		in, content, err := service.IngestText(raw, nil)
		if err != nil {
			return err
		}
		_ = progress.Report(1, &total)

		if err := job.SetStatus("extracting", ""); err != nil {
			return err
		}
		events, err := normalizer.Normalize(in, content)
		if err != nil {
			return err
		}
		for i := range events {
			events[i].RunID = job.ID()
		}

		ext, err := newExtractor(input.UseLLM)
		if err != nil {
			return err
		}
		claims, links, err := ext.extract(events)
		if err != nil {
			return err
		}
		_ = progress.Report(2, &total)

		if err := job.SetStatus("relating", ""); err != nil {
			return err
		}
		rels, err := relate.NewEngine().Detect(claims)
		if err != nil {
			return err
		}
		_ = progress.Report(3, &total)

		if err := job.SetStatus("saving", ""); err != nil {
			return err
		}
		if err := persistArtifacts(db, events, claims, links, rels); err != nil {
			return err
		}

		embeddingCount := 0
		if input.UseEmbeddings {
			if err := job.SetStatus("embedding", ""); err != nil {
				return err
			}
			embeddingCount, err = generateEmbeddings(ctx, db, events)
			if err != nil {
				return err
			}
		}
		_ = progress.Report(5, &total)

		result = processTextOutput{
			RunID:          job.ID(),
			Events:         len(events),
			Claims:         len(claims),
			Relationships:  len(rels),
			Embeddings:     embeddingCount,
			UsedLLM:        input.UseLLM,
			UsedEmbeddings: input.UseEmbeddings,
		}
		return nil
	})
	if err != nil {
		return processTextOutput{}, err
	}

	return result, nil
}

type extractor struct {
	extract func([]domain.Event) ([]domain.Claim, []domain.ClaimEvidence, error)
}

func newExtractor(useLLM bool) (*extractor, error) {
	if !useLLM {
		engine := extract.NewEngine()
		return &extractor{extract: engine.Extract}, nil
	}

	cfg, err := llm.ConfigFromEnv()
	if err != nil {
		return nil, err
	}
	client, err := llm.NewClient(cfg)
	if err != nil {
		return nil, err
	}
	engine := extract.NewLLMEngine(client)
	return &extractor{extract: engine.Extract}, nil
}

func persistArtifacts(db *sql.DB, events []domain.Event, claims []domain.Claim, links []domain.ClaimEvidence, relationships []domain.Relationship) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	q := sqlcgen.New(tx)
	ctx := context.Background()

	for _, event := range events {
		metadata, err := json.Marshal(event.Metadata)
		if err != nil {
			return err
		}
		if err := q.CreateEvent(ctx, sqlcgen.CreateEventParams{
			ID:            event.ID,
			RunID:         event.RunID,
			SchemaVersion: event.SchemaVersion,
			Content:       event.Content,
			SourceInputID: event.SourceInputID,
			Timestamp:     event.Timestamp.UTC().Format(time.RFC3339Nano),
			MetadataJson:  string(metadata),
			IngestedAt:    event.IngestedAt.UTC().Format(time.RFC3339Nano),
		}); err != nil {
			return err
		}
	}

	for _, claim := range claims {
		if err := q.UpsertClaim(ctx, sqlcgen.UpsertClaimParams{
			ID:         claim.ID,
			Text:       claim.Text,
			Type:       string(claim.Type),
			Confidence: claim.Confidence,
			Status:     string(claim.Status),
			CreatedAt:  claim.CreatedAt.UTC().Format(time.RFC3339Nano),
		}); err != nil {
			return err
		}
	}

	for _, link := range links {
		if err := q.UpsertClaimEvidence(ctx, sqlcgen.UpsertClaimEvidenceParams{ClaimID: link.ClaimID, EventID: link.EventID}); err != nil {
			return err
		}
	}

	for _, rel := range relationships {
		if err := q.UpsertRelationship(ctx, sqlcgen.UpsertRelationshipParams{
			ID:          rel.ID,
			Type:        string(rel.Type),
			FromClaimID: rel.FromClaimID,
			ToClaimID:   rel.ToClaimID,
			CreatedAt:   rel.CreatedAt.UTC().Format(time.RFC3339Nano),
		}); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func generateEmbeddings(ctx context.Context, db *sql.DB, events []domain.Event) (int, error) {
	cfg, err := embedding.ConfigFromEnv()
	if err != nil {
		return 0, err
	}
	client, err := embedding.NewClient(cfg)
	if err != nil {
		return 0, err
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
		return 0, err
	}

	repo := sqlite.NewEmbeddingRepository(db)
	for i, ev := range events {
		if i >= len(vectors) {
			break
		}
		if err := repo.Upsert(ev.ID, "event", vectors[i], cfg.Model); err != nil {
			return 0, err
		}
	}

	return len(vectors), nil
}
