package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/felixgeelhaar/mnemos/internal/domain"
	"github.com/felixgeelhaar/mnemos/internal/embedding"
	"github.com/felixgeelhaar/mnemos/internal/extract"
	"github.com/felixgeelhaar/mnemos/internal/ingest"
	"github.com/felixgeelhaar/mnemos/internal/llm"
	"github.com/felixgeelhaar/mnemos/internal/parser"
	"github.com/felixgeelhaar/mnemos/internal/pipeline"
	"github.com/felixgeelhaar/mnemos/internal/query"
	"github.com/felixgeelhaar/mnemos/internal/relate"
	"github.com/felixgeelhaar/mnemos/internal/store/sqlite"
	"github.com/felixgeelhaar/mnemos/internal/workflow"
)

// resolveDBPath returns the database path in the following precedence:
//  1. MNEMOS_DB_PATH environment variable (explicit override)
//  2. Nearest .mnemos/mnemos.db walking up from the working directory
//  3. XDG-compliant global default (~/.local/share/mnemos/mnemos.db)
func resolveDBPath() string {
	if p := os.Getenv("MNEMOS_DB_PATH"); p != "" {
		return p
	}
	if p, _, ok := findProjectDB(); ok {
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

// findProjectDB walks up from the current working directory looking for a
// .mnemos directory. When found, returns the path to mnemos.db inside it,
// the project root (the directory containing .mnemos), and true. Stops at
// the filesystem root or the user's home directory (whichever is reached
// first) to avoid accidentally adopting a parent project's DB.
func findProjectDB() (dbPath, projectRoot string, ok bool) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", "", false
	}
	home, _ := os.UserHomeDir()
	dir := cwd
	for {
		candidate := filepath.Join(dir, ".mnemos")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return filepath.Join(candidate, "mnemos.db"), dir, true
		}
		if home != "" && dir == home {
			return "", "", false
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", "", false
		}
		dir = parent
	}
}

func printProgress(format string, args ...any) {
	_, _ = fmt.Fprintf(os.Stderr, format+"\n", args...)
}

// newExtractor builds the appropriate extraction engine based on the --llm flag.
func newExtractor(useLLM bool) (*pipeline.Extractor, error) {
	ext, err := pipeline.NewExtractor(useLLM)
	if err != nil {
		return nil, &MnemosError{
			Code:    ExitUsage,
			Message: fmt.Sprintf("LLM configuration error: %s", err),
			Hint:    "Set MNEMOS_LLM_PROVIDER and MNEMOS_LLM_API_KEY environment variables\n  Providers: anthropic, openai, gemini, ollama, openai-compat",
		}
	}
	return ext, nil
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(int(ExitUsage))
	}

	flags, args := ParseFlags(os.Args[1:])

	// Default to human-readable output in interactive terminals.
	if !flags.Human && !flags.JSON {
		if fileInfo, _ := os.Stdout.Stat(); fileInfo != nil && fileInfo.Mode()&os.ModeCharDevice != 0 {
			flags.Human = true
		}
	}

	// Auto-enable LLM and embeddings when Ollama is detected locally
	// and no explicit provider is configured.
	if !flags.LLM && os.Getenv("MNEMOS_LLM_PROVIDER") == "" && llm.OllamaAvailable() {
		flags.LLM = true
		flags.Embed = true
		printProgress("ollama detected: enabling LLM extraction and embeddings")
	}

	if flags.Help {
		printUsage()
		os.Exit(int(ExitSuccess))
	}
	if flags.Version {
		fmt.Printf("mnemos %s (commit %s, built %s)\n", version, commit, buildDate)
		os.Exit(int(ExitSuccess))
	}

	command := args[0]
	args = args[1:]

	switch command {
	case "init":
		handleInit(args, flags)
	case "ingest":
		handleIngest(args, flags)
	case "extract":
		handleExtract(args, flags)
	case "relate":
		handleRelate(args, flags)
	case "process":
		handleProcess(args, flags)
	case "query":
		handleQuery(args, flags)
	case "metrics":
		handleMetrics(flags)
	case "mcp":
		handleMCP()
	case "serve":
		handleServe(args, flags)
	case "registry":
		handleRegistry(args, flags)
	case "push":
		handlePush(args, flags)
	case "pull":
		handlePull(args, flags)
	case "audit":
		handleAudit(args, flags)
	case "resolve":
		handleResolve(args, flags)
	case "user":
		handleUser(args, flags)
	case "token":
		handleToken(args, flags)
	case "agent":
		handleAgent(args, flags)
	case "doctor":
		handleDoctor(args, flags)
	case "reset":
		handleReset(args, flags)
	case "delete-claim":
		handleDeleteClaim(args, flags)
	case "delete-event":
		handleDeleteEvent(args, flags)
	case "reembed":
		handleReembed(args, flags)
	default:
		fmt.Fprintf(os.Stderr, "error: unknown command %q\n", command)
		if suggestion := suggestCommand(command); suggestion != "" {
			fmt.Fprintf(os.Stderr, "  Did you mean %q?\n", suggestion)
		}
		fmt.Fprintln(os.Stderr)
		printUsage()
		os.Exit(int(ExitUsage))
	}
}

