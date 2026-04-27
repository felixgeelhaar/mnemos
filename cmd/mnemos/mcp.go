package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
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
	"github.com/felixgeelhaar/mnemos/internal/store"
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
	Hops     int    `json:"hops,omitempty" jsonschema:"description=BFS hop expansion depth through supports/contradicts edges (0-5, default 0)"`
}

type mcpQueryOutput struct {
	Answer         string                `json:"answer"`
	Claims         []domain.Claim        `json:"claims"`
	Contradictions []domain.Relationship `json:"contradictions"`
	Timeline       []string              `json:"timeline"`
	// ClaimProvenance maps claim ID to "local" or a registry URL so the
	// agent can show which claims came from a federated registry.
	ClaimProvenance map[string]string `json:"claim_provenance,omitempty"`
	// ClaimHopDistance maps claim ID to the BFS hop count from the
	// directly-retrieved set (0 = direct, N>0 = expanded via N hops of
	// supports/contradicts). Empty when hops=0.
	ClaimHopDistance map[string]int `json:"claim_hop_distance,omitempty"`
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

	// Resolve the actor once at startup from MNEMOS_USER_ID; every
	// persistence path below stamps it as created_by / changed_by. We
	// only validate against the DB when the env var is non-empty — an
	// unset env reliably means "attribute to <system>" with no lookup.
	mcpActor := resolveMCPActor()

	// When launched inside a project (.mnemos/ exists), bulk-ingest the
	// standard project documents so the agent has context immediately. New
	// or unchanged source paths are skipped, so this is safe to run on
	// every startup.
	if _, projectRoot, ok := findProjectDB(); ok {
		runAutoIngest(projectRoot, mcpActor)
		if repoIsGit(projectRoot) {
			runGitContextIngest(projectRoot, mcpActor)
			runPRContextIngest(projectRoot, mcpActor)
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

	// watch_file uses a long-lived DB connection separate from the
	// per-call connections in the other handlers. Opened lazily so
	// startup doesn't fail just because the watcher isn't needed.
	// We also remember the DB handle so the shutdown defer can close
	// it after stopping the polling goroutine — without this the
	// watcher leaks a connection on every MCP exit.
	var (
		watcherOnce sync.Once
		watcher     *Watcher
		watcherConn *store.Conn
		watcherErr  error
	)
	getWatcher := func() (*Watcher, error) {
		watcherOnce.Do(func() {
			// Background context is fine here: the open is a one-shot
			// per process and the long-lived Conn lifecycle is governed
			// by the deferred closeConn below, not by request-scoped
			// cancellation.
			conn, err := openConn(context.Background())
			if err != nil {
				watcherErr = err
				return
			}
			watcherConn = conn
			watcher = NewWatcher(conn, mcpActor)
		})
		return watcher, watcherErr
	}

	// Build the axi-go kernel that wraps every MCP tool with effect
	// gating, an evidence chain, and an execution budget. If the
	// kernel fails to build the MCP server still starts — we fall
	// back to direct dispatch so a kernel bug never blocks the agent.
	kernel, kernelErr := buildMCPKernel(logger, mcpExecutorMap(mcpActor, getWatcher))
	if kernelErr != nil {
		fmt.Fprintf(os.Stderr, "mcp: axi-go kernel disabled: %v\n", kernelErr)
	}

	srv.Tool("query_knowledge").
		Description("Query the Mnemos knowledge base and return evidence-backed results.").
		OutputSchema(mcpQueryOutput{}).
		ValidateInput().
		Handler(func(ctx context.Context, input mcpQueryInput) (mcpQueryOutput, error) {
			if kernel != nil {
				return dispatchAxiTool[mcpQueryOutput](ctx, kernel, nil, "query_knowledge", input)
			}
			return mcpRunQuery(ctx, input)
		})

	srv.Tool("process_text").
		Description("Ingest raw text, extract claims, detect relationships, and optionally generate embeddings.").
		OutputSchema(mcpProcessTextOutput{}).
		ValidateInput().
		Handler(func(ctx context.Context, input mcpProcessTextInput) (mcpProcessTextOutput, error) {
			if kernel != nil {
				return dispatchAxiTool[mcpProcessTextOutput](ctx, kernel, nil, "process_text", input)
			}
			return mcpRunProcessText(ctx, mcpActor, input)
		})

	srv.Tool("knowledge_metrics").
		Description("Return counts and statistics about the Mnemos knowledge base.").
		OutputSchema(mcpMetricsOutput{}).
		Handler(func(ctx context.Context, _ struct{}) (mcpMetricsOutput, error) {
			if kernel != nil {
				return dispatchAxiTool[mcpMetricsOutput](ctx, kernel, nil, "knowledge_metrics", struct{}{})
			}
			return mcpRunMetrics()
		})

	srv.Tool("list_claims").
		Description("List claims with optional type/status filtering and pagination. Useful for browsing the knowledge base without a specific question.").
		OutputSchema(mcpListClaimsOutput{}).
		ValidateInput().
		Handler(func(ctx context.Context, input mcpListClaimsInput) (mcpListClaimsOutput, error) {
			if kernel != nil {
				return dispatchAxiTool[mcpListClaimsOutput](ctx, kernel, nil, "list_claims", input)
			}
			return mcpRunListClaims(ctx, input)
		})

	srv.Tool("list_decisions").
		Description("List claims classified as decisions (shorthand for list_claims with type=decision).").
		OutputSchema(mcpListClaimsOutput{}).
		ValidateInput().
		Handler(func(ctx context.Context, input mcpListClaimsInput) (mcpListClaimsOutput, error) {
			input.Type = string(domain.ClaimTypeDecision)
			if kernel != nil {
				return dispatchAxiTool[mcpListClaimsOutput](ctx, kernel, nil, "list_decisions", input)
			}
			return mcpRunListClaims(ctx, input)
		})

	srv.Tool("list_contradictions").
		Description("List contradiction relationships hydrated with both claims' text. Pagination supported.").
		OutputSchema(mcpListContradictionsOutput{}).
		ValidateInput().
		Handler(func(ctx context.Context, input mcpListContradictionsInput) (mcpListContradictionsOutput, error) {
			if kernel != nil {
				return dispatchAxiTool[mcpListContradictionsOutput](ctx, kernel, nil, "list_contradictions", input)
			}
			return mcpRunListContradictions(ctx, input)
		})

	srv.Tool("ingest_git_prs").
		Description("Ingest merged GitHub pull requests from the project as events. Requires gh CLI authenticated for the repo's remote. Idempotent — already-ingested PR numbers are skipped.").
		OutputSchema(mcpIngestGitPRsOutput{}).
		ValidateInput().
		Handler(func(ctx context.Context, input mcpIngestGitPRsInput) (mcpIngestGitPRsOutput, error) {
			if kernel != nil {
				return dispatchAxiTool[mcpIngestGitPRsOutput](ctx, kernel, nil, "ingest_git_prs", input)
			}
			return mcpRunIngestGitPRs(ctx, mcpActor, input)
		})

	srv.Tool("ingest_git_log").
		Description("Ingest recent git commits from the project repository as events so they appear in queries. Idempotent — already-ingested commits are skipped by SHA.").
		OutputSchema(mcpIngestGitLogOutput{}).
		ValidateInput().
		Handler(func(ctx context.Context, input mcpIngestGitLogInput) (mcpIngestGitLogOutput, error) {
			if kernel != nil {
				return dispatchAxiTool[mcpIngestGitLogOutput](ctx, kernel, nil, "ingest_git_log", input)
			}
			return mcpRunIngestGitLog(ctx, mcpActor, input)
		})

	srv.Tool("watch_file").
		Description("Register a file to be re-ingested when its content changes. Polls every few seconds; in-memory only — restart drops all watches.").
		OutputSchema(mcpWatchFileOutput{}).
		ValidateInput().
		Handler(func(ctx context.Context, input mcpWatchFileInput) (mcpWatchFileOutput, error) {
			if kernel != nil {
				return dispatchAxiTool[mcpWatchFileOutput](ctx, kernel, nil, "watch_file", input)
			}
			out, err := runWatchFileTool(input, getWatcher)
			if err != nil {
				return mcpWatchFileOutput{}, err
			}
			return out, nil
		})

	// Wire signal handling so a SIGINT/SIGTERM cancels the parent
	// context: ServeStdio observes the cancellation and returns,
	// then we tear the watcher down. Without this, Ctrl+C would
	// leave the polling goroutine alive and the DB unflushed.
	rootCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stopSignals := make(chan os.Signal, 1)
	signal.Notify(stopSignals, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig, ok := <-stopSignals
		if !ok {
			return
		}
		fmt.Fprintf(os.Stderr, "mcp: received %s, shutting down...\n", sig)
		cancel()
	}()

	// Defer watcher shutdown so the polling goroutine exits and the
	// DB connection it holds gets released. Cheap if no watcher was
	// ever started.
	defer func() {
		if watcher != nil {
			watcher.Stop()
		}
		if watcherConn != nil {
			_ = watcherConn.Close()
		}
	}()

	if err := mcp.ServeStdio(rootCtx, srv, mcp.WithMiddleware(mcp.DefaultMiddlewareWithTimeout(mcpBoltLogger{logger: logger}, 30*time.Second)...)); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatal(err)
	}
}

func runGitContextIngest(projectRoot, actor string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	conn, err := openConn(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "git: failed to open DB: %v\n", err)
		return
	}
	defer closeConn(conn)

	ingested, skipped, err := ingestGitLog(ctx, conn, projectRoot, defaultGitLogLimit, "", actor)
	if err != nil {
		fmt.Fprintf(os.Stderr, "git: %v\n", err)
		return
	}
	if ingested > 0 || skipped > 0 {
		fmt.Fprintf(os.Stderr, "git-context: ingested=%d skipped=%d root=%s\n", ingested, skipped, projectRoot)
	}
}

