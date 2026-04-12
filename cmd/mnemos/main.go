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
	"github.com/felixgeelhaar/mnemos/internal/workflow"
)

const defaultDBPath = "data/mnemos.db"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]
	switch command {
	case "ingest":
		handleIngest(os.Args[2:])
	case "extract":
		handleExtract(os.Args[2:])
	case "relate":
		handleRelate(os.Args[2:])
	case "process":
		handleProcess(os.Args[2:])
	case "query":
		handleQuery(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", command)
		printUsage()
		os.Exit(1)
	}
}

func handleIngest(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "ingest expects a file path or --text <content>")
		os.Exit(1)
	}

	service := ingest.NewService()
	normalizer := parser.NewNormalizer()

	if args[0] == "--text" {
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "ingest --text expects content")
			os.Exit(1)
		}

		contentArg := strings.Join(args[1:], " ")
		err := runJob("ingest", map[string]string{"source": "raw_text"}, func(_ context.Context, job *workflow.Job, db *sql.DB) error {
			if err := job.SetStatus("loading", ""); err != nil {
				return err
			}
			input, content, err := service.IngestText(contentArg, nil)
			if err != nil {
				return err
			}
			if err := job.SetStatus("extracting", ""); err != nil {
				return err
			}
			events, err := normalizer.Normalize(input, content)
			if err != nil {
				return err
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
					return err
				}
			}
			fmt.Printf("run_id=%s input=%s type=%s format=%s bytes=%d events=%d db=%s source=%s\n", job.ID(), input.ID, input.Type, input.Format, len(content), len(events), defaultDBPath, input.Metadata["source"])
			return nil
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "ingest job error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "ingest expects exactly one path argument")
		os.Exit(1)
	}

	path := args[0]
	err := runJob("ingest", map[string]string{"path": path}, func(_ context.Context, job *workflow.Job, db *sql.DB) error {
		if err := job.SetStatus("loading", ""); err != nil {
			return err
		}
		input, content, err := service.IngestFile(path)
		if err != nil {
			return err
		}
		if err := job.SetStatus("extracting", ""); err != nil {
			return err
		}
		events, err := normalizer.Normalize(input, content)
		if err != nil {
			return err
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
				return err
			}
		}
		fmt.Printf("run_id=%s input=%s type=%s format=%s bytes=%d events=%d db=%s source=%s\n", job.ID(), input.ID, input.Type, input.Format, len(content), len(events), defaultDBPath, input.Metadata["source_path"])
		return nil
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "ingest job error: %v\n", err)
		os.Exit(1)
	}
}

func handleQuery(args []string) {
	question, runID, err := parseQueryArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "query error: %v\n", err)
		os.Exit(1)
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
			return err
		}

		response := map[string]any{
			"answer":         answer.AnswerText,
			"claims":         answer.Claims,
			"contradictions": answer.Contradictions,
			"timeline":       answer.TimelineEventIDs,
		}
		encoded, err := json.MarshalIndent(response, "", "  ")
		if err != nil {
			return err
		}

		fmt.Println(string(encoded))
		return nil
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "query job error: %v\n", err)
		os.Exit(1)
	}
}

func handleExtract(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "extract expects one or more event IDs")
		os.Exit(1)
	}

	err := runJob("extract", map[string]string{"event_ids": strings.Join(args, ",")}, func(_ context.Context, job *workflow.Job, db *sql.DB) error {
		if err := job.SetStatus("loading", ""); err != nil {
			return err
		}
		eventRepo := sqlite.NewEventRepository(db)
		events, err := eventRepo.ListByIDs(args)
		if err != nil {
			return err
		}
		if len(events) == 0 {
			return fmt.Errorf("no events found for provided IDs")
		}

		if err := job.SetStatus("extracting", ""); err != nil {
			return err
		}
		engine := extract.NewEngine()
		claims, links, err := engine.Extract(events)
		if err != nil {
			return err
		}

		if err := job.SetStatus("saving", ""); err != nil {
			return err
		}
		claimRepo := sqlite.NewClaimRepository(db)
		if err := claimRepo.Upsert(claims); err != nil {
			return err
		}
		if err := claimRepo.UpsertEvidence(links); err != nil {
			return err
		}

		fmt.Printf("events=%d claims=%d evidence_links=%d db=%s\n", len(events), len(claims), len(links), defaultDBPath)
		return nil
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "extract job error: %v\n", err)
		os.Exit(1)
	}
}

