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

// Upsert inserts or updates the given claims in a single transaction.
func (r ClaimRepository) Upsert(claims []domain.Claim) error {
	if len(claims) == 0 {
		return nil
	}

	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin claim upsert tx: %w", err)
	}
	defer rollbackTx(tx)

	qtx := r.q.WithTx(tx)

	for _, claim := range claims {
		if err := claim.Validate(); err != nil {
			return fmt.Errorf("invalid claim %s: %w", claim.ID, err)
		}
		err := qtx.UpsertClaim(context.Background(), sqlcgen.UpsertClaimParams{
			ID:         claim.ID,
			Text:       claim.Text,
			Type:       string(claim.Type),
			Confidence: claim.Confidence,
			Status:     string(claim.Status),
			CreatedAt:  claim.CreatedAt.UTC().Format(time.RFC3339Nano),
		})
		if err != nil {
			return fmt.Errorf("upsert claim %s: %w", claim.ID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit claim upsert tx: %w", err)
	}

	return nil
}

// UpsertEvidence inserts or updates claim-to-event evidence links in a single transaction.
func (r ClaimRepository) UpsertEvidence(links []domain.ClaimEvidence) error {
	if len(links) == 0 {
		return nil
	}

	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin claim evidence tx: %w", err)
	}
	defer rollbackTx(tx)

	qtx := r.q.WithTx(tx)

	for _, link := range links {
		if err := link.Validate(); err != nil {
			return fmt.Errorf("invalid claim evidence: %w", err)
		}
		err := qtx.UpsertClaimEvidence(context.Background(), sqlcgen.UpsertClaimEvidenceParams{
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
ORDER BY c.created_at ASC`, strings.Join(placeholders, ",")) //nolint:gosec // G201: placeholders are literal "?" strings, not user input

	rows, err := r.db.Query(query, args...)
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

// ListAll returns every claim stored in the database.
func (r ClaimRepository) ListAll() ([]domain.Claim, error) {
	rows, err := r.q.ListAllClaims(context.Background())
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
	}

	t, err := time.Parse(time.RFC3339Nano, row.CreatedAt)
	if err != nil {
		return domain.Claim{}, fmt.Errorf("parse claim created_at: %w", err)
	}
	claim.CreatedAt = t

	if err := claim.Validate(); err != nil {
		return domain.Claim{}, fmt.Errorf("validate persisted claim %s: %w", claim.ID, err)
	}

	return claim, nil
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