func runPRContextIngest(projectRoot, actor string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Avoid shelling out to gh if it isn't available or isn't authed —
	// ghAvailable is the cheap probe.
	if !ghAvailable(ctx) {
		return
	}

	conn, err := openConn(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "git-prs: failed to open DB: %v\n", err)
		return
	}
	defer closeConn(conn)

	ingested, skipped, err := ingestGhPRs(ctx, conn, projectRoot, defaultGitPRLimit, actor)
	if err != nil {
		fmt.Fprintf(os.Stderr, "git-prs: %v\n", err)
		return
	}
	if ingested > 0 || skipped > 0 {
		fmt.Fprintf(os.Stderr, "git-prs: ingested=%d skipped=%d root=%s\n", ingested, skipped, projectRoot)
	}
}

func mcpRunIngestGitPRs(ctx context.Context, actor string, input mcpIngestGitPRsInput) (mcpIngestGitPRsOutput, error) {
	_, projectRoot, ok := findProjectDB()
	if !ok {
		return mcpIngestGitPRsOutput{}, fmt.Errorf("no project (.mnemos/) found — run 'mnemos init' first")
	}
	if !ghAvailable(ctx) {
		return mcpIngestGitPRsOutput{}, fmt.Errorf("gh CLI not installed or not authenticated for github.com")
	}
	conn, err := openConn(ctx)
	if err != nil {
		return mcpIngestGitPRsOutput{}, err
	}
	defer closeConn(conn)

	ingested, skipped, err := ingestGhPRs(ctx, conn, projectRoot, input.Limit, actor)
	if err != nil {
		return mcpIngestGitPRsOutput{}, err
	}
	return mcpIngestGitPRsOutput{Ingested: ingested, Skipped: skipped}, nil
}

