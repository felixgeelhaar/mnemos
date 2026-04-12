package sqlite

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/felixgeelhaar/mnemos/internal/domain"
)

type ClaimRepository struct {
	db *sql.DB
}

func NewClaimRepository(db *sql.DB) ClaimRepository {
	return ClaimRepository{db: db}
}

func (r ClaimRepository) Upsert(claims []domain.Claim) error {
	if len(claims) == 0 {
		return nil
	}

	const upsert = `
INSERT INTO claims (id, text, type, confidence, status, created_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
	text = excluded.text,
	type = excluded.type,
	confidence = excluded.confidence,
	status = excluded.status,
	created_at = excluded.created_at`

	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin claim upsert tx: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(upsert)
	if err != nil {
		return fmt.Errorf("prepare claim upsert: %w", err)
	}
	defer stmt.Close()

	for _, claim := range claims {
		if err := claim.Validate(); err != nil {
			return fmt.Errorf("invalid claim %s: %w", claim.ID, err)
		}
		_, err := stmt.Exec(
			claim.ID,
			claim.Text,
			string(claim.Type),
			claim.Confidence,
			string(claim.Status),
			claim.CreatedAt.UTC().Format(time.RFC3339Nano),
		)
		if err != nil {
			return fmt.Errorf("upsert claim %s: %w", claim.ID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit claim upsert tx: %w", err)
	}

	return nil
}

func (r ClaimRepository) UpsertEvidence(links []domain.ClaimEvidence) error {
	if len(links) == 0 {
		return nil
	}

	const upsert = `
INSERT INTO claim_evidence (claim_id, event_id)
VALUES (?, ?)
ON CONFLICT(claim_id, event_id) DO NOTHING`

	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin claim evidence tx: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(upsert)
	if err != nil {
		return fmt.Errorf("prepare claim evidence upsert: %w", err)
	}
	defer stmt.Close()

	for _, link := range links {
		if err := link.Validate(); err != nil {
			return fmt.Errorf("invalid claim evidence: %w", err)
		}
		_, err := stmt.Exec(link.ClaimID, link.EventID)
		if err != nil {
			return fmt.Errorf("upsert claim evidence (%s,%s): %w", link.ClaimID, link.EventID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit claim evidence tx: %w", err)
	}

	return nil
}

func (r ClaimRepository) ListByEventIDs(eventIDs []string) ([]domain.Claim, error) {
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
SELECT DISTINCT c.id, c.text, c.type, c.confidence, c.status, c.created_at
FROM claims c
JOIN claim_evidence ce ON ce.claim_id = c.id
WHERE ce.event_id IN (%s)
ORDER BY c.created_at ASC`, strings.Join(placeholders, ","))

	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list claims by event ids: %w", err)
	}
	defer rows.Close()

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

func (r ClaimRepository) ListAll() ([]domain.Claim, error) {
	const query = `
SELECT id, text, type, confidence, status, created_at
FROM claims
ORDER BY created_at ASC`

	rows, err := r.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("list all claims: %w", err)
	}
	defer rows.Close()

	claims := make([]domain.Claim, 0)
	for rows.Next() {
		claim, err := scanClaim(rows)
		if err != nil {
			return nil, err
		}
		claims = append(claims, claim)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate all claims rows: %w", err)
	}

	return claims, nil
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
	)

	if err := scanner.Scan(
		&claim.ID,
		&claim.Text,
		&claimType,
		&claim.Confidence,
		&status,
		&createdAt,
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

	if err := claim.Validate(); err != nil {
		return domain.Claim{}, fmt.Errorf("validate persisted claim %s: %w", claim.ID, err)
	}

	return claim, nil
}