func handleInit(args []string, _ Flags) {
	if len(args) > 0 {
		fmt.Fprintln(os.Stderr, "error: init takes no arguments")
		fmt.Fprintln(os.Stderr, "  mnemos init")
		os.Exit(int(ExitUsage))
	}

	cwd, err := os.Getwd()
	if err != nil {
		exitWithMnemosError(false, NewSystemError(err, "failed to determine working directory"))
		return
	}

	dir := filepath.Join(cwd, ".mnemos")
	dbPath := filepath.Join(dir, "mnemos.db")

	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		fmt.Printf("already initialized: %s\n", dir)
		fmt.Printf("db=%s\n", dbPath)
		os.Exit(int(ExitSuccess))
	}

	if err := os.MkdirAll(dir, 0o750); err != nil {
		exitWithMnemosError(false, NewSystemError(err, "failed to create %s", dir))
		return
	}

	fmt.Printf("initialized empty mnemos project in %s\n", dir)
	fmt.Printf("db=%s\n", dbPath)
	fmt.Println("\nNext: 'mnemos process <path>' to ingest content, or 'mnemos mcp' to start the server.")
}

func handleIngest(args []string, f Flags) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "error: ingest requires a file path or --text flag")
		fmt.Fprintln(os.Stderr, "  mnemos ingest <path>")
		fmt.Fprintln(os.Stderr, "  mnemos ingest --text <content>")
		os.Exit(int(ExitUsage))
	}

	service := ingest.NewService()
	normalizer := parser.NewNormalizer()

	if args[0] == "--text" {
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "error: --text flag requires content after it")
			fmt.Fprintln(os.Stderr, "  mnemos ingest --text <content>")
			os.Exit(int(ExitUsage))
		}

		contentArg := strings.Join(args[1:], " ")
		err := runJob("ingest", map[string]string{"source": "raw_text"}, f.Verbose, func(ctx context.Context, job *workflow.Job, db *sql.DB) error {
			actor, err := resolveActor(ctx, db, f.Actor)
			if err != nil {
				return err
			}
			if err := job.SetStatus("loading", ""); err != nil {
				return err
			}
			input, content, err := service.IngestText(contentArg, nil)
			if err != nil {
				return NewSystemError(err, "failed to ingest text")
			}
			if err := job.SetStatus("extracting", ""); err != nil {
				return err
			}
			events, err := normalizer.Normalize(input, content)
			if err != nil {
				return NewSystemError(err, "failed to normalize text")
			}
			for i := range events {
				events[i].RunID = job.ID()
			}
			stampEventActor(events, actor)
			if err := job.SetStatus("saving", ""); err != nil {
				return err
			}
			repo := sqlite.NewEventRepository(db)
			for _, event := range events {
				if err := repo.Append(ctx, event); err != nil {
					return NewSystemError(err, "failed to persist event %s", event.ID)
				}
			}
			fmt.Printf("run_id=%s input=%s type=%s format=%s bytes=%d events=%d db=%s source=%s\n", job.ID(), input.ID, input.Type, input.Format, len(content), len(events), resolveDBPath(), input.Metadata["source"])
			printIngestHint(job.ID())
			return nil
		})
		exitWithMnemosError(f.Verbose, err)
		return
	}

	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "error: ingest accepts exactly one path argument")
		fmt.Fprintln(os.Stderr, "  mnemos ingest <path>")
		os.Exit(int(ExitUsage))
	}

	path := args[0]
	err := runJob("ingest", map[string]string{"path": path}, f.Verbose, func(ctx context.Context, job *workflow.Job, db *sql.DB) error {
		actor, err := resolveActor(ctx, db, f.Actor)
		if err != nil {
			return err
		}
		if err := job.SetStatus("loading", ""); err != nil {
			return err
		}
		input, content, err := service.IngestFile(path)
		if err != nil {
			return NewSystemError(err, "failed to read file %q", path)
		}
		if err := job.SetStatus("extracting", ""); err != nil {
			return err
		}
		events, err := normalizer.Normalize(input, content)
		if err != nil {
			return NewSystemError(err, "failed to normalize content")
		}
		for i := range events {
			events[i].RunID = job.ID()
		}
		stampEventActor(events, actor)
		if err := job.SetStatus("saving", ""); err != nil {
			return err
		}
		repo := sqlite.NewEventRepository(db)
		for _, event := range events {
			if err := repo.Append(ctx, event); err != nil {
				return NewSystemError(err, "failed to persist event %s", event.ID)
			}
		}
		fmt.Printf("run_id=%s input=%s type=%s format=%s bytes=%d events=%d db=%s source=%s\n", job.ID(), input.ID, input.Type, input.Format, len(content), len(events), resolveDBPath(), input.Metadata["source_path"])
		printIngestHint(job.ID())
		return nil
	})
	exitWithMnemosError(f.Verbose, err)
}

