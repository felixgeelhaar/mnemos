package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/felixgeelhaar/mnemos/internal/domain"
	"github.com/felixgeelhaar/mnemos/internal/store/sqlite"
)

// handleResolve implements the `mnemos resolve <winner-id> --over <loser-id>`
// command. This is the operator-driven half of the issue #6 truth layer:
// when two claims contradict each other, a human picks a winner. The
// winning claim transitions to `resolved` and the losing one to
// `deprecated`. Both transitions land in claim_status_history with the
// provided reason so the audit trail captures not just what was decided
// but why.
//
// Explicitly NOT implemented: automatic resolution by recency or
// confidence. That path is risky (recency ≠ truth; confidence is
// heuristic) and would undermine the project's "surface contradictions,
// let humans judge" stance. If it ever lands, it will be as an opt-in
// probe with its own eval, not a default.
func handleResolve(args []string, _ Flags) {
	if len(args) < 1 {
		exitWithMnemosError(false, NewUserError("resolve requires a winning claim id\n  mnemos resolve <winner-id> --over <loser-id> [--reason \"...\"]"))
		return
	}
	winnerID := args[0]
	args = args[1:]

	loserID := ""
	reason := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--over":
			if i+1 >= len(args) {
				exitWithMnemosError(false, NewUserError("--over requires a claim id"))
				return
			}
			loserID = args[i+1]
			i++
		case "--reason":
			if i+1 >= len(args) {
				exitWithMnemosError(false, NewUserError("--reason requires a string"))
				return
			}
			reason = args[i+1]
			i++
		default:
			exitWithMnemosError(false, NewUserError("unknown resolve flag %q", args[i]))
			return
		}
	}

	if strings.TrimSpace(winnerID) == "" || strings.TrimSpace(loserID) == "" {
		exitWithMnemosError(false, NewUserError("both winner and --over <loser-id> are required"))
		return
	}
	if winnerID == loserID {
		exitWithMnemosError(false, NewUserError("winner and loser must be different claims"))
		return
	}
	if reason == "" {
		reason = "operator resolution via mnemos resolve"
	}

	dbPath := resolveDBPath()
	db, err := sqlite.Open(dbPath)
	if err != nil {
		exitWithMnemosError(false, NewSystemError(err, "open database"))
		return
	}
	defer closeDB(db)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	claimRepo := sqlite.NewClaimRepository(db)
	found, err := claimRepo.ListByIDs(ctx, []string{winnerID, loserID})
	if err != nil {
		exitWithMnemosError(false, NewSystemError(err, "look up claims"))
		return
	}
	byID := make(map[string]domain.Claim, len(found))
	for _, c := range found {
		byID[c.ID] = c
	}
	winner, winnerOK := byID[winnerID]
	loser, loserOK := byID[loserID]
	if !winnerOK {
		exitWithMnemosError(false, NewUserError("winner claim %q not found", winnerID))
		return
	}
	if !loserOK {
		exitWithMnemosError(false, NewUserError("loser claim %q not found", loserID))
		return
	}

	winner.Status = domain.ClaimStatusResolved
	loser.Status = domain.ClaimStatusDeprecated

	// Single-batch upsert so both transitions succeed or fail together —
	// the audit trail should never show a half-resolved pair.
	if err := claimRepo.UpsertWithReason(ctx, []domain.Claim{winner, loser}, reason); err != nil {
		exitWithMnemosError(false, NewSystemError(err, "persist resolution"))
		return
	}

	fmt.Printf("resolved: %s (%s → resolved) over %s (%s → deprecated)\n",
		winner.ID, winner.Type, loser.ID, loser.Type)
	fmt.Printf("reason: %s\n", reason)
}
