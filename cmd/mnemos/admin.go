package main

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"

	"strconv"
	"time"

	"github.com/felixgeelhaar/mnemos/internal/embedding"
	"github.com/felixgeelhaar/mnemos/internal/pipeline"
	"github.com/felixgeelhaar/mnemos/internal/store/sqlite"
	"github.com/felixgeelhaar/mnemos/internal/store/sqlite/sqlcgen"
	"github.com/felixgeelhaar/mnemos/internal/trust"
	"github.com/felixgeelhaar/mnemos/internal/workflow"
)

// resetCounts captures what was removed during a reset for the user-facing
// summary. Zero values are still printed so users see exactly what changed.
type resetCounts struct {
	Claims        int64
	Evidence      int64
	StatusHistory int64
	Relationships int64
	Embeddings    int64
	Events        int64
}

func handleReset(args []string, f Flags) {
	keepEvents := false
	for _, a := range args {
		switch a {
		case "--keep-events":
			keepEvents = true
		default:
			fmt.Fprintf(os.Stderr, "error: unknown argument %q for reset\n", a)
			fmt.Fprintln(os.Stderr, "  mnemos reset [--keep-events] [--yes]")
			os.Exit(int(ExitUsage))
		}
	}

	if !f.Yes {
		desc := "all events, claims, relationships, and embeddings"
		if keepEvents {
			desc = "all claims, relationships, and embeddings (events kept)"
		}
		if !confirm(fmt.Sprintf("This will delete %s from %s. Continue?", desc, resolveDBPath())) {
			fmt.Println("aborted")
			os.Exit(int(ExitSuccess))
		}
	}

	err := runJob("reset", map[string]string{"keep_events": fmt.Sprintf("%t", keepEvents)}, f.Verbose, func(ctx context.Context, _ *workflow.Job, db *sql.DB) error {
		counts, err := resetDB(ctx, db, keepEvents)
		if err != nil {
			return NewSystemError(err, "reset failed")
		}
		printResetSummary(counts, keepEvents)
		return nil
	})
	exitWithMnemosError(f.Verbose, err)
}

func handleDeleteClaim(args []string, f Flags) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "error: delete-claim requires at least one claim id")
		fmt.Fprintln(os.Stderr, "  mnemos delete-claim <id> [<id>...]")
		os.Exit(int(ExitUsage))
	}

	err := runJob("delete-claim", map[string]string{"ids": strings.Join(args, ",")}, f.Verbose, func(ctx context.Context, _ *workflow.Job, db *sql.DB) error {
		var deletedClaims, deletedRels, deletedEvidence int64
		err := withTx(ctx, db, func(q *sqlcgen.Queries) error {
			for _, id := range args {
				if err := q.DeleteRelationshipsByClaimID(ctx, sqlcgen.DeleteRelationshipsByClaimIDParams{
					FromClaimID: id, ToClaimID: id,
				}); err != nil {
					return fmt.Errorf("delete relationships for %s: %w", id, err)
				}
				deletedRels++
				if err := q.DeleteClaimEvidenceByClaimID(ctx, id); err != nil {
					return fmt.Errorf("delete evidence for %s: %w", id, err)
				}
				deletedEvidence++
				if err := q.DeleteClaimStatusHistoryByClaimID(ctx, id); err != nil {
					return fmt.Errorf("delete status history for %s: %w", id, err)
				}
				if err := q.DeleteEmbeddingByEntity(ctx, sqlcgen.DeleteEmbeddingByEntityParams{
					EntityID: id, EntityType: "claim",
				}); err != nil {
					return fmt.Errorf("delete embedding for %s: %w", id, err)
				}
				if err := q.DeleteClaimByID(ctx, id); err != nil {
					return fmt.Errorf("delete claim %s: %w", id, err)
				}
				deletedClaims++
			}
			return nil
		})
		if err != nil {
			return NewSystemError(err, "delete-claim failed")
		}
		fmt.Printf("Deleted %d claim(s) and their evidence/embeddings/relationships.\n", deletedClaims)
		return nil
	})
	exitWithMnemosError(f.Verbose, err)
}