func handleQuery(args []string, f Flags) {
	question, runID, hops, err := parseQueryArgs(args)
	if err != nil {
		exitWithMnemosError(f.Verbose, err)
	}

	scope := map[string]string{"question": question}
	if runID != "" {
		scope["run_id"] = runID
	}
	if hops > 0 {
		scope["hops"] = strconv.Itoa(hops)
	}

	err = runJob("query", scope, f.Verbose, func(ctx context.Context, job *workflow.Job, db *sql.DB) error {
		if err := job.SetStatus("loading", ""); err != nil {
			return err
		}
		engine := query.NewEngine(
			sqlite.NewEventRepository(db),
			sqlite.NewClaimRepository(db),
			sqlite.NewRelationshipRepository(db),
		)

		if f.Embed {
			printProgress("semantic search: preparing query embeddings")
			embCfg, err := embedding.ConfigFromEnv()
			if err != nil {
				printProgress("warning: --embed requested but embedding config failed: %v (falling back to keyword matching)", err)
			} else {
				embClient, err := embedding.NewClient(embCfg)
				if err != nil {
					printProgress("warning: --embed requested but embedding client failed: %v (falling back to keyword matching)", err)
				} else {
					engine = engine.WithEmbeddings(
						sqlite.NewEmbeddingRepository(db),
						embClient,
					)
				}
			}
		}

		if f.LLM {
			llmCfg, err := llm.ConfigFromEnv()
			if err == nil {
				llmClient, err := llm.NewClient(llmCfg)
				if err == nil {
					engine = engine.WithLLM(llmClient)
					printProgress("grounded generation: using %s for answer synthesis", llmCfg.Provider)
				}
			}
		}

		if statusErr := job.SetStatus("querying", ""); statusErr != nil {
			return statusErr
		}
		var answer domain.Answer
		var queryErr error
		opts := query.AnswerOptions{Hops: hops}
		if runID != "" {
			answer, queryErr = engine.AnswerForRunWithOptions(question, runID, opts)
		} else {
			answer, queryErr = engine.AnswerWithOptions(question, opts)
		}
		if queryErr != nil {
			return NewSystemError(queryErr, "query engine failed")
		}

		if f.Human {
			// Resolve claim text for contradiction display — some
			// contradictions may reference claims outside the answer set.
			resolveContradictionClaimText(ctx, db, &answer)
			printHumanReadableAnswer(question, answer)
		} else {
			response := map[string]any{
				"answer":         answer.AnswerText,
				"claims":         answer.Claims,
				"contradictions": answer.Contradictions,
				"timeline":       answer.TimelineEventIDs,
			}
			encoded, err := json.MarshalIndent(response, "", "  ")
			if err != nil {
				return NewSystemError(err, "failed to encode response")
			}
			fmt.Println(string(encoded))
		}
		return nil
	})
	exitWithMnemosError(f.Verbose, err)
}

// resolveContradictionClaimText ensures all claims referenced in contradictions
// have their text available in the answer's claim set for display purposes.
func resolveContradictionClaimText(ctx context.Context, db *sql.DB, answer *domain.Answer) {
	if len(answer.Contradictions) == 0 {
		return
	}

	known := make(map[string]struct{}, len(answer.Claims))
	for _, c := range answer.Claims {
		known[c.ID] = struct{}{}
	}

	hasMissing := false
	for _, rel := range answer.Contradictions {
		if _, ok := known[rel.FromClaimID]; !ok {
			hasMissing = true
			break
		}
		if _, ok := known[rel.ToClaimID]; !ok {
			hasMissing = true
			break
		}
	}
	if !hasMissing {
		return
	}

	claimRepo := sqlite.NewClaimRepository(db)
	allClaims, err := claimRepo.ListAll(ctx)
	if err != nil {
		return
	}
	byID := make(map[string]domain.Claim, len(allClaims))
	for _, c := range allClaims {
		byID[c.ID] = c
	}
	for _, rel := range answer.Contradictions {
		if _, ok := known[rel.FromClaimID]; !ok {
			if c, found := byID[rel.FromClaimID]; found {
				answer.Claims = append(answer.Claims, c)
				known[rel.FromClaimID] = struct{}{}
			}
		}
		if _, ok := known[rel.ToClaimID]; !ok {
			if c, found := byID[rel.ToClaimID]; found {
				answer.Claims = append(answer.Claims, c)
				known[rel.ToClaimID] = struct{}{}
			}
		}
	}
}

