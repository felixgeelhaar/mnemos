package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/felixgeelhaar/bolt"
	mcp "github.com/felixgeelhaar/mcp-go"
	"github.com/felixgeelhaar/mnemos/internal/domain"
	"github.com/felixgeelhaar/mnemos/internal/embedding"
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
	// ClaimProvenance maps claim ID to "local" or a registry URL so the
	// agent can show which claims came from a federated registry.
	ClaimProvenance map[string]string `json:"claim_provenance,omitempty"`
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

type mcpIngestGitLogInput struct {
	Limit int    `json:"limit,omitempty" jsonschema:"description=Max number of commits to ingest (default 50, cap 1000)"`
	Since string `json:"since,omitempty" jsonschema:"description=Optional date string passed to git --since (e.g. '2026-01-01' or '2 weeks ago')"`
}

type mcpIngestGitLogOutput struct {
	Ingested int `json:"ingested"`
	Skipped  int `json:"skipped"`
}

type mcpIngestGitPRsInput struct {
	Limit int `json:"limit,omitempty" jsonschema:"description=Max number of merged PRs to ingest (default 20, cap 200)"`
}

type mcpIngestGitPRsOutput struct {
	Ingested int `json:"ingested"`
	Skipped  int `json:"skipped"`
}

type mcpWatchFileInput struct {
	Path string `json:"path" jsonschema:"required,description=Absolute or relative path to the file to watch for changes"`
}

type mcpWatchFileOutput struct {
	Watching      bool   `json:"watching"`
	Path          string `json:"path"`
	ActiveWatches int    `json:"activeWatches"`
}

// handleMCP starts the MCP server over stdio. This is a long-lived process
// that blocks until the connection is closed.
func handleMCP() {
	logger := bolt.New(bolt.NewJSONHandler(os.Stderr))

	// When launched inside a project (.mnemos/ exists), bulk-ingest the
	// standard project documents so the agent has context immediately. New
	// or unchanged source paths are skipped, so this is safe to run on
	// every startup.
	if _, projectRoot, ok := findProjectDB(); ok {
		runAutoIngest(projectRoot)
		if repoIsGit(projectRoot) {
			runGitContextIngest(projectRoot)
			runPRContextIngest(projectRoot)
		}
	}

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
		mcp.WithInstructions("Use query_knowledge to read the knowledge base, process_text to ingest raw text, and watch_file to keep a specific file's claims fresh as it changes. Prefer process_text before querying when no knowledge exists yet."),
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

	// watch_file uses a long-lived DB connection separate from the
	// per-call connections in the other handlers. Opened lazily so
	// startup doesn't fail just because the watcher isn't needed.
	var (
		watcherOnce sync.Once
		watcher     *Watcher
		watcherErr  error
	)
	getWatcher := func() (*Watcher, error) {
		watcherOnce.Do(func() {
			db, err := sqlite.Open(resolveDBPath())
			if err != nil {
				watcherErr = err
				return
			}
			watcher = NewWatcher(db)
		})
		return watcher, watcherErr
	}

	srv.Tool("list_claims").
		Description("List claims with optional type/status filtering and pagination. Useful for browsing the knowledge base without a specific question.").
		OutputSchema(mcpListClaimsOutput{}).
		ValidateInput().
		Handler(func(ctx context.Context, input mcpListClaimsInput) (mcpListClaimsOutput, error) {
			return mcpRunListClaims(ctx, input)
		})

	srv.Tool("list_decisions").
		Description("List claims classified as decisions (shorthand for list_claims with type=decision).").
		OutputSchema(mcpListClaimsOutput{}).
		ValidateInput().
		Handler(func(ctx context.Context, input mcpListClaimsInput) (mcpListClaimsOutput, error) {
			input.Type = string(domain.ClaimTypeDecision)
			return mcpRunListClaims(ctx, input)
		})

	srv.Tool("list_contradictions").
		Description("List contradiction relationships hydrated with both claims' text. Pagination supported.").
		OutputSchema(mcpListContradictionsOutput{}).
		ValidateInput().
		Handler(func(ctx context.Context, input mcpListContradictionsInput) (mcpListContradictionsOutput, error) {
			return mcpRunListContradictions(ctx, input)
		})

	srv.Tool("ingest_git_prs").
		Description("Ingest merged GitHub pull requests from the project as events. Requires gh CLI authenticated for the repo's remote. Idempotent — already-ingested PR numbers are skipped.").
		OutputSchema(mcpIngestGitPRsOutput{}).
		ValidateInput().
		Handler(func(ctx context.Context, input mcpIngestGitPRsInput) (mcpIngestGitPRsOutput, error) {
			return mcpRunIngestGitPRs(ctx, input)
		})

	srv.Tool("ingest_git_log").
		Description("Ingest recent git commits from the project repository as events so they appear in queries. Idempotent — already-ingested commits are skipped by SHA.").
		OutputSchema(mcpIngestGitLogOutput{}).
		ValidateInput().
		Handler(func(ctx context.Context, input mcpIngestGitLogInput) (mcpIngestGitLogOutput, error) {
			return mcpRunIngestGitLog(ctx, input)
		})

	srv.Tool("watch_file").
		Description("Register a file to be re-ingested when its content changes. Polls every few seconds; in-memory only — restart drops all watches.").
		OutputSchema(mcpWatchFileOutput{}).
		ValidateInput().
		Handler(func(_ context.Context, input mcpWatchFileInput) (mcpWatchFileOutput, error) {
			w, err := getWatcher()
			if err != nil {
				return mcpWatchFileOutput{}, err
			}
			count, err := w.Add(input.Path)
			if err != nil {
				return mcpWatchFileOutput{}, err
			}
			return mcpWatchFileOutput{
				Watching:      true,
				Path:          input.Path,
				ActiveWatches: count,
			}, nil
		})

	if err := mcp.ServeStdio(context.Background(), srv, mcp.WithMiddleware(mcp.DefaultMiddlewareWithTimeout(mcpBoltLogger{logger: logger}, 30*time.Second)...)); err != nil {
		log.Fatal(err)
	}
}