func handleDeleteEvent(args []string, f Flags) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "error: delete-event requires at least one event id")
		fmt.Fprintln(os.Stderr, "  mnemos delete-event <id> [<id>...]")
		os.Exit(int(ExitUsage))
	}

	err := runJob("delete-event", map[string]string{"ids": strings.Join(args, ",")}, f.Verbose, func(ctx context.Context, _ *workflow.Job, db *sql.DB) error {
		var deletedEvents int64
		var cascadedClaims int64
		err := withTx(ctx, db, func(q *sqlcgen.Queries) error {
			for _, id := range args {
				// Cascade through dependent claims first.
				claimIDs, err := q.ListClaimsByEventID(ctx, id)
				if err != nil {
					return fmt.Errorf("list claims for event %s: %w", id, err)
				}
				for _, cid := range claimIDs {
					if err := q.DeleteRelationshipsByClaimID(ctx, sqlcgen.DeleteRelationshipsByClaimIDParams{
						FromClaimID: cid, ToClaimID: cid,
					}); err != nil {
						return err
					}
					if err := q.DeleteClaimEvidenceByClaimID(ctx, cid); err != nil {
						return err
					}
					if err := q.DeleteClaimStatusHistoryByClaimID(ctx, cid); err != nil {
						return err
					}
					if err := q.DeleteEmbeddingByEntity(ctx, sqlcgen.DeleteEmbeddingByEntityParams{
						EntityID: cid, EntityType: "claim",
					}); err != nil {
						return err
					}
					if err := q.DeleteClaimByID(ctx, cid); err != nil {
						return err
					}
					cascadedClaims++
				}
				if err := q.DeleteEmbeddingByEntity(ctx, sqlcgen.DeleteEmbeddingByEntityParams{
					EntityID: id, EntityType: "event",
				}); err != nil {
					return err
				}
				if err := q.DeleteEventByID(ctx, id); err != nil {
					return fmt.Errorf("delete event %s: %w", id, err)
				}
				deletedEvents++
			}
			return nil
		})
		if err != nil {
			return NewSystemError(err, "delete-event failed")
		}
		fmt.Printf("Deleted %d event(s); cascaded %d claim(s).\n", deletedEvents, cascadedClaims)
		return nil
	})
	exitWithMnemosError(f.Verbose, err)
}

// handleDedupe runs the semantic-dedupe pipeline against the local
// claim store. Defaults to dry-run because the operation is
// destructive (claims are merged, others deleted); --apply commits.
//
// Threshold default 0.92 is conservative on purpose. Lowering to
// 0.85 catches more paraphrases but also more legitimate distinct
// claims. Users should re-tune for their corpus.
func handleDedupe(args []string, f Flags) {
	threshold := 0.92
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--threshold":
			if i+1 >= len(args) {
				exitWithMnemosError(false, NewUserError("--threshold requires a value in (0, 1]"))
				return
			}
			t, err := strconv.ParseFloat(args[i+1], 64)
			if err != nil || t <= 0 || t > 1 {
				exitWithMnemosError(false, NewUserError("--threshold must be a float in (0, 1]"))
				return
			}
			threshold = t
			i++
		default:
			exitWithMnemosError(false, NewUserError("unknown argument %q for dedup", args[i]))
			return
		}
	}

	// --apply must be opt-in; default is dry-run. We borrow Flags.Force
	// for "yes really apply this" so users get a single mental model
	// across reembed, dedupe, etc.
	apply := f.Force
	if !apply && !f.DryRun {
		// Neither flag set → still default to dry-run, just say so.
		f.DryRun = true
	}

	err := runJob("dedup", map[string]string{
		"threshold": strconv.FormatFloat(threshold, 'f', 2, 64),
		"apply":     fmt.Sprintf("%t", apply),
	}, f.Verbose, func(ctx context.Context, _ *workflow.Job, db *sql.DB) error {
		plan, err := pipeline.PlanSemanticDedupe(ctx, db, threshold)
		if err != nil {
			return NewSystemError(err, "plan semantic dedupe")
		}
		printDedupePlan(plan)
		if !apply {
			fmt.Println("\nDry run. Re-run with --force to apply.")
			return nil
		}
		merged, err := pipeline.ApplySemanticDedupe(ctx, db, plan)
		if err != nil {
			return NewSystemError(err, "apply semantic dedupe")
		}
		fmt.Printf("\nMerged %d duplicate claim(s).\n", merged)
		// Trust ranking depends on the evidence count we just
		// changed; recompute so the next query sees fresh scores.
		repo := sqlite.NewClaimRepository(db)
		now := time.Now().UTC()
		if _, err := repo.RecomputeTrust(ctx, func(confidence float64, evidenceCount int, latestEvidence time.Time) float64 {
			return trust.Score(confidence, evidenceCount, latestEvidence, now)
		}); err != nil {
			fmt.Fprintf(os.Stderr, "  warning: post-dedupe trust recompute failed: %v\n", err)
		}
		return nil
	})
	exitWithMnemosError(f.Verbose, err)
}

