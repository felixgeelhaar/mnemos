package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/felixgeelhaar/mnemos/internal/domain"
	"github.com/felixgeelhaar/mnemos/internal/store/sqlite/sqlcgen"
)

// ClaimRepository provides SQLite-backed storage for claims and claim evidence.
type ClaimRepository struct {
	db *sql.DB
	q  *sqlcgen.Queries
}

// NewClaimRepository returns a ClaimRepository backed by the given database.
func NewClaimRepository(db *sql.DB) ClaimRepository {
	return ClaimRepository{db: db, q: sqlcgen.New(db)}
}

// Upsert inserts or updates the given claims in a single transaction. When
// a claim's status changes (or a new claim is created), a row is appended
// to claim_status_history so the lifecycle is reviewable. Callers don't
// opt in — status is a first-class concept and its timeline should be
// recorded for every write.
func (r ClaimRepository) Upsert(ctx context.Context, claims []domain.Claim) error {
	return r.upsertWithReason(ctx, claims, "", "")
}

// UpsertWithReason is like Upsert but records a human-readable reason on
// each status transition. Use this when the caller has meaningful context
// (e.g., "auto: contradiction detected with cl_abc", "resolved via mnemos
// resolve"); pass empty to Upsert and the transition records "" which
// still captures the when, just not the why.
func (r ClaimRepository) UpsertWithReason(ctx context.Context, claims []domain.Claim, reason string) error {
	return r.upsertWithReason(ctx, claims, reason, "")
}

// UpsertWithReasonAs is the actor-aware variant of UpsertWithReason. The
// changedBy id is recorded on every status transition row so the audit
// trail can attribute the change to a specific user. Empty string falls
// back to SystemUser via actorOr.
func (r ClaimRepository) UpsertWithReasonAs(ctx context.Context, claims []domain.Claim, reason, changedBy string) error {
	return r.upsertWithReason(ctx, claims, reason, changedBy)
}

