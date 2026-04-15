package main

import (
	"context"
	"database/sql"
	"log"
	"os"
	"strings"
	"time"

	"github.com/felixgeelhaar/bolt"
	mcp "github.com/felixgeelhaar/mcp-go"
	"github.com/felixgeelhaar/mnemos/internal/domain"
	"github.com/felixgeelhaar/mnemos/internal/ingest"
	"github.com/felixgeelhaar/mnemos/internal/llm"
	"github.com/felixgeelhaar/mnemos/internal/parser"
	"github.com/felixgeelhaar/mnemos/internal/pipeline"
	"github.com/felixgeelhaar/mnemos/internal/query"
	"github.com/felixgeelhaar/mnemos/internal/relate"
	"github.com/felixgeelhaar/mnemos/internal/store/sqlite"
	"github.com/felixgeelhaar/mnemos/internal/workflow"
)

type mcpBoltLogger struct {
	logger *bolt.Logger
}

func (l mcpBoltLogger) Info(msg string, fields ...mcp.LogField) {
	l.log(l.logger.Info(), msg, fields...)
}
func (l mcpBoltLogger) Error(msg string, fields ...mcp.LogField) {
	l.log(l.logger.Error(), msg, fields...)
}
func (l mcpBoltLogger) Debug(msg string, fields ...mcp.LogField) {
	l.log(l.logger.Debug(), msg, fields...)
}
func (l mcpBoltLogger) Warn(msg string, fields ...mcp.LogField) {
	l.log(l.logger.Warn(), msg, fields...)
}

func (l mcpBoltLogger) log(event *bolt.Event, msg string, fields ...mcp.LogField) {
	for _, field := range fields {
		event = event.Any(field.Key, field.Value)
	}
	event.Msg(msg)
}

type mcpQueryInput struct {
	Question string `json:"question" jsonschema:"required,description=Natural language question to ask Mnemos"`
	RunID    string `json:"runId,omitempty" jsonschema:"description=Optional run ID to scope the query"`
}

type mcpQueryOutput struct {
	Answer         string                `json:"answer"`
	Claims         []domain.Claim        `json:"claims"`
	Contradictions []domain.Relationship `json:"contradictions"`
	Timeline       []string              `json:"timeline"`
}

type mcpProcessTextInput struct {
	Text          string `json:"text" jsonschema:"required,description=Raw text to ingest and process"`
	UseLLM        bool   `json:"useLlm,omitempty" jsonschema:"description=Use configured LLM extraction provider"`
	UseEmbeddings bool   `json:"useEmbeddings,omitempty" jsonschema:"description=Generate embeddings after processing"`
}

type mcpProcessTextOutput struct {
	RunID          string `json:"runId"`
	Events         int    `json:"events"`
	Claims         int    `json:"claims"`
	Relationships  int    `json:"relationships"`
	Embeddings     int    `json:"embeddings"`
	UsedLLM        bool   `json:"usedLlm"`
	UsedEmbeddings bool   `json:"usedEmbeddings"`
}

type mcpMetricsOutput struct {
	Runs            int64 `json:"runs"`
	Events          int64 `json:"events"`
	Claims          int64 `json:"claims"`
	ContestedClaims int64 `json:"contested_claims"`
	Relationships   int64 `json:"relationships"`
	Contradictions  int64 `json:"contradictions"`
	Embeddings      int64 `json:"embeddings"`
}

// handleMCP starts the MCP server over stdio. This is a long-lived process
// that blocks until the connection is closed.
func handleMCP() {
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
		OutputSchema(mcpQueryOutput{}).
		ValidateInput().
		Handler(func(ctx context.Context, input mcpQueryInput) (mcpQueryOutput, error) {
			return mcpRunQuery(ctx, input)
		})

	srv.Tool("process_text").
		Description("Ingest raw text, extract claims, detect relationships, and optionally generate embeddings.").
		OutputSchema(mcpProcessTextOutput{}).
		ValidateInput().
		Handler(func(ctx context.Context, input mcpProcessTextInput) (mcpProcessTextOutput, error) {
			return mcpRunProcessText(ctx, input)
		})

	srv.Tool("knowledge_metrics").
		Description("Return counts and statistics about the Mnemos knowledge base.").
		OutputSchema(mcpMetricsOutput{}).
		Handler(func(_ context.Context, _ struct{}) (mcpMetricsOutput, error) {
			return mcpRunMetrics()
		})

	if err := mcp.ServeStdio(context.Background(), srv, mcp.WithMiddleware(mcp.DefaultMiddlewareWithTimeout(mcpBoltLogger{logger: logger}, 30*time.Second)...)); err != nil {
		log.Fatal(err)
	}
}