func printDedupePlan(plan pipeline.SemanticDedupePlan) {
	fmt.Printf("Semantic dedupe plan (threshold=%.2f)\n", plan.Threshold)
	fmt.Printf("  scanned:   %d claim(s) with embeddings\n", plan.ClaimsScanned)
	if plan.SkippedNoEmbedding > 0 {
		fmt.Printf("  skipped:   %d claim(s) without embeddings (run 'mnemos reembed' to include them)\n", plan.SkippedNoEmbedding)
	}
	if len(plan.Merges) == 0 {
		fmt.Println("  no near-duplicates found.")
		return
	}
	fmt.Printf("  proposing: %d merge(s)\n", len(plan.Merges))
	for i, m := range plan.Merges {
		fmt.Printf("    %d. winner=%s sim=%.3f absorbs %d duplicate(s): %s\n",
			i+1, m.WinnerID, m.MaxSimilarity, len(m.DuplicateIDs), strings.Join(m.DuplicateIDs, ", "))
	}
}

// handleRecomputeTrust rebuilds trust_score for every claim under the
// current scoring policy. Useful after upgrading (the v1→v2 migration
// adds the column with default 0; this command actually populates
// it), after tuning the trust constants in internal/trust, or as a
// nightly cron via `mnemos schedule`.
func handleRecomputeTrust(args []string, f Flags) {
	for _, a := range args {
		if a != "--all" {
			fmt.Fprintf(os.Stderr, "error: unknown argument %q for recompute-trust\n", a)
			fmt.Fprintln(os.Stderr, "  mnemos recompute-trust [--all]")
			os.Exit(int(ExitUsage))
		}
	}

	err := runJob("recompute-trust", map[string]string{}, f.Verbose, func(ctx context.Context, _ *workflow.Job, db *sql.DB) error {
		repo := sqlite.NewClaimRepository(db)
		now := time.Now().UTC()
		n, err := repo.RecomputeTrust(ctx, func(confidence float64, evidenceCount int, latestEvidence time.Time) float64 {
			return trust.Score(confidence, evidenceCount, latestEvidence, now)
		})
		if err != nil {
			return NewSystemError(err, "recompute trust")
		}
		fmt.Printf("Recomputed trust for %d claim(s).\n", n)
		return nil
	})
	exitWithMnemosError(f.Verbose, err)
}

func handleReembed(args []string, f Flags) {
	for _, a := range args {
		switch a {
		default:
			fmt.Fprintf(os.Stderr, "error: unknown argument %q for reembed\n", a)
			fmt.Fprintln(os.Stderr, "  mnemos reembed [--force] [--dry-run]")
			os.Exit(int(ExitUsage))
		}
	}

	err := runJob("reembed", map[string]string{"force": fmt.Sprintf("%t", f.Force), "dry_run": fmt.Sprintf("%t", f.DryRun)}, f.Verbose, func(ctx context.Context, _ *workflow.Job, db *sql.DB) error {
		q := sqlcgen.New(db)

		// Determine which claim ids need (re-)embedding.
		var ids []string
		if f.Force {
			rows, err := q.ListAllClaims(ctx)
			if err != nil {
				return NewSystemError(err, "list claims")
			}
			for _, r := range rows {
				ids = append(ids, r.ID)
			}
		} else {
			missing, err := q.ListEntityIDsMissingEmbedding(ctx)
			if err != nil {
				return NewSystemError(err, "list missing embeddings")
			}
			ids = missing
		}

		if len(ids) == 0 {
			fmt.Println("No claims need embeddings. Nothing to do.")
			return nil
		}

		if f.DryRun {
			fmt.Printf("Would (re)embed %d claim(s). Run without --dry-run to apply.\n", len(ids))
			return nil
		}

		// Build the text-by-id map.
		allClaims, err := q.ListAllClaims(ctx)
		if err != nil {
			return NewSystemError(err, "load claim text")
		}
		text := make(map[string]string, len(allClaims))
		for _, c := range allClaims {
			text[c.ID] = c.Text
		}

		cfg, err := embedding.ConfigFromEnv()
		if err != nil {
			return NewSystemError(err, "embedding config")
		}
		client, err := embedding.NewClient(cfg)
		if err != nil {
			return NewSystemError(err, "embedding client")
		}

		texts := make([]string, 0, len(ids))
		keep := make([]string, 0, len(ids))
		for _, id := range ids {
			t, ok := text[id]
			if !ok {
				continue
			}
			texts = append(texts, t)
			keep = append(keep, id)
		}

		vectors, err := client.Embed(ctx, texts)
		if err != nil {
			return NewSystemError(err, "embed claims")
		}

		repo := sqlite.NewEmbeddingRepository(db)
		for i, id := range keep {
			if i >= len(vectors) {
				break
			}
			if err := repo.Upsert(ctx, id, "claim", vectors[i], cfg.Model); err != nil {
				return NewSystemError(err, "store embedding for %s", id)
			}
		}

		fmt.Printf("Embedded %d claim(s) with %s/%s.\n", len(vectors), cfg.Provider, cfg.Model)
		return nil
	})
	exitWithMnemosError(f.Verbose, err)
}