func printHumanReadableAnswer(question string, answer domain.Answer) {
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("  Question: %s\n", question)
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("")

	if answer.AnswerText != "" && answer.AnswerText != "No ingested events yet." {
		fmt.Printf("  %s\n\n", answer.AnswerText)
	}

	if len(answer.Claims) > 0 {
		fmt.Println("  Evidence:")
		for i, claim := range answer.Claims {
			typeLabel := "Fact"
			switch claim.Type {
			case domain.ClaimTypeDecision:
				typeLabel = "Decision"
			case domain.ClaimTypeHypothesis:
				typeLabel = "Hypothesis"
			}

			status := ""
			if claim.Status == domain.ClaimStatusContested {
				status = " ⚠️  CONFLICT"
			}

			fmt.Printf("  %d. [%s] %s%s\n", i+1, typeLabel, claim.Text, status)
		}
		fmt.Println("")
	}

	if len(answer.Contradictions) > 0 {
		// Build claim text lookup for human-readable contradiction output.
		claimText := make(map[string]string, len(answer.Claims))
		for _, c := range answer.Claims {
			claimText[c.ID] = c.Text
		}

		fmt.Println("  Contradictions detected:")
		for i, rel := range answer.Contradictions {
			if rel.Type == domain.RelationshipTypeContradicts {
				from := claimText[rel.FromClaimID]
				if from == "" {
					from = rel.FromClaimID
				}
				to := claimText[rel.ToClaimID]
				if to == "" {
					to = rel.ToClaimID
				}
				fmt.Printf("  %d. \"%s\" contradicts \"%s\"\n", i+1, from, to)
			}
		}
		fmt.Println("")
	}

	if len(answer.Claims) == 0 && answer.AnswerText == "No ingested events yet." {
		fmt.Println("  No knowledge found yet.")
		fmt.Println("")
		fmt.Println("  Tip: Run 'mnemos process --text <your text>' to add knowledge")
	}
}

func handleExtract(args []string, f Flags) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "error: extract requires event IDs or --run flag")
		fmt.Fprintln(os.Stderr, "  mnemos extract <event-id> [event-id ...]")
		fmt.Fprintln(os.Stderr, "  mnemos extract --run <run-id>")
		os.Exit(int(ExitUsage))
	}

	// Parse --run flag for run-scoped extraction.
	var runID string
	eventIDs := args
	if args[0] == "--run" {
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "error: --run flag requires a run ID")
			os.Exit(int(ExitUsage))
		}
		runID = args[1]
		eventIDs = args[2:]
	}

	scope := map[string]string{}
	if runID != "" {
		scope["run_id"] = runID
	} else {
		scope["event_ids"] = strings.Join(eventIDs, ",")
	}

	err := runJob("extract", scope, f.Verbose, func(ctx context.Context, job *workflow.Job, db *sql.DB) error {
		actor, actorErr := resolveActor(ctx, db, f.Actor)
		if actorErr != nil {
			return actorErr
		}
		if err := job.SetStatus("loading", ""); err != nil {
			return err
		}
		eventRepo := sqlite.NewEventRepository(db)

		var events []domain.Event
		var err error
		if runID != "" {
			events, err = eventRepo.ListByRunID(ctx, runID)
		} else {
			events, err = eventRepo.ListByIDs(ctx, eventIDs)
		}
		if err != nil {
			return NewSystemError(err, "database lookup failed")
		}
		if len(events) == 0 {
			hint := "Tip: Run 'mnemos ingest <file>' or 'mnemos process --text <text>' first"
			if runID != "" {
				return &MnemosError{Code: ExitNotFound, Message: fmt.Sprintf("no events found for run %q", runID), Hint: hint}
			}
			return &MnemosError{Code: ExitNotFound, Message: fmt.Sprintf("no events found for the provided IDs (%d given)", len(eventIDs)), Hint: hint}
		}

		if err := job.SetStatus("extracting", ""); err != nil {
			return err
		}
		if f.LLM {
			printProgress("llm extraction: sending content to %s", os.Getenv("MNEMOS_LLM_PROVIDER"))
		}
		ext, err := newExtractor(f.LLM)
		if err != nil {
			return err
		}
		claims, links, err := ext.ExtractFn(events)
		if err != nil {
			return NewSystemError(err, "extraction failed")
		}
		if f.LLM {
			printProgress("llm extraction: extracted %d claims", len(claims))
		}

		if err := job.SetStatus("saving", ""); err != nil {
			return err
		}
		stampClaimActor(claims, actor)
		claimRepo := sqlite.NewClaimRepository(db)
		if err := claimRepo.Upsert(ctx, claims); err != nil {
			return NewSystemError(err, "failed to persist claims")
		}
		if err := claimRepo.UpsertEvidence(ctx, links); err != nil {
			return NewSystemError(err, "failed to persist claim evidence links")
		}

		fmt.Printf("events=%d claims=%d evidence_links=%d db=%s\n", len(events), len(claims), len(links), resolveDBPath())
		return nil
	})
	exitWithMnemosError(f.Verbose, err)
}