func mcpRunIngestGitLog(ctx context.Context, actor string, input mcpIngestGitLogInput) (mcpIngestGitLogOutput, error) {
	_, projectRoot, ok := findProjectDB()
	if !ok {
		return mcpIngestGitLogOutput{}, fmt.Errorf("no project (.mnemos/) found — run 'mnemos init' first")
	}
	if !repoIsGit(projectRoot) {
		return mcpIngestGitLogOutput{}, fmt.Errorf("project root %s is not a git repository", projectRoot)
	}
	conn, err := openConn(ctx)
	if err != nil {
		return mcpIngestGitLogOutput{}, err
	}
	defer closeConn(conn)

	ingested, skipped, err := ingestGitLog(ctx, conn, projectRoot, input.Limit, input.Since, actor)
	if err != nil {
		return mcpIngestGitLogOutput{}, err
	}
	return mcpIngestGitLogOutput{Ingested: ingested, Skipped: skipped}, nil
}

func runAutoIngest(projectRoot, actor string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	conn, err := openConn(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "auto-ingest: failed to open DB: %v\n", err)
		return
	}
	defer closeConn(conn)

	report := autoIngestProjectDocs(ctx, conn, projectRoot, actor)
	if report.Ingested > 0 || report.Skipped > 0 || report.HasFailures() {
		fmt.Fprintf(os.Stderr, "auto-ingest: ingested=%d skipped=%d failed=%d root=%s\n",
			report.Ingested, report.Skipped, len(report.PerFileErrors), projectRoot)
	}
	if report.DedupeFailed {
		fmt.Fprintln(os.Stderr, "auto-ingest: warning — dedupe lookup failed; nothing was ingested to avoid duplicate runs")
	}
	if report.ExtractorError != nil {
		fmt.Fprintf(os.Stderr, "auto-ingest: warning — extractor build failed (%v); nothing was attempted\n", report.ExtractorError)
	}
}