func runGitContextIngest(projectRoot string) {
	db, err := sqlite.Open(resolveDBPath())
	if err != nil {
		fmt.Fprintf(os.Stderr, "git: failed to open DB: %v\n", err)
		return
	}
	defer func() { _ = db.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ingested, skipped, err := ingestGitLog(ctx, db, projectRoot, defaultGitLogLimit, "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "git: %v\n", err)
		return
	}
	if ingested > 0 || skipped > 0 {
		fmt.Fprintf(os.Stderr, "git-context: ingested=%d skipped=%d root=%s\n", ingested, skipped, projectRoot)
	}
}

func runPRContextIngest(projectRoot string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Avoid shelling out to gh if it isn't available or isn't authed —
	// ghAvailable is the cheap probe.
	if !ghAvailable(ctx) {
		return
	}

	db, err := sqlite.Open(resolveDBPath())
	if err != nil {
		fmt.Fprintf(os.Stderr, "git-prs: failed to open DB: %v\n", err)
		return
	}
	defer func() { _ = db.Close() }()

	ingested, skipped, err := ingestGhPRs(ctx, db, projectRoot, defaultGitPRLimit)
	if err != nil {
		fmt.Fprintf(os.Stderr, "git-prs: %v\n", err)
		return
	}
	if ingested > 0 || skipped > 0 {
		fmt.Fprintf(os.Stderr, "git-prs: ingested=%d skipped=%d root=%s\n", ingested, skipped, projectRoot)
	}
}

func mcpRunIngestGitPRs(ctx context.Context, input mcpIngestGitPRsInput) (mcpIngestGitPRsOutput, error) {
	_, projectRoot, ok := findProjectDB()
	if !ok {
		return mcpIngestGitPRsOutput{}, fmt.Errorf("no project (.mnemos/) found — run 'mnemos init' first")
	}
	if !ghAvailable(ctx) {
		return mcpIngestGitPRsOutput{}, fmt.Errorf("gh CLI not installed or not authenticated for github.com")
	}
	db, err := sqlite.Open(resolveDBPath())
	if err != nil {
		return mcpIngestGitPRsOutput{}, err
	}
	defer func() { _ = db.Close() }()

	ingested, skipped, err := ingestGhPRs(ctx, db, projectRoot, input.Limit)
	if err != nil {
		return mcpIngestGitPRsOutput{}, err
	}
	return mcpIngestGitPRsOutput{Ingested: ingested, Skipped: skipped}, nil
}

func mcpRunIngestGitLog(ctx context.Context, input mcpIngestGitLogInput) (mcpIngestGitLogOutput, error) {
	_, projectRoot, ok := findProjectDB()
	if !ok {
		return mcpIngestGitLogOutput{}, fmt.Errorf("no project (.mnemos/) found — run 'mnemos init' first")
	}
	if !repoIsGit(projectRoot) {
		return mcpIngestGitLogOutput{}, fmt.Errorf("project root %s is not a git repository", projectRoot)
	}
	db, err := sqlite.Open(resolveDBPath())
	if err != nil {
		return mcpIngestGitLogOutput{}, err
	}
	defer func() { _ = db.Close() }()

	ingested, skipped, err := ingestGitLog(ctx, db, projectRoot, input.Limit, input.Since)
	if err != nil {
		return mcpIngestGitLogOutput{}, err
	}
	return mcpIngestGitLogOutput{Ingested: ingested, Skipped: skipped}, nil
}

func runAutoIngest(projectRoot string) {
	db, err := sqlite.Open(resolveDBPath())
	if err != nil {
		fmt.Fprintf(os.Stderr, "auto-ingest: failed to open DB: %v\n", err)
		return
	}
	defer func() { _ = db.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ingested, skipped := autoIngestProjectDocs(ctx, db, projectRoot)
	if ingested > 0 || skipped > 0 {
		fmt.Fprintf(os.Stderr, "auto-ingest: ingested=%d skipped=%d root=%s\n", ingested, skipped, projectRoot)
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

	// Enable semantic ranking when an embedding provider is configured —
	// auto-detected via Ollama or env vars. Without this the engine falls
	// back to token-overlap ranking and semantic matches are missed even
	// when the DB has embeddings.
	if embCfg, err := embedding.ConfigFromEnv(); err == nil {
		if embClient, err := embedding.NewClient(embCfg); err == nil {
			engine = engine.WithEmbeddings(
				sqlite.NewEmbeddingRepository(db),
				embClient,
			)
		}
	}

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
		Answer:          answer.AnswerText,
		Claims:          answer.Claims,
		Contradictions:  answer.Contradictions,
		Timeline:        answer.TimelineEventIDs,
		ClaimProvenance: answer.ClaimProvenance,
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