func handleRelate(args []string, f Flags) {
	err := runJob("relate", map[string]string{"event_ids": strings.Join(args, ",")}, f.Verbose, func(ctx context.Context, job *workflow.Job, db *sql.DB) error {
		actor, actorErr := resolveActor(ctx, db, f.Actor)
		if actorErr != nil {
			return actorErr
		}
		if err := job.SetStatus("loading", ""); err != nil {
			return err
		}
		claimRepo := sqlite.NewClaimRepository(db)
		relRepo := sqlite.NewRelationshipRepository(db)

		var claims []domain.Claim
		var err error
		if len(args) == 0 {
			claims, err = claimRepo.ListAll(ctx)
		} else {
			claims, err = claimRepo.ListByEventIDs(ctx, args)
		}
		if err != nil {
			return NewSystemError(err, "database lookup failed")
		}
		if len(claims) < 2 {
			return &MnemosError{
				Code:    ExitUsage,
				Message: fmt.Sprintf("need at least 2 claims to detect relationships (found %d)", len(claims)),
				Hint:    "Tip: Run 'mnemos ingest' followed by 'mnemos extract' to add more claims",
			}
		}

		if err := job.SetStatus("relating", ""); err != nil {
			return err
		}
		engine := relate.NewEngine()
		rels, err := engine.Detect(claims)
		if err != nil {
			return NewSystemError(err, "relationship detection failed")
		}
		if err := job.SetStatus("saving", ""); err != nil {
			return err
		}
		stampRelationshipActor(rels, actor)
		if err := relRepo.Upsert(ctx, rels); err != nil {
			return NewSystemError(err, "failed to persist relationships")
		}

		fmt.Printf("claims=%d relationships=%d db=%s\n", len(claims), len(rels), resolveDBPath())
		return nil
	})
	exitWithMnemosError(f.Verbose, err)
}

func handleProcess(args []string, f Flags) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "error: process requires a file path or --text flag")
		fmt.Fprintln(os.Stderr, "  mnemos process <path>")
		fmt.Fprintln(os.Stderr, "  mnemos process --text <content>")
		os.Exit(int(ExitUsage))
	}

	service := ingest.NewService()
	normalizer := parser.NewNormalizer()

	scope := map[string]string{"source": "file"}
	if args[0] == "--text" {
		scope = map[string]string{"source": "raw_text"}
	}

	err := runJob("process", scope, f.Verbose, func(ctx context.Context, job *workflow.Job, db *sql.DB) error {
		actor, actorErr := resolveActor(ctx, db, f.Actor)
		if actorErr != nil {
			return actorErr
		}
		if err := job.SetStatus("loading", ""); err != nil {
			return err
		}

		var (
			input   domain.Input
			content string
			err     error
		)

		if args[0] == "--text" {
			if len(args) < 2 {
				return NewUserError("--text flag requires content after it")
			}
			input, content, err = service.IngestText(strings.Join(args[1:], " "), nil)
		} else {
			if len(args) != 1 {
				return NewUserError("process accepts exactly one path argument")
			}
			input, content, err = service.IngestFile(args[0])
		}
		if err != nil {
			return NewSystemError(err, "failed to read input")
		}

		if err := job.SetStatus("extracting", ""); err != nil {
			return err
		}
		events, err := normalizer.Normalize(input, content)
		if err != nil {
			return NewSystemError(err, "normalization failed")
		}
		for i := range events {
			events[i].RunID = job.ID()
		}

		ext, err := newExtractor(f.LLM)
		if err != nil {
			return err
		}
		if f.LLM {
			printProgress("llm extraction: sending content to %s", os.Getenv("MNEMOS_LLM_PROVIDER"))
		}
		claims, links, err := ext.ExtractFn(events)
		if err != nil {
			return NewSystemError(err, "claim extraction failed")
		}
		if f.LLM {
			printProgress("llm extraction: extracted %d claims", len(claims))
		}

		if err := job.SetStatus("relating", ""); err != nil {
			return err
		}
		relEngine := relate.NewEngine()
		rels, err := relEngine.Detect(claims)
		if err != nil {
			return NewSystemError(err, "relationship detection failed")
		}

		// Incremental: compare new claims against previously stored claims.
		// Any failure here is surfaced as a warning rather than aborting the
		// whole job — extraction succeeded, the user's claims should be
		// persisted, and the next run will pick up the missing edges.
		// Skipped entirely when --no-relate is set.
		if !f.NoRelate {
			claimRepo := sqlite.NewClaimRepository(db)
			loadCtx, loadCancel := context.WithTimeout(ctx, 30*time.Second)
			existingClaims, loadErr := claimRepo.ListAll(loadCtx)
			loadCancel()
			switch {
			case loadErr != nil:
				warnRelateSkipped(loadErr, "loading existing claims")
			case len(existingClaims) > 0:
				incrementalRels, incErr := relEngine.DetectIncremental(claims, existingClaims)
				if incErr != nil {
					warnRelateSkipped(incErr, "comparing against existing claims")
				} else {
					rels = append(rels, incrementalRels...)
				}
			}
		}

		if err := job.SetStatus("saving", ""); err != nil {
			return err
		}
		stampEventActor(events, actor)
		stampClaimActor(claims, actor)
		stampRelationshipActor(rels, actor)
		if err := pipeline.PersistArtifacts(ctx, db, events, claims, links, rels); err != nil {
			return err
		}

		if f.Embed {
			if err := job.SetStatus("embedding", ""); err != nil {
				return err
			}
			printProgress("embedding: generating vectors with %s", os.Getenv("MNEMOS_EMBED_PROVIDER"))
			if n, err := pipeline.GenerateEmbeddings(ctx, db, events); err != nil {
				// Embedding failure is non-fatal but should be prominent since --embed was explicit.
				warn := icon("⚠️", "(!)")
				fmt.Fprintf(os.Stderr, "\n  %s Event embedding failed: %v\n", warn, err)
				fmt.Fprintf(os.Stderr, "  Queries will fall back to keyword matching instead of semantic search.\n")
				fmt.Fprintf(os.Stderr, "  Check MNEMOS_EMBED_PROVIDER and MNEMOS_EMBED_API_KEY.\n\n")
			} else {
				printProgress("embedding: generated %d event vectors", n)
			}

			if nc, err := pipeline.GenerateClaimEmbeddings(ctx, db, claims); err != nil {
				warn := icon("⚠️", "(!)")
				fmt.Fprintf(os.Stderr, "\n  %s Claim embedding failed: %v\n", warn, err)
			} else {
				printProgress("embedding: generated %d claim vectors", nc)
			}
		}

		fmt.Printf("Run ID: %s\n", job.ID())
		fmt.Printf("Processed %d event(s) into %d claim(s).\n", len(events), len(claims))

		printExtractionSummary(claims, rels)
		if len(claims) > 0 {
			printClaimPreview(claims, 3)
		}

		return nil
	})
	exitWithMnemosError(f.Verbose, err)
}

