package main

import (
	"context"
	"database/sql"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/felixgeelhaar/bolt"
	mcp "github.com/felixgeelhaar/mcp-go"
	"github.com/felixgeelhaar/mnemos/internal/domain"
	"github.com/felixgeelhaar/mnemos/internal/ingest"
	"github.com/felixgeelhaar/mnemos/internal/parser"
	"github.com/felixgeelhaar/mnemos/internal/pipeline"
	"github.com/felixgeelhaar/mnemos/internal/query"
	"github.com/felixgeelhaar/mnemos/internal/relate"
	"github.com/felixgeelhaar/mnemos/internal/store/sqlite"
	"github.com/felixgeelhaar/mnemos/internal/workflow"
)

// resolveDBPath returns the database path from MNEMOS_DB_PATH or the
// XDG-compliant default (~/.local/share/mnemos/mnemos.db).
func resolveDBPath() string {
	if p := os.Getenv("MNEMOS_DB_PATH"); p != "" {
		return p
	}
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return filepath.Join("data", "mnemos.db")
		}
		dataHome = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(dataHome, "mnemos", "mnemos.db")
}

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

type metricsOutput struct {
	Runs            int64 `json:"runs"`
	Events          int64 `json:"events"`
	Claims          int64 `json:"claims"`
	ContestedClaims int64 `json:"contested_claims"`
	Relationships   int64 `json:"relationships"`
	Contradictions  int64 `json:"contradictions"`
	Embeddings      int64 `json:"embeddings"`
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

	srv.Tool("knowledge_metrics").
		Description("Return counts and statistics about the Mnemos knowledge base.").
		OutputSchema(metricsOutput{}).
		Handler(func(_ context.Context, _ struct{}) (metricsOutput, error) {
			return runMetrics()
		})

	if err := mcp.ServeStdio(context.Background(), srv, mcp.WithMiddleware(mcp.DefaultMiddlewareWithTimeout(boltLogger{logger: logger}, 30*time.Second)...)); err != nil {
		log.Fatal(err)
	}
}

func runQuery(_ context.Context, input queryInput) (queryOutput, error) {
	db, err := sqlite.Open(resolveDBPath())
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

	db, err := sqlite.Open(resolveDBPath())
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
		rels, err := relate.NewEngine().Detect(claims)
		if err != nil {
			return err
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

func runMetrics() (metricsOutput, error) {
	db, err := sqlite.Open(resolveDBPath())
	if err != nil {
		return metricsOutput{}, err
	}
	defer func() { _ = db.Close() }()

	return metricsOutput{
		Runs:            countRows(db, `SELECT COUNT(DISTINCT run_id) FROM events WHERE run_id <> ''`),
		Events:          countRows(db, `SELECT COUNT(*) FROM events`),
		Claims:          countRows(db, `SELECT COUNT(*) FROM claims`),
		ContestedClaims: countRows(db, `SELECT COUNT(*) FROM claims WHERE status = 'contested'`),
		Relationships:   countRows(db, `SELECT COUNT(*) FROM relationships`),
		Contradictions:  countRows(db, `SELECT COUNT(*) FROM relationships WHERE type = 'contradicts'`),
		Embeddings:      countRows(db, `SELECT COUNT(*) FROM embeddings`),
	}, nil
}

func countRows(db *sql.DB, query string) int64 {
	var n int64
	if err := db.QueryRow(query).Scan(&n); err != nil {
		return 0
	}
	return n
}
