package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

// auditWhoExport is the on-the-wire shape of `mnemos audit who`.
// Distinct from auditExport so the dump-everything snapshot's schema
// can evolve independently from the principal-scoped report.
type auditWhoExport struct {
	SchemaVersion string                 `json:"schema_version"`
	GeneratedAt   string                 `json:"generated_at"`
	Principal     string                 `json:"principal"`
	Since         string                 `json:"since,omitempty"`
	Counts        auditWhoCounts         `json:"counts"`
	Events        []auditWhoEvent        `json:"events"`
	Claims        []auditWhoClaim        `json:"claims"`
	Relationships []auditWhoRelationship `json:"relationships"`
	Embeddings    []auditWhoEmbedding    `json:"embeddings"`
	Transitions   []auditWhoTransition   `json:"status_transitions"`
}

type auditWhoCounts struct {
	Events        int `json:"events"`
	Claims        int `json:"claims"`
	Relationships int `json:"relationships"`
	Embeddings    int `json:"embeddings"`
	Transitions   int `json:"status_transitions"`
}

type auditWhoEvent struct {
	ID        string `json:"id"`
	RunID     string `json:"run_id"`
	Content   string `json:"content"`
	Timestamp string `json:"timestamp"`
}

type auditWhoClaim struct {
	ID         string  `json:"id"`
	Text       string  `json:"text"`
	Type       string  `json:"type"`
	Status     string  `json:"status"`
	Confidence float64 `json:"confidence"`
	CreatedAt  string  `json:"created_at"`
}

type auditWhoRelationship struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	FromClaimID string `json:"from_claim_id"`
	ToClaimID   string `json:"to_claim_id"`
	CreatedAt   string `json:"created_at"`
}

type auditWhoEmbedding struct {
	EntityID   string `json:"entity_id"`
	EntityType string `json:"entity_type"`
	Model      string `json:"model"`
	Dimensions int    `json:"dimensions"`
	CreatedAt  string `json:"created_at"`
}

type auditWhoTransition struct {
	ClaimID    string `json:"claim_id"`
	FromStatus string `json:"from_status"`
	ToStatus   string `json:"to_status"`
	ChangedAt  string `json:"changed_at"`
	Reason     string `json:"reason"`
}

const auditWhoSchemaVersion = "audit_who.v1"