func handleMetrics(f Flags) {
	err := runJob("metrics", map[string]string{}, f.Verbose, func(ctx context.Context, job *workflow.Job, db *sql.DB) error {
		if err := job.SetStatus("loading", ""); err != nil {
			return err
		}

		metrics := map[string]any{
			"runs":                countValue(db, `SELECT COUNT(DISTINCT run_id) FROM events WHERE run_id <> ''`),
			"events":              countValue(db, `SELECT COUNT(*) FROM events`),
			"claims":              countValue(db, `SELECT COUNT(*) FROM claims`),
			"contested_claims":    countValue(db, `SELECT COUNT(*) FROM claims WHERE status = 'contested'`),
			"relationships":       countValue(db, `SELECT COUNT(*) FROM relationships`),
			"contradictions":      countValue(db, `SELECT COUNT(*) FROM relationships WHERE type = 'contradicts'`),
			"embeddings":          countValue(db, `SELECT COUNT(*) FROM embeddings`),
			"llm_cache_entries":   cacheEntryCount(),
			"prompt_version":      extract.PromptVersion,
			"eval_cases":          78,
			"llm_eval_configured": strings.TrimSpace(os.Getenv("MNEMOS_LLM_PROVIDER")) != "",
		}

		if f.Human {
			fmt.Println("Mnemos Metrics")
			fmt.Printf("Runs: %v\n", metrics["runs"])
			fmt.Printf("Events: %v\n", metrics["events"])
			fmt.Printf("Claims: %v\n", metrics["claims"])
			fmt.Printf("Contested claims: %v\n", metrics["contested_claims"])
			fmt.Printf("Relationships: %v\n", metrics["relationships"])
			fmt.Printf("Contradictions: %v\n", metrics["contradictions"])
			fmt.Printf("Embeddings: %v\n", metrics["embeddings"])
			fmt.Printf("LLM cache entries: %v\n", metrics["llm_cache_entries"])
			fmt.Printf("Eval cases: %v\n", metrics["eval_cases"])
			fmt.Printf("Prompt version: %v\n", metrics["prompt_version"])
			return nil
		}

		encoded, err := json.MarshalIndent(metrics, "", "  ")
		if err != nil {
			return NewSystemError(err, "failed to encode metrics")
		}
		fmt.Println(string(encoded))
		return nil
	})
	exitWithMnemosError(f.Verbose, err)
}

func countValue(db *sql.DB, query string) int64 {
	var value int64
	if err := db.QueryRow(query).Scan(&value); err != nil {
		return 0
	}
	return value
}

func cacheEntryCount() int {
	entries, err := os.ReadDir(filepath.Join("data", "cache", "llm-extraction"))
	if err != nil {
		return 0
	}
	count := 0
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
			count++
		}
	}
	return count
}