// resetDB deletes all derived state (and optionally events) inside a single
// transaction. Returns row counts via a separate read pass before deleting
// so the user-facing summary is accurate.
func resetDB(ctx context.Context, db *sql.DB, keepEvents bool) (resetCounts, error) {
	var counts resetCounts

	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM claims").Scan(&counts.Claims); err != nil {
		return counts, fmt.Errorf("count claims: %w", err)
	}
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM claim_evidence").Scan(&counts.Evidence); err != nil {
		return counts, fmt.Errorf("count evidence: %w", err)
	}
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM claim_status_history").Scan(&counts.StatusHistory); err != nil {
		return counts, fmt.Errorf("count status history: %w", err)
	}
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM relationships").Scan(&counts.Relationships); err != nil {
		return counts, fmt.Errorf("count relationships: %w", err)
	}
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM embeddings").Scan(&counts.Embeddings); err != nil {
		return counts, fmt.Errorf("count embeddings: %w", err)
	}
	if !keepEvents {
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM events").Scan(&counts.Events); err != nil {
			return counts, fmt.Errorf("count events: %w", err)
		}
	}

	err := withTx(ctx, db, func(q *sqlcgen.Queries) error {
		if err := q.DeleteAllRelationships(ctx); err != nil {
			return err
		}
		if err := q.DeleteAllClaimEvidence(ctx); err != nil {
			return err
		}
		if err := q.DeleteAllClaimStatusHistory(ctx); err != nil {
			return err
		}
		if err := q.DeleteAllEmbeddings(ctx); err != nil {
			return err
		}
		if err := q.DeleteAllClaims(ctx); err != nil {
			return err
		}
		if !keepEvents {
			if err := q.DeleteAllEvents(ctx); err != nil {
				return err
			}
		}
		return nil
	})
	return counts, err
}

func printResetSummary(c resetCounts, keepEvents bool) {
	fmt.Printf("Reset complete (db=%s)\n", resolveDBPath())
	fmt.Printf("  claims:        %-8d (deleted)\n", c.Claims)
	fmt.Printf("  evidence:      %-8d (deleted)\n", c.Evidence)
	fmt.Printf("  status hist:   %-8d (deleted)\n", c.StatusHistory)
	fmt.Printf("  relationships: %-8d (deleted)\n", c.Relationships)
	fmt.Printf("  embeddings:    %-8d (deleted)\n", c.Embeddings)
	if keepEvents {
		fmt.Printf("  events:        kept\n")
	} else {
		fmt.Printf("  events:        %-8d (deleted)\n", c.Events)
	}
}

// withTx runs fn inside a transaction. Commits on success, rolls back on
// any error. Caller passes a sqlcgen.Queries bound to the tx.
func withTx(ctx context.Context, db *sql.DB, fn func(*sqlcgen.Queries) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	q := sqlcgen.New(tx)
	if err := fn(q); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			return fmt.Errorf("%w (rollback also failed: %v)", err, rbErr)
		}
		return err
	}
	return tx.Commit()
}

func confirm(prompt string) bool {
	fmt.Printf("%s [y/N]: ", prompt)
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return false
	}
	line = strings.TrimSpace(strings.ToLower(line))
	return line == "y" || line == "yes"
}