// handleAuditWho implements `mnemos audit who <principal-id>
// [--since <duration>] [--human]`. The principal can be any string
// recorded in created_by / changed_by — a user id (usr_*), an agent
// id (agt_*), or the <system> sentinel.
func handleAuditWho(args []string, f Flags) {
	if len(args) == 0 {
		exitWithMnemosError(false, NewUserError("audit who <principal-id> [--since <duration>]"))
		return
	}
	principal := args[0]
	args = args[1:]

	var since time.Time
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--since":
			if i+1 >= len(args) {
				exitWithMnemosError(false, NewUserError("--since requires a duration like 24h"))
				return
			}
			d, err := time.ParseDuration(args[i+1])
			if err != nil {
				exitWithMnemosError(false, NewUserError("invalid --since: %v", err))
				return
			}
			since = time.Now().UTC().Add(-d)
			i++
		default:
			exitWithMnemosError(false, NewUserError("unknown audit who flag %q", args[i]))
			return
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db, conn, err := openDB(ctx)
	if err != nil {
		exitWithMnemosError(false, NewSystemError(err, "open database"))
		return
	}
	defer closeConn(conn)

	export, err := buildAuditWhoExport(ctx, db, principal, since)
	if err != nil {
		exitWithMnemosError(false, NewSystemError(err, "build audit-who export"))
		return
	}

	if f.Human {
		printAuditWhoHuman(export)
		return
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(export); err != nil {
		exitWithMnemosError(false, NewSystemError(err, "encode audit-who export"))
		return
	}
}

// buildAuditWhoExport runs five small SELECTs (one per write surface)
// rather than a UNION because the column shapes differ enough that
// a UNION would force lossy projections. The query count is bounded
// by table count, not row count, so this scales fine.
func buildAuditWhoExport(ctx context.Context, db *sql.DB, principal string, since time.Time) (auditWhoExport, error) {
	export := auditWhoExport{
		SchemaVersion: auditWhoSchemaVersion,
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
		Principal:     principal,
		Events:        []auditWhoEvent{},
		Claims:        []auditWhoClaim{},
		Relationships: []auditWhoRelationship{},
		Embeddings:    []auditWhoEmbedding{},
		Transitions:   []auditWhoTransition{},
	}
	if !since.IsZero() {
		export.Since = since.Format(time.RFC3339)
	}

	if err := loadAuditWhoEvents(ctx, db, principal, since, &export); err != nil {
		return export, err
	}
	if err := loadAuditWhoClaims(ctx, db, principal, since, &export); err != nil {
		return export, err
	}
	if err := loadAuditWhoRelationships(ctx, db, principal, since, &export); err != nil {
		return export, err
	}
	if err := loadAuditWhoEmbeddings(ctx, db, principal, since, &export); err != nil {
		return export, err
	}
	if err := loadAuditWhoTransitions(ctx, db, principal, since, &export); err != nil {
		return export, err
	}

	export.Counts = auditWhoCounts{
		Events:        len(export.Events),
		Claims:        len(export.Claims),
		Relationships: len(export.Relationships),
		Embeddings:    len(export.Embeddings),
		Transitions:   len(export.Transitions),
	}
	return export, nil
}

// sinceClause returns ("" or " AND <col> >= ?", arg) for an optional
// timestamp filter. Centralised so each loader can prepend its own
// principal predicate without re-implementing the conditional.
func sinceClause(col string, since time.Time) (string, []any) {
	if since.IsZero() {
		return "", nil
	}
	return fmt.Sprintf(" AND %s >= ?", col), []any{since.UTC().Format(time.RFC3339Nano)}
}

func loadAuditWhoEvents(ctx context.Context, db *sql.DB, principal string, since time.Time, out *auditWhoExport) error {
	clause, extra := sinceClause("timestamp", since)
	args := append([]any{principal}, extra...)
	//nolint:gosec // G202: clause is a literal " AND <col> >= ?" returned by sinceClause; values pass through ? bindings
	q := `SELECT id, run_id, content, timestamp FROM events WHERE created_by = ?` + clause + ` ORDER BY timestamp ASC`
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("query events: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var e auditWhoEvent
		if err := rows.Scan(&e.ID, &e.RunID, &e.Content, &e.Timestamp); err != nil {
			return fmt.Errorf("scan event: %w", err)
		}
		out.Events = append(out.Events, e)
	}
	return rows.Err()
}

func loadAuditWhoClaims(ctx context.Context, db *sql.DB, principal string, since time.Time, out *auditWhoExport) error {
	clause, extra := sinceClause("created_at", since)
	args := append([]any{principal}, extra...)
	//nolint:gosec // G202: clause is a literal " AND <col> >= ?" returned by sinceClause; values pass through ? bindings
	q := `SELECT id, text, type, status, confidence, created_at FROM claims WHERE created_by = ?` + clause + ` ORDER BY created_at ASC`
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("query claims: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var c auditWhoClaim
		if err := rows.Scan(&c.ID, &c.Text, &c.Type, &c.Status, &c.Confidence, &c.CreatedAt); err != nil {
			return fmt.Errorf("scan claim: %w", err)
		}
		out.Claims = append(out.Claims, c)
	}
	return rows.Err()
}

func loadAuditWhoRelationships(ctx context.Context, db *sql.DB, principal string, since time.Time, out *auditWhoExport) error {
	clause, extra := sinceClause("created_at", since)
	args := append([]any{principal}, extra...)
	//nolint:gosec // G202: clause is a literal " AND <col> >= ?" returned by sinceClause; values pass through ? bindings
	q := `SELECT id, type, from_claim_id, to_claim_id, created_at FROM relationships WHERE created_by = ?` + clause + ` ORDER BY created_at ASC`
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("query relationships: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var r auditWhoRelationship
		if err := rows.Scan(&r.ID, &r.Type, &r.FromClaimID, &r.ToClaimID, &r.CreatedAt); err != nil {
			return fmt.Errorf("scan relationship: %w", err)
		}
		out.Relationships = append(out.Relationships, r)
	}
	return rows.Err()
}

func loadAuditWhoEmbeddings(ctx context.Context, db *sql.DB, principal string, since time.Time, out *auditWhoExport) error {
	clause, extra := sinceClause("created_at", since)
	args := append([]any{principal}, extra...)
	//nolint:gosec // G202: clause is a literal " AND <col> >= ?" returned by sinceClause; values pass through ? bindings
	q := `SELECT entity_id, entity_type, model, dimensions, created_at FROM embeddings WHERE created_by = ?` + clause + ` ORDER BY created_at ASC`
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("query embeddings: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var e auditWhoEmbedding
		var dims int64
		if err := rows.Scan(&e.EntityID, &e.EntityType, &e.Model, &dims, &e.CreatedAt); err != nil {
			return fmt.Errorf("scan embedding: %w", err)
		}
		e.Dimensions = int(dims)
		out.Embeddings = append(out.Embeddings, e)
	}
	return rows.Err()
}

func loadAuditWhoTransitions(ctx context.Context, db *sql.DB, principal string, since time.Time, out *auditWhoExport) error {
	clause, extra := sinceClause("changed_at", since)
	args := append([]any{principal}, extra...)
	//nolint:gosec // G202: clause is a literal " AND <col> >= ?" returned by sinceClause; values pass through ? bindings
	q := `SELECT claim_id, from_status, to_status, changed_at, reason FROM claim_status_history WHERE changed_by = ?` + clause + ` ORDER BY changed_at ASC`
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("query transitions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var t auditWhoTransition
		if err := rows.Scan(&t.ClaimID, &t.FromStatus, &t.ToStatus, &t.ChangedAt, &t.Reason); err != nil {
			return fmt.Errorf("scan transition: %w", err)
		}
		out.Transitions = append(out.Transitions, t)
	}
	return rows.Err()
}

// printAuditWhoHuman renders the export as a chronological table.
// Sorting interleaves rows from different write surfaces by their
// natural timestamp so an operator reading top-to-bottom sees the
// agent's actual activity sequence, not five separate sub-reports.
func printAuditWhoHuman(e auditWhoExport) {
	type row struct {
		At   string
		Kind string
		ID   string
		Note string
	}
	var rows []row
	for _, ev := range e.Events {
		rows = append(rows, row{ev.Timestamp, "event", ev.ID, truncate(strings.ReplaceAll(ev.Content, "\n", " "), 60)})
	}
	for _, c := range e.Claims {
		rows = append(rows, row{c.CreatedAt, "claim", c.ID, fmt.Sprintf("[%s/%s] %s", c.Type, c.Status, truncate(c.Text, 50))})
	}
	for _, r := range e.Relationships {
		rows = append(rows, row{r.CreatedAt, "rel", r.ID, fmt.Sprintf("%s %s → %s", r.Type, r.FromClaimID, r.ToClaimID)})
	}
	for _, em := range e.Embeddings {
		rows = append(rows, row{em.CreatedAt, "embed", em.EntityID, fmt.Sprintf("%s/%dD %s", em.EntityType, em.Dimensions, em.Model)})
	}
	for _, t := range e.Transitions {
		rows = append(rows, row{t.ChangedAt, "status", t.ClaimID, fmt.Sprintf("%s → %s (%s)", t.FromStatus, t.ToStatus, t.Reason)})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].At < rows[j].At })

	fmt.Printf("audit: principal=%s", e.Principal)
	if e.Since != "" {
		fmt.Printf(" since=%s", e.Since)
	}
	fmt.Println()
	fmt.Printf("counts: events=%d claims=%d rels=%d embed=%d transitions=%d\n\n",
		e.Counts.Events, e.Counts.Claims, e.Counts.Relationships, e.Counts.Embeddings, e.Counts.Transitions)
	if len(rows) == 0 {
		fmt.Println("(no writes attributed to this principal)")
		return
	}
	fmt.Printf("%-30s %-8s %-26s %s\n", "AT", "KIND", "ID", "DETAILS")
	for _, r := range rows {
		fmt.Printf("%-30s %-8s %-26s %s\n", r.At, r.Kind, r.ID, r.Note)
	}
}