func printUsage() {
	fmt.Println("Mnemos CLI — local-first knowledge engine")
	fmt.Println("")
	fmt.Println("Quick Start:")
	fmt.Println("  mnemos init                          Create .mnemos/ in the current directory")
	fmt.Println("  mnemos process --text \"Your text here\"")
	fmt.Println("  mnemos query --human \"Your question\"")
	fmt.Println("")
	fmt.Println("Pipeline Commands:")
	fmt.Println("  ingest <path>                        Ingest a file as events")
	fmt.Println("  ingest --text <content>              Ingest raw text as events")
	fmt.Println("  extract <event-id> [event-id ...]    Extract claims from events")
	fmt.Println("  extract --run <run-id>               Extract claims from all events in a run")
	fmt.Println("  relate [event-id ...]                Detect relationships between claims")
	fmt.Println("")
	fmt.Println("All-in-One:")
	fmt.Println("  process <path>                       Ingest + extract + relate in one step")
	fmt.Println("  process --text <content>             Same, from raw text")
	fmt.Println("  process --llm <path>                 Use LLM-powered extraction")
	fmt.Println("  process --llm --embed <path>         LLM extraction + embeddings")
	fmt.Println("")
	fmt.Println("Query & Reporting:")
	fmt.Println("  query [--run <run-id>] <question>    Query with evidence")
	fmt.Println("  query --hops <N> <question>          Expand result claims via N hops of supports/contradicts")
	fmt.Println("  query --llm <question>               Query with LLM-grounded answer")
	fmt.Println("  metrics [--human]                    Knowledge base statistics")
	fmt.Println("  audit [--include-embeddings]         Export the full knowledge base as JSON")
	fmt.Println("  resolve <winner> --over <loser>      Resolve a contradiction: winner → resolved, loser → deprecated")
	fmt.Println("")
	fmt.Println("Identity:")
	fmt.Println("  user create --name <n> --email <e>   Create a user identity")
	fmt.Println("  user list                            List users")
	fmt.Println("  user revoke <id>                     Revoke a user (soft delete)")
	fmt.Println("  token issue --user <id> [--ttl <d>]  Mint a JWT for a user (default ttl 90 days)")
	fmt.Println("  token revoke <jti>                   Add a JWT's jti to the denylist")
	fmt.Println("  mcp                                  Start MCP server over stdio")
	fmt.Println("  serve [--port <n>]                   Start HTTP registry server (default :7777)")
	fmt.Println("")
	fmt.Println("Registry Sync:")
	fmt.Println("  registry connect <url> [--token T]   Wire this project to a remote registry")
	fmt.Println("  push [--url U] [--token T]           Send local knowledge to the registry")
	fmt.Println("  pull [--url U] [--token T]           Fetch knowledge from the registry")
	fmt.Println("")
	fmt.Println("Maintenance:")
	fmt.Println("  reset [--keep-events] [--yes]        Wipe claims/relationships/embeddings (events optional)")
	fmt.Println("  delete-claim <id> [<id>...]          Delete specific claims and their derived state")
	fmt.Println("  delete-event <id> [<id>...]          Delete events and cascade to derived claims")
	fmt.Println("  reembed [--force] [--dry-run]        (Re)generate claim embeddings under the current embed config")
	fmt.Println("")
	fmt.Println("Flags:")
	fmt.Println("  -h, --help     show this help message")
	fmt.Println("  --version      print version and exit")
	fmt.Println("  -v, --verbose  show detailed error output")
	fmt.Println("  --human        human-readable output (default: JSON)")
	fmt.Println("  --json         force JSON output (default for non-query commands)")
	fmt.Println("  --llm          use LLM-powered extraction (requires MNEMOS_LLM_PROVIDER)")
	fmt.Println("  --embed        generate embeddings for semantic search")
	fmt.Println("  --no-relate    skip the relate stage in 'process' (faster ingest, no cross-claim edges)")
	fmt.Println("  --force        with reembed: re-embed all claims, not just those missing")
	fmt.Println("  --dry-run      with reembed: report what would change without writing")
	fmt.Println("  -y, --yes      with reset: skip the confirmation prompt")
	fmt.Println("")
	fmt.Println("Environment Variables:")
	fmt.Println("  MNEMOS_DB_PATH         database path (overrides project and global defaults)")
	fmt.Println("                         resolution order: env → ./.mnemos/mnemos.db (walked up) → ~/.local/share/mnemos/mnemos.db")
	fmt.Println("  MNEMOS_LLM_PROVIDER    anthropic, openai, gemini, ollama, openai-compat")
	fmt.Println("  MNEMOS_LLM_API_KEY     API key (required for cloud providers)")
	fmt.Println("  MNEMOS_LLM_MODEL       model override (optional)")
	fmt.Println("  MNEMOS_LLM_BASE_URL    custom endpoint")
	fmt.Println("                         - required for openai-compat")
	fmt.Println("                         - required for ollama when not on the same host as Mnemos")
	fmt.Println("                           (e.g. Mnemos in a container, Ollama on the host:")
	fmt.Println("                            set http://host.docker.internal:11434 on Docker Desktop)")
	fmt.Println("                         - defaults to http://localhost:11434 for ollama")
	fmt.Println("  MNEMOS_LLM_TIMEOUT     per-request LLM HTTP timeout (default 120s; e.g. 60s, 5m)")
	fmt.Println("  MNEMOS_EXTRACT_MODEL   override MNEMOS_LLM_MODEL just for the extract stage")
	fmt.Println("  MNEMOS_JOB_TIMEOUT     overall job deadline (default 10m; raise for slow local LLMs)")
	fmt.Println("  MNEMOS_EMBED_PROVIDER  embedding provider (falls back to LLM provider)")
	fmt.Println("  MNEMOS_EMBED_API_KEY   embedding API key (falls back to LLM key)")
	fmt.Println("  MNEMOS_EMBED_MODEL     embedding model override (optional)")
	fmt.Println("  MNEMOS_EMBED_BASE_URL  embedding endpoint (optional; same container note as MNEMOS_LLM_BASE_URL)")
	fmt.Println("  MNEMOS_EMBED_TIMEOUT   per-request embedding HTTP timeout (default 60s)")
}

