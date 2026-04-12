package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/felixgeelhaar/mnemos/internal/domain"
	"github.com/felixgeelhaar/mnemos/internal/extract"
	"github.com/felixgeelhaar/mnemos/internal/ingest"
	"github.com/felixgeelhaar/mnemos/internal/parser"
	"github.com/felixgeelhaar/mnemos/internal/query"
	"github.com/felixgeelhaar/mnemos/internal/relate"
	"github.com/felixgeelhaar/mnemos/internal/store/sqlite"
	"github.com/felixgeelhaar/mnemos/internal/store/sqlite/sqlcgen"
	"github.com/felixgeelhaar/mnemos/internal/workflow"
)

const defaultDBPath = "data/mnemos.db"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(int(ExitUsage))
	}

	flags, args := ParseFlags(os.Args[1:])
	if flags.Help {
		printUsage()
		os.Exit(int(ExitSuccess))
	}

	command := args[0]
	args = args[1:]

	switch command {
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
	default:
		fmt.Fprintf(os.Stderr, "error: unknown command %q\n\n", command)
		printUsage()
		os.Exit(int(ExitUsage))
	}
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
		err := runJob("ingest", map[string]string{"source": "raw_text"}, func(_ context.Context, job *workflow.Job, db *sql.DB) error {
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
			if err := job.SetStatus("saving", ""); err != nil {
				return err
			}
			repo := sqlite.NewEventRepository(db)
			for _, event := range events {
				if err := repo.Append(event); err != nil {
					return NewSystemError(err, "failed to persist event %s", event.ID)
				}
			}
			fmt.Printf("run_id=%s input=%s type=%s format=%s bytes=%d events=%d db=%s source=%s\n", job.ID(), input.ID, input.Type, input.Format, len(content), len(events), defaultDBPath, input.Metadata["source"])
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
	err := runJob("ingest", map[string]string{"path": path}, func(_ context.Context, job *workflow.Job, db *sql.DB) error {
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
		if err := job.SetStatus("saving", ""); err != nil {
			return err
		}
		repo := sqlite.NewEventRepository(db)
		for _, event := range events {
			if err := repo.Append(event); err != nil {
				return NewSystemError(err, "failed to persist event %s", event.ID)
			}
		}
		fmt.Printf("run_id=%s input=%s type=%s format=%s bytes=%d events=%d db=%s source=%s\n", job.ID(), input.ID, input.Type, input.Format, len(content), len(events), defaultDBPath, input.Metadata["source_path"])
		return nil
	})
	exitWithMnemosError(f.Verbose, err)
}

func handleQuery(args []string, f Flags) {
	question, runID, err := parseQueryArgs(args)
	if err != nil {
		exitWithMnemosError(f.Verbose, err)
	}

	scope := map[string]string{"question": question}
	if runID != "" {
		scope["run_id"] = runID
	}

	err = runJob("query", scope, func(_ context.Context, job *workflow.Job, db *sql.DB) error {
		if err := job.SetStatus("loading", ""); err != nil {
			return err
		}
		engine := query.NewEngine(
			sqlite.NewEventRepository(db),
			sqlite.NewClaimRepository(db),
			sqlite.NewRelationshipRepository(db),
		)
		if err := job.SetStatus("querying", ""); err != nil {
			return err
		}
		var answer domain.Answer
		var err error
		if runID != "" {
			answer, err = engine.AnswerForRun(question, runID)
		} else {
			answer, err = engine.Answer(question)
		}
		if err != nil {
			return NewSystemError(err, "query engine failed")
		}

		if f.Human {
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
		fmt.Println("  ⚠️  Contradictions detected:")
		for i, rel := range answer.Contradictions {
			if rel.Type == domain.RelationshipTypeContradicts {
				fmt.Printf("  %d. %s contradicts %s\n", i+1, rel.FromClaimID, rel.ToClaimID)
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
		fmt.Fprintln(os.Stderr, "error: extract requires one or more event IDs")
		fmt.Fprintln(os.Stderr, "  mnemos extract <event-id> [event-id ...]")
		fmt.Fprintln(os.Stderr, "  mnemos extract ev_abc123 ev_def456")
		os.Exit(int(ExitUsage))
	}

	err := runJob("extract", map[string]string{"event_ids": strings.Join(args, ",")}, func(_ context.Context, job *workflow.Job, db *sql.DB) error {
		if err := job.SetStatus("loading", ""); err != nil {
			return err
		}
		eventRepo := sqlite.NewEventRepository(db)
		events, err := eventRepo.ListByIDs(args)
		if err != nil {
			return NewSystemError(err, "database lookup failed")
		}
		if len(events) == 0 {
			return &MnemosError{
				Code:    ExitNotFound,
				Message: fmt.Sprintf("no events found for the provided IDs (%d given)", len(args)),
				Hint:    "Tip: Run 'mnemos ingest <file>' or 'mnemos process --text <text>' first",
			}
		}

		if err := job.SetStatus("extracting", ""); err != nil {
			return err
		}
		engine := extract.NewEngine()
		claims, links, err := engine.Extract(events)
		if err != nil {
			return NewSystemError(err, "extraction failed")
		}

		if err := job.SetStatus("saving", ""); err != nil {
			return err
		}
		claimRepo := sqlite.NewClaimRepository(db)
		if err := claimRepo.Upsert(claims); err != nil {
			return NewSystemError(err, "failed to persist claims")
		}
		if err := claimRepo.UpsertEvidence(links); err != nil {
			return NewSystemError(err, "failed to persist claim evidence links")
		}

		fmt.Printf("events=%d claims=%d evidence_links=%d db=%s\n", len(events), len(claims), len(links), defaultDBPath)
		return nil
	})
	exitWithMnemosError(f.Verbose, err)
}

func handleRelate(args []string, f Flags) {
	err := runJob("relate", map[string]string{"event_ids": strings.Join(args, ",")}, func(_ context.Context, job *workflow.Job, db *sql.DB) error {
		if err := job.SetStatus("loading", ""); err != nil {
			return err
		}
		claimRepo := sqlite.NewClaimRepository(db)
		relRepo := sqlite.NewRelationshipRepository(db)

		var claims []domain.Claim
		var err error
		if len(args) == 0 {
			claims, err = claimRepo.ListAll()
		} else {
			claims, err = claimRepo.ListByEventIDs(args)
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
		if err := relRepo.Upsert(rels); err != nil {
			return NewSystemError(err, "failed to persist relationships")
		}

		fmt.Printf("claims=%d relationships=%d db=%s\n", len(claims), len(rels), defaultDBPath)
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

	err := runJob("process", scope, func(_ context.Context, job *workflow.Job, db *sql.DB) error {
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

		engine := extract.NewEngine()
		claims, links, err := engine.Extract(events)
		if err != nil {
			return NewSystemError(err, "claim extraction failed")
		}

		if err := job.SetStatus("relating", ""); err != nil {
			return err
		}
		relEngine := relate.NewEngine()
		rels, err := relEngine.Detect(claims)
		if err != nil {
			return NewSystemError(err, "relationship detection failed")
		}

		if err := job.SetStatus("saving", ""); err != nil {
			return err
		}
		if err := persistProcessArtifacts(db, events, claims, links, rels); err != nil {
			return err
		}

		eventIDs := make([]string, 0, len(events))
		for _, event := range events {
			eventIDs = append(eventIDs, event.ID)
		}

		fmt.Printf("Session: %s\n", job.ID())
		fmt.Printf("Processed: %d events → %d claims\n", len(events), len(claims))

		printExtractionSummary(claims, rels)
		if len(claims) > 0 {
			printClaimPreview(claims, 3)
		}

		return nil
	})
	exitWithMnemosError(f.Verbose, err)
}

func persistProcessArtifacts(db *sql.DB, events []domain.Event, claims []domain.Claim, links []domain.ClaimEvidence, relationships []domain.Relationship) error {
	tx, err := db.Begin()
	if err != nil {
		return NewSystemError(err, "database transaction failed")
	}
	//nolint:errcheck
	defer tx.Rollback()

	q := sqlcgen.New(tx)
	ctx := context.Background()

	for _, event := range events {
		metadata, err := json.Marshal(event.Metadata)
		if err != nil {
			return NewSystemError(err, "internal error marshaling event metadata")
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
			return NewSystemError(err, "failed to insert event %s", event.ID)
		}
	}

	for _, claim := range claims {
		if err := claim.Validate(); err != nil {
			return NewSystemError(err, "internal: invalid claim %s", claim.ID)
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
			return NewSystemError(err, "failed to upsert claim %s", claim.ID)
		}
	}

	for _, link := range links {
		if err := link.Validate(); err != nil {
			return NewSystemError(err, "internal: invalid claim evidence")
		}
		err = q.UpsertClaimEvidence(ctx, sqlcgen.UpsertClaimEvidenceParams{ClaimID: link.ClaimID, EventID: link.EventID})
		if err != nil {
			return NewSystemError(err, "failed to upsert claim evidence (%s,%s)", link.ClaimID, link.EventID)
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
			return NewSystemError(err, "failed to upsert relationship %s", rel.ID)
		}
	}

	if err := tx.Commit(); err != nil {
		return NewSystemError(err, "failed to commit transaction")
	}

	return nil
}

func printUsage() {
	fmt.Println("Mnemos CLI")
	fmt.Println("")
	fmt.Println("Usage:")
	fmt.Println("  mnemos ingest <path>")
	fmt.Println("  mnemos ingest --text <content>")
	fmt.Println("  mnemos extract <event-id> [event-id ...]")
	fmt.Println("  mnemos relate [event-id ...]")
	fmt.Println("  mnemos process <path>")
	fmt.Println("  mnemos process --text <content>")
	fmt.Println("  mnemos query [--run <run-id>] [--human] <question>")
	fmt.Println("")
	fmt.Println("Flags:")
	fmt.Println("  -h, --help     show this help message")
	fmt.Println("  -v, --verbose  show detailed error output")
	fmt.Println("  --human        human-readable output (default: JSON)")
	fmt.Println("")
	fmt.Println("Quick Start:")
	fmt.Println("  mnemos process --text \"Your text here\"")
	fmt.Println("  mnemos query --human \"Your question\"")
}

func parseQueryArgs(args []string) (string, string, error) {
	if len(args) == 0 {
		return "", "", NewUserError("query requires a question")
	}

	runID := ""
	questionArgs := args
	if args[0] == "--run" {
		if len(args) < 3 {
			return "", "", NewUserError("--run flag requires <run-id> followed by a question")
		}
		runID = strings.TrimSpace(args[1])
		if runID == "" {
			return "", "", NewUserError("--run flag requires a non-empty run-id")
		}
		questionArgs = args[2:]
	}

	question := strings.TrimSpace(strings.Join(questionArgs, " "))
	if question == "" {
		return "", "", NewUserError("query requires a question")
	}

	return question, runID, nil
}

func runJob(kind string, scope map[string]string, fn func(context.Context, *workflow.Job, *sql.DB) error) error {
	if isFirstRun(defaultDBPath) && kind != "ingest" && kind != "process" {
		printWelcome()
		fmt.Println("  First run detected. Use 'process' or 'ingest' to add knowledge.")
		printFirstRunHints()
	}

	db, err := sqlite.Open(defaultDBPath)
	if err != nil {
		return NewSystemError(err, "failed to open database at %q", defaultDBPath)
	}
	//nolint:errcheck
	defer db.Close()

	runner := workflow.NewRunner(sqlite.NewCompilationJobRepository(db))
	runner.Timeout = 20 * time.Second
	runner.MaxRetries = 1

	jobErr := runner.Run(kind, scope, func(ctx context.Context, job *workflow.Job) error {
		return fn(ctx, job, db)
	})
	return jobErr
}
