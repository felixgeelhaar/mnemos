package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/felixgeelhaar/mnemos/internal/domain"
)

// ClaimRepository persists claims, claim evidence links, and
// claim_status_history. Trust scoring (RecomputeTrust /
// AverageTrust / CountClaimsBelowTrust) is implemented so this
// repository satisfies ports.TrustScorer.
type ClaimRepository struct {
	db *sql.DB
	ns string
}

// Upsert satisfies the corresponding ports method.
func (r ClaimRepository) Upsert(ctx context.Context, claims []domain.Claim) error {
	return r.upsertWithReason(ctx, claims, "", "")
}

// UpsertWithReason satisfies the corresponding ports method.
func (r ClaimRepository) UpsertWithReason(ctx context.Context, claims []domain.Claim, reason string) error {
	return r.upsertWithReason(ctx, claims, reason, "")
}

// UpsertWithReasonAs satisfies the corresponding ports method.
func (r ClaimRepository) UpsertWithReasonAs(ctx context.Context, claims []domain.Claim, reason, changedBy string) error {
	return r.upsertWithReason(ctx, claims, reason, changedBy)
}

// upsertWithReason satisfies the corresponding ports method.
func (r ClaimRepository) upsertWithReason(ctx context.Context, claims []domain.Claim, reason, changedBy string) error {
	if len(claims) == 0 {
		return nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin claim upsert tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC()
	upsert := fmt.Sprintf(`
INSERT INTO %s (id, text, type, confidence, status, created_at, created_by, valid_from, trust_score, valid_to)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 0, NULL)
ON CONFLICT (id) DO UPDATE SET
  text = EXCLUDED.text,
  type = EXCLUDED.type,
  confidence = EXCLUDED.confidence,
  status = EXCLUDED.status,
  valid_from = EXCLUDED.valid_from`, qualify(r.ns, "claims"))
	historyInsert := fmt.Sprintf(`
INSERT INTO %s (claim_id, from_status, to_status, changed_at, reason, changed_by)
VALUES ($1, $2, $3, $4, $5, $6)`, qualify(r.ns, "claim_status_history"))
	priorQuery := fmt.Sprintf(`SELECT status FROM %s WHERE id = $1`, qualify(r.ns, "claims"))

	for _, claim := range claims {
		if err := claim.Validate(); err != nil {
			return fmt.Errorf("invalid claim %s: %w", claim.ID, err)
		}
		var priorStatus string
		err := tx.QueryRowContext(ctx, priorQuery, claim.ID).Scan(&priorStatus)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("look up prior status for %s: %w", claim.ID, err)
		}

		validFrom := claim.ValidFrom
		if validFrom.IsZero() {
			validFrom = claim.CreatedAt
		}
		if _, err := tx.ExecContext(ctx, upsert,
			claim.ID, claim.Text, string(claim.Type), claim.Confidence,
			string(claim.Status), claim.CreatedAt.UTC(), actorOr(claim.CreatedBy),
			validFrom.UTC(),
		); err != nil {
			return fmt.Errorf("upsert claim %s: %w", claim.ID, err)
		}

		newStatus := string(claim.Status)
		if priorStatus == newStatus {
			continue
		}
		if _, err := tx.ExecContext(ctx, historyInsert,
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

// UpsertEvidence inserts (claim, event) link rows. Idempotent.
func (r ClaimRepository) UpsertEvidence(ctx context.Context, links []domain.ClaimEvidence) error {
	if len(links) == 0 {
		return nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin evidence tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt := fmt.Sprintf(`
INSERT INTO %s (claim_id, event_id) VALUES ($1, $2)
ON CONFLICT (claim_id, event_id) DO NOTHING`, qualify(r.ns, "claim_evidence"))
	for _, link := range links {
		if err := link.Validate(); err != nil {
			return fmt.Errorf("invalid claim evidence: %w", err)
		}
		if _, err := tx.ExecContext(ctx, stmt, link.ClaimID, link.EventID); err != nil {
			return fmt.Errorf("upsert claim evidence (%s,%s): %w", link.ClaimID, link.EventID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit evidence tx: %w", err)
	}
	return nil
}

// ListByEventIDs returns the claims linked to any of the given event ids.
func (r ClaimRepository) ListByEventIDs(ctx context.Context, eventIDs []string) ([]domain.Claim, error) {
	if len(eventIDs) == 0 {
		return []domain.Claim{}, nil
	}
	q := fmt.Sprintf(`
SELECT DISTINCT c.id, c.text, c.type, c.confidence, c.status, c.created_at, c.created_by, c.trust_score, c.valid_from, c.valid_to
FROM %s c
JOIN %s ce ON ce.claim_id = c.id
WHERE ce.event_id = ANY($1)
ORDER BY c.created_at ASC`, qualify(r.ns, "claims"), qualify(r.ns, "claim_evidence"))
	rows, err := r.db.QueryContext(ctx, q, pgArray(eventIDs))
	if err != nil {
		return nil, fmt.Errorf("list claims by event ids: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return collectClaimRows(rows)
}

// ListEvidenceByClaimIDs satisfies the corresponding ports method.
func (r ClaimRepository) ListEvidenceByClaimIDs(ctx context.Context, claimIDs []string) ([]domain.ClaimEvidence, error) {
	if len(claimIDs) == 0 {
		return []domain.ClaimEvidence{}, nil
	}
	rows, err := r.db.QueryContext(ctx, fmt.Sprintf(`
SELECT claim_id, event_id FROM %s WHERE claim_id = ANY($1)`, qualify(r.ns, "claim_evidence")), pgArray(claimIDs))
	if err != nil {
		return nil, fmt.Errorf("list evidence by claim ids: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := make([]domain.ClaimEvidence, 0)
	for rows.Next() {
		var ev domain.ClaimEvidence
		if err := rows.Scan(&ev.ClaimID, &ev.EventID); err != nil {
			return nil, fmt.Errorf("scan claim evidence row: %w", err)
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

// ListByIDs satisfies the corresponding ports method.
func (r ClaimRepository) ListByIDs(ctx context.Context, claimIDs []string) ([]domain.Claim, error) {
	if len(claimIDs) == 0 {
		return []domain.Claim{}, nil
	}
	rows, err := r.db.QueryContext(ctx, fmt.Sprintf(`
SELECT id, text, type, confidence, status, created_at, created_by, trust_score, valid_from, valid_to
FROM %s WHERE id = ANY($1)`, qualify(r.ns, "claims")), pgArray(claimIDs))
	if err != nil {
		return nil, fmt.Errorf("list claims by ids: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return collectClaimRows(rows)
}

// ListAll satisfies the corresponding ports method.
func (r ClaimRepository) ListAll(ctx context.Context) ([]domain.Claim, error) {
	rows, err := r.db.QueryContext(ctx, fmt.Sprintf(`
SELECT id, text, type, confidence, status, created_at, created_by, trust_score, valid_from, valid_to
FROM %s ORDER BY created_at ASC`, qualify(r.ns, "claims")))
	if err != nil {
		return nil, fmt.Errorf("list all claims: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return collectClaimRows(rows)
}

// ListStatusHistoryByClaimID satisfies the corresponding ports method.
func (r ClaimRepository) ListStatusHistoryByClaimID(ctx context.Context, claimID string) ([]domain.ClaimStatusTransition, error) {
	rows, err := r.db.QueryContext(ctx, fmt.Sprintf(`
SELECT claim_id, from_status, to_status, changed_at, reason, changed_by
FROM %s WHERE claim_id = $1 ORDER BY id ASC`, qualify(r.ns, "claim_status_history")), claimID)
	if err != nil {
		return nil, fmt.Errorf("list status history for %s: %w", claimID, err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]domain.ClaimStatusTransition, 0)
	for rows.Next() {
		var t domain.ClaimStatusTransition
		var from, to string
		if err := rows.Scan(&t.ClaimID, &from, &to, &t.ChangedAt, &t.Reason, &t.ChangedBy); err != nil {
			return nil, fmt.Errorf("scan status history row: %w", err)
		}
		t.FromStatus = domain.ClaimStatus(from)
		t.ToStatus = domain.ClaimStatus(to)
		out = append(out, t)
	}
	return out, rows.Err()
}

// SetValidity satisfies the corresponding ports method.
func (r ClaimRepository) SetValidity(ctx context.Context, claimID string, validTo time.Time) error {
	var args []any
	var stmt string
	if validTo.IsZero() {
		stmt = fmt.Sprintf(`UPDATE %s SET valid_to = NULL WHERE id = $1`, qualify(r.ns, "claims"))
		args = []any{claimID}
	} else {
		stmt = fmt.Sprintf(`UPDATE %s SET valid_to = $1 WHERE id = $2`, qualify(r.ns, "claims"))
		args = []any{validTo.UTC(), claimID}
	}
	res, err := r.db.ExecContext(ctx, stmt, args...)
	if err != nil {
		return fmt.Errorf("set validity for %s: %w", claimID, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("claim %s: %w", claimID, sql.ErrNoRows)
	}
	return nil
}

// RecomputeTrust applies the supplied scoring function to every
// claim. Returns the count touched.
func (r ClaimRepository) RecomputeTrust(ctx context.Context, score func(confidence float64, evidenceCount int, latestEvidence time.Time) float64) (int, error) {
	rows, err := r.db.QueryContext(ctx, fmt.Sprintf(`
SELECT c.id, c.confidence, COUNT(DISTINCT ce.event_id), COALESCE(MAX(e.timestamp), 'epoch'::timestamptz)
FROM %s c
LEFT JOIN %s ce ON ce.claim_id = c.id
LEFT JOIN %s e ON e.id = ce.event_id
GROUP BY c.id, c.confidence`,
		qualify(r.ns, "claims"),
		qualify(r.ns, "claim_evidence"),
		qualify(r.ns, "events"),
	))
	if err != nil {
		return 0, fmt.Errorf("list trust inputs: %w", err)
	}
	type input struct {
		id         string
		confidence float64
		count      int
		latest     time.Time
	}
	var inputs []input
	for rows.Next() {
		var in input
		if err := rows.Scan(&in.id, &in.confidence, &in.count, &in.latest); err != nil {
			_ = rows.Close()
			return 0, fmt.Errorf("scan trust input: %w", err)
		}
		inputs = append(inputs, in)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate trust inputs: %w", err)
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin trust tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	stmt := fmt.Sprintf(`UPDATE %s SET trust_score = $1 WHERE id = $2`, qualify(r.ns, "claims"))
	for _, in := range inputs {
		s := score(in.confidence, in.count, in.latest)
		if _, err := tx.ExecContext(ctx, stmt, s, in.id); err != nil {
			return 0, fmt.Errorf("update trust for %s: %w", in.id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit trust update: %w", err)
	}
	return len(inputs), nil
}

// AverageTrust satisfies the corresponding ports method.
func (r ClaimRepository) AverageTrust(ctx context.Context) (float64, error) {
	var avg sql.NullFloat64
	err := r.db.QueryRowContext(ctx, fmt.Sprintf(`SELECT AVG(trust_score) FROM %s`, qualify(r.ns, "claims"))).Scan(&avg)
	if err != nil {
		return 0, fmt.Errorf("average trust: %w", err)
	}
	if !avg.Valid {
		return 0, nil
	}
	return avg.Float64, nil
}

// CountClaimsBelowTrust satisfies the corresponding ports method.
func (r ClaimRepository) CountClaimsBelowTrust(ctx context.Context, threshold float64) (int64, error) {
	var n int64
	err := r.db.QueryRowContext(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE trust_score < $1`, qualify(r.ns, "claims")), threshold).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count claims below trust: %w", err)
	}
	return n, nil
}

func collectClaimRows(rows *sql.Rows) ([]domain.Claim, error) {
	out := make([]domain.Claim, 0)
	for rows.Next() {
		c, err := scanClaimRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func scanClaimRow(rows *sql.Rows) (domain.Claim, error) {
	var c domain.Claim
	var typ, status string
	var validFrom sql.NullTime
	var validTo sql.NullTime
	if err := rows.Scan(
		&c.ID, &c.Text, &typ, &c.Confidence, &status,
		&c.CreatedAt, &c.CreatedBy, &c.TrustScore, &validFrom, &validTo,
	); err != nil {
		return domain.Claim{}, fmt.Errorf("scan claim row: %w", err)
	}
	c.Type = domain.ClaimType(typ)
	c.Status = domain.ClaimStatus(status)
	if validFrom.Valid {
		c.ValidFrom = validFrom.Time
	}
	if validTo.Valid {
		c.ValidTo = validTo.Time
	}
	if err := c.Validate(); err != nil {
		return domain.Claim{}, fmt.Errorf("validate persisted claim %s: %w", c.ID, err)
	}
	return c, nil
}