func handleRelate(args []string) {
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
			return err
		}
		if len(claims) < 2 {
			return fmt.Errorf("need at least two claims to detect relationships")
		}

		if err := job.SetStatus("relating", ""); err != nil {
			return err
		}
		engine := relate.NewEngine()
		rels, err := engine.Detect(claims)
		if err != nil {
			return err
		}
		if err := job.SetStatus("saving", ""); err != nil {
			return err
		}
		if err := relRepo.Upsert(rels); err != nil {
			return err
		}

		fmt.Printf("claims=%d relationships=%d db=%s\n", len(claims), len(rels), defaultDBPath)
		return nil
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "relate job error: %v\n", err)
		os.Exit(1)
	}
}

func handleProcess(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "process expects a file path or --text <content>")
		os.Exit(1)
	}

	service := ingest.NewService()
	normalizer := parser.NewNormalizer()

	err := runJob("process", map[string]string{"args": strings.Join(args, " ")}, func(_ context.Context, job *workflow.Job, db *sql.DB) error {
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
				return fmt.Errorf("process --text expects content")
			}
			input, content, err = service.IngestText(strings.Join(args[1:], " "), nil)
		} else {
			if len(args) != 1 {
				return fmt.Errorf("process expects exactly one path argument")
			}
			input, content, err = service.IngestFile(args[0])
		}
		if err != nil {
			return err
		}

		if err := job.SetStatus("extracting", ""); err != nil {
			return err
		}
		events, err := normalizer.Normalize(input, content)
		if err != nil {
			return err
		}
		for i := range events {
			events[i].RunID = job.ID()
		}

		if err := job.SetStatus("saving", ""); err != nil {
			return err
		}
		eventRepo := sqlite.NewEventRepository(db)
		for _, event := range events {
			if err := eventRepo.Append(event); err != nil {
				return err
			}
		}

		engine := extract.NewEngine()
		claims, links, err := engine.Extract(events)
		if err != nil {
			return err
		}
		claimRepo := sqlite.NewClaimRepository(db)
		if err := claimRepo.Upsert(claims); err != nil {
			return err
		}
		if err := claimRepo.UpsertEvidence(links); err != nil {
			return err
		}

		if err := job.SetStatus("relating", ""); err != nil {
			return err
		}
		relEngine := relate.NewEngine()
		rels, err := relEngine.Detect(claims)
		if err != nil {
			return err
		}
		relRepo := sqlite.NewRelationshipRepository(db)
		if err := relRepo.Upsert(rels); err != nil {
			return err
		}

		eventIDs := make([]string, 0, len(events))
		for _, event := range events {
			eventIDs = append(eventIDs, event.ID)
		}

		fmt.Printf("run_id=%s input=%s events=%d claims=%d relationships=%d event_ids=%s db=%s\n", job.ID(), input.ID, len(events), len(claims), len(rels), strings.Join(eventIDs, ","), defaultDBPath)
		return nil
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "process job error: %v\n", err)
		os.Exit(1)
	}
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
	fmt.Println("  mnemos query [--run <run-id>] <question>")
}

func parseQueryArgs(args []string) (string, string, error) {
	if len(args) == 0 {
		return "", "", fmt.Errorf("query expects a question")
	}

	runID := ""
	questionArgs := args
	if args[0] == "--run" {
		if len(args) < 3 {
			return "", "", fmt.Errorf("query --run expects <run-id> and question")
		}
		runID = strings.TrimSpace(args[1])
		if runID == "" {
			return "", "", fmt.Errorf("query --run requires a non-empty run id")
		}
		questionArgs = args[2:]
	}

	question := strings.TrimSpace(strings.Join(questionArgs, " "))
	if question == "" {
		return "", "", fmt.Errorf("query expects a question")
	}

	return question, runID, nil
}

func runJob(kind string, scope map[string]string, fn func(context.Context, *workflow.Job, *sql.DB) error) error {
	db, err := sqlite.Open(defaultDBPath)
	if err != nil {
		return err
	}
	defer db.Close()

	runner := workflow.NewRunner(sqlite.NewCompilationJobRepository(db))
	runner.Timeout = 20 * time.Second
	runner.MaxRetries = 1

	jobErr := runner.Run(kind, scope, func(ctx context.Context, job *workflow.Job) error {
		return fn(ctx, job, db)
	})
	return jobErr
}