func parseQueryArgs(args []string) (string, string, int, error) {
	if len(args) == 0 {
		return "", "", 0, NewUserError("query requires a question")
	}

	runID := ""
	hops := 0
	questionArgs := args
	for len(questionArgs) > 0 {
		switch questionArgs[0] {
		case "--run":
			if len(questionArgs) < 3 {
				return "", "", 0, NewUserError("--run flag requires <run-id> followed by a question")
			}
			runID = strings.TrimSpace(questionArgs[1])
			if runID == "" {
				return "", "", 0, NewUserError("--run flag requires a non-empty run-id")
			}
			questionArgs = questionArgs[2:]
		case "--hops":
			if len(questionArgs) < 2 {
				return "", "", 0, NewUserError("--hops flag requires a value")
			}
			n, err := strconv.Atoi(questionArgs[1])
			if err != nil || n < 0 || n > 5 {
				return "", "", 0, NewUserError("--hops must be an integer in [0, 5]")
			}
			hops = n
			questionArgs = questionArgs[2:]
		default:
			goto done
		}
	}
done:

	question := strings.TrimSpace(strings.Join(questionArgs, " "))
	if question == "" {
		return "", "", 0, NewUserError("query requires a question")
	}

	return question, runID, hops, nil
}

// warnRelateSkipped surfaces an incremental-relate failure as a warning
// rather than a fatal error. Distinguishes a deadline-exceeded cause —
// usually upstream budget exhaustion, not a real DB problem — from other
// failures, and points users at the right knobs.
func warnRelateSkipped(err error, stage string) {
	warn := icon("⚠️", "(!)")
	if errors.Is(err, context.DeadlineExceeded) {
		fmt.Fprintf(os.Stderr, "\n  %s Skipped incremental relate (%s): job deadline exceeded.\n", warn, stage)
		fmt.Fprintf(os.Stderr, "    Extracted claims have been persisted; cross-run edges will be picked up next time.\n")
		fmt.Fprintf(os.Stderr, "    Tune MNEMOS_JOB_TIMEOUT (default 10m) or MNEMOS_LLM_TIMEOUT (default 120s) for slower providers,\n")
		fmt.Fprintf(os.Stderr, "    or pass --no-relate to skip this stage entirely.\n\n")
		return
	}
	fmt.Fprintf(os.Stderr, "\n  %s Skipped incremental relate (%s): %v\n", warn, stage, err)
	fmt.Fprintf(os.Stderr, "    Extracted claims have been persisted; cross-run edges will be picked up next time.\n\n")
}

// jobTimeout returns the per-job workflow timeout, honoring MNEMOS_JOB_TIMEOUT.
// Defaults to 10 minutes — generous enough for local-LLM extraction over
// many events. The previous 20s default forced the downstream relate-stage
// DB read onto an exhausted ctx, surfacing as the misleading "failed to
// load existing claims: list all claims: context deadline exceeded".
func jobTimeout() time.Duration {
	raw := strings.TrimSpace(os.Getenv("MNEMOS_JOB_TIMEOUT"))
	if raw == "" {
		return 10 * time.Minute
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		fmt.Fprintf(os.Stderr, "warning: invalid MNEMOS_JOB_TIMEOUT=%q (want 60s, 5m, etc.); using 10m\n", raw)
		return 10 * time.Minute
	}
	return d
}

func runJob(kind string, scope map[string]string, verbose bool, fn func(context.Context, *workflow.Job, *sql.DB) error) error {
	dbPath := resolveDBPath()

	if isFirstRun(dbPath) && kind != "ingest" && kind != "process" {
		printWelcome()
		fmt.Println("  First run detected. Use 'process' or 'ingest' to add knowledge.")
		printFirstRunHints()
	}

	db, err := sqlite.Open(dbPath)
	if err != nil {
		return NewSystemError(err, "failed to open database at %q", dbPath)
	}
	defer closeDB(db)

	runner := workflow.NewRunner(sqlite.NewCompilationJobRepository(db))
	runner.Timeout = jobTimeout()
	runner.MaxRetries = 1
	runner.Verbose = verbose

	jobErr := runner.Run(kind, scope, func(ctx context.Context, job *workflow.Job) error {
		return fn(ctx, job, db)
	})
	return jobErr
}