func (r ClaimRepository) upsertWithReason(ctx context.Context, claims []domain.Claim, reason, changedBy string) error {
	if len(claims) == 0 {
		return nil
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin claim upsert tx: %w", err)
	}
	defer rollbackTx(tx)

	qtx := r.q.WithTx(tx)
	now := time.Now().UTC().Format(time.RFC3339Nano)

	for _, claim := range claims {
		if err := claim.Validate(); err != nil {
			return fmt.Errorf("invalid claim %s: %w", claim.ID, err)
		}

		priorStatus, err := currentClaimStatus(ctx, tx, claim.ID)
		if err != nil {
			return fmt.Errorf("look up prior status for %s: %w", claim.ID, err)
		}

		// valid_from defaults to created_at when the caller hasn't
		// already populated it (legacy code paths and tests). The
		// pipeline normally fills this from the earliest evidence
		// event before reaching the repo.
		validFrom := claim.ValidFrom
		if validFrom.IsZero() {
			validFrom = claim.CreatedAt
		}
		err = qtx.UpsertClaim(ctx, sqlcgen.UpsertClaimParams{
			ID:         claim.ID,
			Text:       claim.Text,
			Type:       string(claim.Type),
			Confidence: claim.Confidence,
			Status:     string(claim.Status),
			CreatedAt:  claim.CreatedAt.UTC().Format(time.RFC3339Nano),
			CreatedBy:  actorOr(claim.CreatedBy),
			ValidFrom:  validFrom.UTC().Format(time.RFC3339Nano),
		})
		if err != nil {
			return fmt.Errorf("upsert claim %s: %w", claim.ID, err)
		}

		newStatus := string(claim.Status)
		if priorStatus == newStatus {
			continue // no transition
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO claim_status_history (claim_id, from_status, to_status, changed_at, reason, changed_by) VALUES (?, ?, ?, ?, ?, ?)`,
			claim.ID, priorStatus, newStatus, now, reason, actorOr(changedBy),
		); err != nil {
			return fmt.Errorf("record status transition for %s: %w", claim.ID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit claim upsert tx: %w", err)
	}

	return nil
}

// currentClaimStatus returns the claim's stored status, or "" when the
// claim does not yet exist (meaning the incoming write is a fresh insert
// and the transition row will record an empty from_status).
func currentClaimStatus(ctx context.Context, tx *sql.Tx, claimID string) (string, error) {
	var status string
	err := tx.QueryRowContext(ctx, `SELECT status FROM claims WHERE id = ?`, claimID).Scan(&status)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return status, err
}

// UpsertEvidence inserts or updates claim-to-event evidence links in a single transaction.
func (r ClaimRepository) UpsertEvidence(ctx context.Context, links []domain.ClaimEvidence) error {
	if len(links) == 0 {
		return nil
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin claim evidence tx: %w", err)
	}
	defer rollbackTx(tx)

	qtx := r.q.WithTx(tx)

	for _, link := range links {
		if err := link.Validate(); err != nil {
			return fmt.Errorf("invalid claim evidence: %w", err)
		}
		err := qtx.UpsertClaimEvidence(ctx, sqlcgen.UpsertClaimEvidenceParams{
			ClaimID: link.ClaimID,
			EventID: link.EventID,
		})
		if err != nil {
			return fmt.Errorf("upsert claim evidence (%s,%s): %w", link.ClaimID, link.EventID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit claim evidence tx: %w", err)
	}

	return nil
}

// ListByEventIDs returns all claims linked to the given event IDs via claim evidence.
func (r ClaimRepository) ListByEventIDs(ctx context.Context, eventIDs []string) ([]domain.Claim, error) {
	if len(eventIDs) == 0 {
		return []domain.Claim{}, nil
	}

	placeholders := make([]string, 0, len(eventIDs))
	args := make([]any, 0, len(eventIDs))
	for _, id := range eventIDs {
		placeholders = append(placeholders, "?")
		args = append(args, id)
	}

	query := fmt.Sprintf(`
SELECT DISTINCT c.id, c.text, c.type, c.confidence, c.status, c.created_at, c.created_by, c.trust_score, c.valid_from, c.valid_to
FROM claims c
JOIN claim_evidence ce ON ce.claim_id = c.id
WHERE ce.event_id IN (%s)
ORDER BY c.created_at ASC`, strings.Join(placeholders, ",")) //nolint:gosec // G201: placeholders are literal "?" strings, not user input

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list claims by event ids: %w", err)
	}
	defer closeRows(rows)

	claims := make([]domain.Claim, 0)
	for rows.Next() {
		claim, err := scanClaim(rows)
		if err != nil {
			return nil, err
		}
		claims = append(claims, claim)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate claims rows: %w", err)
	}

	return claims, nil
}

// ListStatusHistoryByClaimID returns the claim's status transitions in
// chronological order (oldest first). An empty slice means either the
// claim doesn't exist, or it exists but its status has never changed
// (pre-existing claims from before the history table was added fall into
// this bucket).
func (r ClaimRepository) ListStatusHistoryByClaimID(ctx context.Context, claimID string) ([]domain.ClaimStatusTransition, error) {
	// Order by id, not changed_at: id is AUTOINCREMENT so it reflects
	// insertion order exactly. RFC3339Nano string sort is theoretically
	// correct too, but two upserts in the same millisecond can collide,
	// and id always disambiguates.
	rows, err := r.db.QueryContext(ctx,
		`SELECT claim_id, from_status, to_status, changed_at, reason
		 FROM claim_status_history
		 WHERE claim_id = ?
		 ORDER BY id ASC`, claimID)
	if err != nil {
		return nil, fmt.Errorf("list status history for %s: %w", claimID, err)
	}
	defer closeRows(rows)

	out := make([]domain.ClaimStatusTransition, 0)
	for rows.Next() {
		var (
			cid, from, to, changedAt, reason string
		)
		if err := rows.Scan(&cid, &from, &to, &changedAt, &reason); err != nil {
			return nil, fmt.Errorf("scan status history row: %w", err)
		}
		t, err := time.Parse(time.RFC3339Nano, changedAt)
		if err != nil {
			return nil, fmt.Errorf("parse status history changed_at: %w", err)
		}
		out = append(out, domain.ClaimStatusTransition{
			ClaimID:    cid,
			FromStatus: domain.ClaimStatus(from),
			ToStatus:   domain.ClaimStatus(to),
			ChangedAt:  t,
			Reason:     reason,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate status history rows: %w", err)
	}
	return out, nil
}

// ListEvidenceByClaimIDs returns the (claim_id, event_id) link rows for the
// given claim IDs. Used by the query engine to attribute claim provenance
// back to the events they were extracted from.
func (r ClaimRepository) ListEvidenceByClaimIDs(ctx context.Context, claimIDs []string) ([]domain.ClaimEvidence, error) {
	if len(claimIDs) == 0 {
		return []domain.ClaimEvidence{}, nil
	}

	placeholders := make([]string, 0, len(claimIDs))
	args := make([]any, 0, len(claimIDs))
	for _, id := range claimIDs {
		placeholders = append(placeholders, "?")
		args = append(args, id)
	}

	query := fmt.Sprintf(`
SELECT claim_id, event_id
FROM claim_evidence
WHERE claim_id IN (%s)`, strings.Join(placeholders, ",")) //nolint:gosec // G201: placeholders are literal "?" strings, not user input

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list evidence by claim ids: %w", err)
	}
	defer closeRows(rows)

	out := make([]domain.ClaimEvidence, 0)
	for rows.Next() {
		var ev domain.ClaimEvidence
		if err := rows.Scan(&ev.ClaimID, &ev.EventID); err != nil {
			return nil, fmt.Errorf("scan claim evidence row: %w", err)
		}
		out = append(out, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate claim evidence rows: %w", err)
	}

	return out, nil
}

// ListByIDs returns the claims with the given IDs (in unspecified order).
// Used by the query engine for hop-expanded claim lookup — given a set of
// neighbor claim IDs from relationship traversal, materialize the full
// Claim records.
func (r ClaimRepository) ListByIDs(ctx context.Context, claimIDs []string) ([]domain.Claim, error) {
	if len(claimIDs) == 0 {
		return []domain.Claim{}, nil
	}

	placeholders := make([]string, 0, len(claimIDs))
	args := make([]any, 0, len(claimIDs))
	for _, id := range claimIDs {
		placeholders = append(placeholders, "?")
		args = append(args, id)
	}

	query := fmt.Sprintf(`
SELECT id, text, type, confidence, status, created_at, created_by, trust_score, valid_from, valid_to
FROM claims
WHERE id IN (%s)`, strings.Join(placeholders, ",")) //nolint:gosec // G201: placeholders are literal "?" strings, not user input

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list claims by ids: %w", err)
	}
	defer closeRows(rows)

	out := make([]domain.Claim, 0, len(claimIDs))
	for rows.Next() {
		c, err := scanClaim(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate claims by ids rows: %w", err)
	}
	return out, nil
}

// SetValidity sets a claim's valid_to timestamp. A zero `validTo`
// clears the column (un-supersedes the claim) — useful when an
// operator reverts a resolution. Returns an error if the claim does
// not exist; callers that don't care should ignore sql.ErrNoRows.
func (r ClaimRepository) SetValidity(ctx context.Context, claimID string, validTo time.Time) error {
	val := sql.NullString{}
	if !validTo.IsZero() {
		val = sql.NullString{String: validTo.UTC().Format(time.RFC3339Nano), Valid: true}
	}
	return r.q.SetClaimValidity(ctx, sqlcgen.SetClaimValidityParams{
		ValidTo: val,
		ID:      claimID,
	})
}

// RecomputeTrust recalculates trust_score for every claim based on its
// confidence, the count of distinct corroborating events, and the
// freshness of the most recent evidence. Returns the number of claims
// touched. Caller supplies the scoring function (typically
// trust.Score) so the repository stays free of policy decisions.
func (r ClaimRepository) RecomputeTrust(ctx context.Context, score func(confidence float64, evidenceCount int, latestEvidence time.Time) float64) (int, error) {
	rows, err := r.q.ListClaimTrustInputs(ctx)
	if err != nil {
		return 0, fmt.Errorf("list trust inputs: %w", err)
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	qtx := r.q.WithTx(tx)
	for _, row := range rows {
		var latest time.Time
		if row.LatestEvidenceAt != "" {
			if t, perr := time.Parse(time.RFC3339Nano, row.LatestEvidenceAt); perr == nil {
				latest = t
			}
		}
		s := score(row.Confidence, int(row.EvidenceCount), latest)
		if err := qtx.UpdateClaimTrust(ctx, sqlcgen.UpdateClaimTrustParams{
			TrustScore: s,
			ID:         row.ClaimID,
		}); err != nil {
			return 0, fmt.Errorf("update trust for %s: %w", row.ClaimID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit trust update: %w", err)
	}
	return len(rows), nil
}

// AverageTrust returns the mean trust_score across all claims; 0 when
// the table is empty.
func (r ClaimRepository) AverageTrust(ctx context.Context) (float64, error) {
	return r.q.AverageTrust(ctx)
}

// CountClaimsBelowTrust returns how many claims fall under the given
// trust_score threshold. Useful for the metrics output and for
// surfacing low-quality knowledge to the user.
func (r ClaimRepository) CountClaimsBelowTrust(ctx context.Context, threshold float64) (int64, error) {
	return r.q.CountClaimsBelowTrust(ctx, threshold)
}

// RepointEvidence rewrites every claim_evidence row pointing at
// fromClaimID to point at toClaimID, then deletes the original
// rows. Idempotent on the (claim_id, event_id) primary key —
// duplicate evidence collapses silently. Single-tx so partial
// failures don't leave dangling pointers.
func (r ClaimRepository) RepointEvidence(ctx context.Context, fromClaimID, toClaimID string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin repoint evidence tx: %w", err)
	}
	defer rollbackTx(tx)
	if _, err := tx.ExecContext(ctx,
		`INSERT OR IGNORE INTO claim_evidence (claim_id, event_id)
		 SELECT ?, event_id FROM claim_evidence WHERE claim_id = ?`,
		toClaimID, fromClaimID,
	); err != nil {
		return fmt.Errorf("copy evidence %s -> %s: %w", fromClaimID, toClaimID, err)
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM claim_evidence WHERE claim_id = ?`, fromClaimID,
	); err != nil {
		return fmt.Errorf("delete original evidence %s: %w", fromClaimID, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit repoint evidence tx: %w", err)
	}
	return nil
}

// DeleteCascade removes a claim and the rows owned only by it
// (claim_evidence by claim_id, claim_status_history by claim_id,
// the claim row itself). Embeddings, relationships, and
// claim_entities are owned by other repositories — callers must
// clean those up separately. Single-tx for atomicity.
func (r ClaimRepository) DeleteCascade(ctx context.Context, claimID string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin claim delete tx: %w", err)
	}
	defer rollbackTx(tx)
	if _, err := tx.ExecContext(ctx, `DELETE FROM claim_evidence WHERE claim_id = ?`, claimID); err != nil {
		return fmt.Errorf("delete claim_evidence: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM claim_status_history WHERE claim_id = ?`, claimID); err != nil {
		return fmt.Errorf("delete claim_status_history: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM claims WHERE id = ?`, claimID); err != nil {
		return fmt.Errorf("delete claim: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit claim delete tx: %w", err)
	}
	return nil
}

// CountAll returns the total number of claims stored.
func (r ClaimRepository) CountAll(ctx context.Context) (int64, error) {
	var n int64
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM claims`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count claims: %w", err)
	}
	return n, nil
}

// ListAllEvidence returns every (claim_id, event_id) link in
// claim_evidence. Order is undefined.
func (r ClaimRepository) ListAllEvidence(ctx context.Context) ([]domain.ClaimEvidence, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT claim_id, event_id FROM claim_evidence`)
	if err != nil {
		return nil, fmt.Errorf("list all claim evidence: %w", err)
	}
	defer closeRows(rows)

	out := make([]domain.ClaimEvidence, 0)
	for rows.Next() {
		var ev domain.ClaimEvidence
		if err := rows.Scan(&ev.ClaimID, &ev.EventID); err != nil {
			return nil, fmt.Errorf("scan claim_evidence row: %w", err)
		}
		out = append(out, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate claim_evidence rows: %w", err)
	}
	return out, nil
}

// DeleteAll wipes claims plus the rows owned by claims (claim_evidence,
// claim_status_history) inside a single transaction. Caller is
// responsible for cross-repo cleanup (relationships pointing at
// claims, embeddings keyed on claim id).
func (r ClaimRepository) DeleteAll(ctx context.Context) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin claims delete-all tx: %w", err)
	}
	defer rollbackTx(tx)
	if _, err := tx.ExecContext(ctx, `DELETE FROM claim_evidence`); err != nil {
		return fmt.Errorf("delete claim_evidence: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM claim_status_history`); err != nil {
		return fmt.Errorf("delete claim_status_history: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM claims`); err != nil {
		return fmt.Errorf("delete claims: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit claims delete-all tx: %w", err)
	}
	return nil
}

// ListIDsMissingEmbedding returns claim ids that have no embedding
// row. Backed by a single anti-join query.
func (r ClaimRepository) ListIDsMissingEmbedding(ctx context.Context) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT c.id FROM claims c
		LEFT JOIN embeddings e ON e.entity_id = c.id AND e.entity_type = 'claim'
		WHERE e.entity_id IS NULL
		ORDER BY c.created_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("list ids missing embedding: %w", err)
	}
	defer closeRows(rows)
	out := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan id: %w", err)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// ListAllStatusHistory returns every claim_status_history row in
// chronological order. Used by `mnemos audit who` to filter by
// ChangedBy in-process — we keep the filter caller-side so the
// port surface stays small.
func (r ClaimRepository) ListAllStatusHistory(ctx context.Context) ([]domain.ClaimStatusTransition, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT claim_id, from_status, to_status, changed_at, reason, changed_by
		 FROM claim_status_history
		 ORDER BY id ASC`)
	if err != nil {
		return nil, fmt.Errorf("list all status history: %w", err)
	}
	defer closeRows(rows)

	out := make([]domain.ClaimStatusTransition, 0)
	for rows.Next() {
		var (
			cid, from, to, changedAt, reason, changedBy string
		)
		if err := rows.Scan(&cid, &from, &to, &changedAt, &reason, &changedBy); err != nil {
			return nil, fmt.Errorf("scan status_history row: %w", err)
		}
		t, err := time.Parse(time.RFC3339Nano, changedAt)
		if err != nil {
			return nil, fmt.Errorf("parse changed_at: %w", err)
		}
		out = append(out, domain.ClaimStatusTransition{
			ClaimID:    cid,
			FromStatus: domain.ClaimStatus(from),
			ToStatus:   domain.ClaimStatus(to),
			ChangedAt:  t,
			Reason:     reason,
			ChangedBy:  changedBy,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate status_history rows: %w", err)
	}
	return out, nil
}

// ListAll returns every claim stored in the database.
func (r ClaimRepository) ListAll(ctx context.Context) ([]domain.Claim, error) {
	rows, err := r.q.ListAllClaims(ctx)
	if err != nil {
		return nil, fmt.Errorf("list all claims: %w", err)
	}

	claims := make([]domain.Claim, 0, len(rows))
	for _, row := range rows {
		claim, err := mapSQLClaim(row)
		if err != nil {
			return nil, err
		}
		claims = append(claims, claim)
	}

	return claims, nil
}

func mapSQLClaim(row sqlcgen.Claim) (domain.Claim, error) {
	claim := domain.Claim{
		ID:         row.ID,
		Text:       row.Text,
		Type:       domain.ClaimType(row.Type),
		Confidence: row.Confidence,
		Status:     domain.ClaimStatus(row.Status),
		CreatedBy:  row.CreatedBy,
		TrustScore: row.TrustScore,
	}

	t, err := time.Parse(time.RFC3339Nano, row.CreatedAt)
	if err != nil {
		return domain.Claim{}, fmt.Errorf("parse claim created_at: %w", err)
	}
	claim.CreatedAt = t

	if vf, perr := parseOptionalTime(row.ValidFrom); perr != nil {
		return domain.Claim{}, fmt.Errorf("parse claim valid_from: %w", perr)
	} else {
		claim.ValidFrom = vf
	}
	if row.ValidTo.Valid {
		if vt, perr := parseOptionalTime(row.ValidTo.String); perr != nil {
			return domain.Claim{}, fmt.Errorf("parse claim valid_to: %w", perr)
		} else {
			claim.ValidTo = vt
		}
	}

	if err := claim.Validate(); err != nil {
		return domain.Claim{}, fmt.Errorf("validate persisted claim %s: %w", claim.ID, err)
	}

	return claim, nil
}

// parseOptionalTime returns the zero time for empty strings (the
// sentinel produced by ALTER TABLE ADD COLUMN ... DEFAULT ” on
// legacy rows that haven't been touched since the v0.8 migration ran
// the backfill, and the storage form for "no upper bound" on
// valid_to). RFC3339Nano otherwise.
func parseOptionalTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339Nano, s)
}

type claimRowScanner interface {
	Scan(dest ...any) error
}

func scanClaim(scanner claimRowScanner) (domain.Claim, error) {
	var (
		claim     domain.Claim
		claimType string
		status    string
		createdAt string
		validFrom string
		validTo   sql.NullString
	)

	if err := scanner.Scan(
		&claim.ID,
		&claim.Text,
		&claimType,
		&claim.Confidence,
		&status,
		&createdAt,
		&claim.CreatedBy,
		&claim.TrustScore,
		&validFrom,
		&validTo,
	); err != nil {
		return domain.Claim{}, err
	}

	claim.Type = domain.ClaimType(claimType)
	claim.Status = domain.ClaimStatus(status)

	t, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return domain.Claim{}, fmt.Errorf("parse claim created_at: %w", err)
	}
	claim.CreatedAt = t

	if vf, perr := parseOptionalTime(validFrom); perr != nil {
		return domain.Claim{}, fmt.Errorf("parse claim valid_from: %w", perr)
	} else {
		claim.ValidFrom = vf
	}
	if validTo.Valid {
		if vt, perr := parseOptionalTime(validTo.String); perr != nil {
			return domain.Claim{}, fmt.Errorf("parse claim valid_to: %w", perr)
		} else {
			claim.ValidTo = vt
		}
	}

	if err := claim.Validate(); err != nil {
		return domain.Claim{}, fmt.Errorf("validate persisted claim %s: %w", claim.ID, err)
	}

	return claim, nil
}
