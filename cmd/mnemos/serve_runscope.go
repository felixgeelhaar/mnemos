package main

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// checkEventRunsAllowed returns ("", nil) when every supplied event
// id maps to a run_id present in the allowed whitelist, otherwise it
// returns the first offending (eventID, runID) pair.
//
// Empty allowed slice short-circuits to allowed=true (no
// restriction). Unknown event ids are treated as "not in whitelist"
// — an agent referencing a nonexistent event from a write payload
// is suspicious and not worth distinguishing from a cross-run leak.
func checkEventRunsAllowed(ctx context.Context, db *sql.DB, eventIDs []string, allowed []string) (badEventID, badRunID string, err error) {
	if len(allowed) == 0 || len(eventIDs) == 0 {
		return "", "", nil
	}

	allowedSet := make(map[string]struct{}, len(allowed))
	for _, a := range allowed {
		allowedSet[a] = struct{}{}
	}

	placeholders := make([]string, 0, len(eventIDs))
	args := make([]any, 0, len(eventIDs))
	seen := map[string]struct{}{}
	for _, id := range eventIDs {
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		placeholders = append(placeholders, "?")
		args = append(args, id)
	}

	//nolint:gosec // G202: placeholders are literal "?" strings; values pass through ? bindings
	q := "SELECT id, run_id FROM events WHERE id IN (" + strings.Join(placeholders, ",") + ")"
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return "", "", fmt.Errorf("lookup event runs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	found := map[string]string{}
	for rows.Next() {
		var id, runID string
		if err := rows.Scan(&id, &runID); err != nil {
			return "", "", fmt.Errorf("scan event run: %w", err)
		}
		found[id] = runID
	}
	if err := rows.Err(); err != nil {
		return "", "", fmt.Errorf("iterate event runs: %w", err)
	}

	for id := range seen {
		runID, ok := found[id]
		if !ok {
			// Referencing an event that doesn't exist — don't let
			// the write proceed and don't leak which event id was
			// missing in the error.
			return id, "", nil
		}
		if _, allowed := allowedSet[runID]; !allowed {
			return id, runID, nil
		}
	}
	return "", "", nil
}

// claimEventIDs returns the de-duplicated set of event ids that the
// given claim ids are linked to via claim_evidence. Used to derive
// the run-id surface for run-scope enforcement on relationship and
// embedding writes that reference claims rather than events directly.
func claimEventIDs(ctx context.Context, db *sql.DB, claimIDs []string) ([]string, error) {
	if len(claimIDs) == 0 {
		return nil, nil
	}
	placeholders := make([]string, 0, len(claimIDs))
	args := make([]any, 0, len(claimIDs))
	for _, id := range claimIDs {
		placeholders = append(placeholders, "?")
		args = append(args, id)
	}
	//nolint:gosec // G202: placeholders are literal "?" strings; values pass through ? bindings
	q := "SELECT DISTINCT event_id FROM claim_evidence WHERE claim_id IN (" + strings.Join(placeholders, ",") + ")"
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("lookup claim evidence: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var ev string
		if err := rows.Scan(&ev); err != nil {
			return nil, fmt.Errorf("scan claim evidence: %w", err)
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}