func mcpRunQuery(_ context.Context, input mcpQueryInput) (mcpQueryOutput, error) {
	db, err := sqlite.Open(resolveDBPath())
	if err != nil {
		return mcpQueryOutput{}, err
	}
	defer func() { _ = db.Close() }()

	engine := query.NewEngine(
		sqlite.NewEventRepository(db),
		sqlite.NewClaimRepository(db),
		sqlite.NewRelationshipRepository(db),
	)

	if llmCfg, err := llm.ConfigFromEnv(); err == nil {
		if llmClient, err := llm.NewClient(llmCfg); err == nil {
			engine = engine.WithLLM(llmClient)
		}
	}

	var answer domain.Answer
	if strings.TrimSpace(input.RunID) != "" {
		answer, err = engine.AnswerForRun(strings.TrimSpace(input.Question), strings.TrimSpace(input.RunID))
	} else {
		answer, err = engine.Answer(strings.TrimSpace(input.Question))
	}
	if err != nil {
		return mcpQueryOutput{}, err
	}

	return mcpQueryOutput{
		Answer:         answer.AnswerText,
		Claims:         answer.Claims,
		Contradictions: answer.Contradictions,
		Timeline:       answer.TimelineEventIDs,
	}, nil
}

func mcpRunProcessText(ctx context.Context, input mcpProcessTextInput) (mcpProcessTextOutput, error) {
	service := ingest.NewService()
	normalizer := parser.NewNormalizer()
	progress := mcp.ProgressFromContext(ctx)

	db, err := sqlite.Open(resolveDBPath())
	if err != nil {
		return mcpProcessTextOutput{}, err
	}
	defer func() { _ = db.Close() }()

	runner := workflow.NewRunner(sqlite.NewCompilationJobRepository(db))
	runner.Timeout = 30 * time.Second
	runner.MaxRetries = 1

	var result mcpProcessTextOutput
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

		ext, err := pipeline.NewExtractor(input.UseLLM)
		if err != nil {
			return err
		}
		claims, links, err := ext.ExtractFn(events)
		if err != nil {
			return err
		}
		_ = progress.Report(2, &total)

		if err := job.SetStatus("relating", ""); err != nil {
			return err
		}
		relEngine := relate.NewEngine()
		rels, err := relEngine.Detect(claims)
		if err != nil {
			return err
		}

		claimRepo := sqlite.NewClaimRepository(db)
		existingClaims, err := claimRepo.ListAll(ctx)
		if err != nil {
			return err
		}
		if len(existingClaims) > 0 {
			incrementalRels, err := relEngine.DetectIncremental(claims, existingClaims)
			if err != nil {
				return err
			}
			rels = append(rels, incrementalRels...)
		}
		_ = progress.Report(3, &total)

		if err := job.SetStatus("saving", ""); err != nil {
			return err
		}
		if err := pipeline.PersistArtifacts(ctx, db, events, claims, links, rels); err != nil {
			return err
		}

		embeddingCount := 0
		if input.UseEmbeddings {
			if err := job.SetStatus("embedding", ""); err != nil {
				return err
			}
			embeddingCount, err = pipeline.GenerateEmbeddings(ctx, db, events)
			if err != nil {
				return err
			}
			claimEmbCount, claimErr := pipeline.GenerateClaimEmbeddings(ctx, db, claims)
			if claimErr != nil {
				return claimErr
			}
			embeddingCount += claimEmbCount
		}
		_ = progress.Report(5, &total)

		result = mcpProcessTextOutput{
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
		return mcpProcessTextOutput{}, err
	}

	return result, nil
}

func mcpRunMetrics() (mcpMetricsOutput, error) {
	db, err := sqlite.Open(resolveDBPath())
	if err != nil {
		return mcpMetricsOutput{}, err
	}
	defer func() { _ = db.Close() }()

	return mcpMetricsOutput{
		Runs:            mcpCountRows(db, `SELECT COUNT(DISTINCT run_id) FROM events WHERE run_id <> ''`),
		Events:          mcpCountRows(db, `SELECT COUNT(*) FROM events`),
		Claims:          mcpCountRows(db, `SELECT COUNT(*) FROM claims`),
		ContestedClaims: mcpCountRows(db, `SELECT COUNT(*) FROM claims WHERE status = 'contested'`),
		Relationships:   mcpCountRows(db, `SELECT COUNT(*) FROM relationships`),
		Contradictions:  mcpCountRows(db, `SELECT COUNT(*) FROM relationships WHERE type = 'contradicts'`),
		Embeddings:      mcpCountRows(db, `SELECT COUNT(*) FROM embeddings`),
	}, nil
}

func mcpCountRows(db *sql.DB, q string) int64 {
	var n int64
	if err := db.QueryRow(q).Scan(&n); err != nil {
		return 0
	}
	return n
}