// resolveMCPActor reads MNEMOS_USER_ID at MCP startup. Empty env ->
// SystemUser. Non-empty env -> validated against the local DB so typos
// surface immediately instead of silently stamping a nonexistent user.
// A lookup failure here logs a warning and falls back to SystemUser,
// since the MCP process shouldn't refuse to start over an auth config
// mistake — downstream writes will just carry the fallback attribution.
func resolveMCPActor() string {
	candidate := strings.TrimSpace(os.Getenv("MNEMOS_USER_ID"))
	if candidate == "" {
		return domain.SystemUser
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	db, conn, err := openDB(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mcp: MNEMOS_USER_ID=%s but couldn't open DB to validate: %v — using <system>\n", candidate, err)
		return domain.SystemUser
	}
	defer closeConn(conn)

	actor, err := resolveActor(ctx, db, candidate)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mcp: MNEMOS_USER_ID=%s rejected: %v — using <system>\n", candidate, err)
		return domain.SystemUser
	}
	return actor
}

func mcpRunQuery(ctx context.Context, input mcpQueryInput) (mcpQueryOutput, error) {
	db, conn, err := openDB(ctx)
	if err != nil {
		return mcpQueryOutput{}, err
	}
	defer closeConn(conn)

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

	hops := input.Hops
	if hops < 0 {
		hops = 0
	}
	if hops > 5 {
		hops = 5
	}
	opts := query.AnswerOptions{Hops: hops}

	var answer domain.Answer
	if strings.TrimSpace(input.RunID) != "" {
		answer, err = engine.AnswerForRunWithOptions(strings.TrimSpace(input.Question), strings.TrimSpace(input.RunID), opts)
	} else {
		answer, err = engine.AnswerWithOptions(strings.TrimSpace(input.Question), opts)
	}
	if err != nil {
		return mcpQueryOutput{}, err
	}

	return mcpQueryOutput{
		Answer:           answer.AnswerText,
		Claims:           answer.Claims,
		Contradictions:   answer.Contradictions,
		Timeline:         answer.TimelineEventIDs,
		ClaimProvenance:  answer.ClaimProvenance,
		ClaimHopDistance: answer.ClaimHopDistance,
	}, nil
}

func mcpRunProcessText(ctx context.Context, actor string, input mcpProcessTextInput) (mcpProcessTextOutput, error) {
	service := ingest.NewService()
	normalizer := parser.NewNormalizer()
	progress := mcp.ProgressFromContext(ctx)

	db, conn, err := openDB(ctx)
	if err != nil {
		return mcpProcessTextOutput{}, err
	}
	defer closeConn(conn)

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
		claims, links, mcpEntities, err := ext.ExtractFn(events)
		if err != nil {
			return err
		}
		_ = progress.Report(2, &total)
		// MCP path defers entity materialisation to the
		// post-PersistArtifacts step at the end of this handler so
		// the audit/persist transaction stays focused on artifacts.
		_ = mcpEntities

		if err := job.SetStatus("relating", ""); err != nil {
			return err
		}
		relEngine := relate.NewEngine()
		rels, err := relEngine.Detect(claims)
		if err != nil {
			return err
		}

		existingClaims, err := conn.Claims.ListAll(ctx)
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
		stampEventActor(events, actor)
		stampClaimActor(claims, actor)
		stampRelationshipActor(rels, actor)
		if err := pipeline.PersistArtifacts(ctx, conn, events, claims, links, rels); err != nil {
			return err
		}
		// Best-effort entity materialisation; failures are logged
		// but don't abort the MCP response. The agent caller cares
		// about the answer; entity tagging is enrichment that can
		// be backfilled via `mnemos extract-entities`.
		if _, entErr := pipeline.MaterializeEntities(ctx, conn, mcpEntities, actor); entErr != nil {
			fmt.Fprintf(os.Stderr, "  entity materialisation (mcp) failed: %v\n", entErr)
		}

		embeddingCount := 0
		if input.UseEmbeddings {
			if err := job.SetStatus("embedding", ""); err != nil {
				return err
			}
			embeddingCount, err = pipeline.GenerateEmbeddings(ctx, conn, events)
			if err != nil {
				return err
			}
			claimEmbCount, claimErr := pipeline.GenerateClaimEmbeddings(ctx, conn, claims)
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
	db, conn, err := openDB(context.Background())
	if err != nil {
		return mcpMetricsOutput{}, err
	}
	defer closeConn(conn)

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
