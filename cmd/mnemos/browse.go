package main

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/felixgeelhaar/mnemos/internal/domain"
	"github.com/felixgeelhaar/mnemos/internal/store/sqlite"
)

const (
	defaultListLimit = 50
	maxListLimit     = 200
)

type mcpListClaimsInput struct {
	Type   string `json:"type,omitempty" jsonschema:"description=Filter by claim type: fact, hypothesis, or decision"`
	Status string `json:"status,omitempty" jsonschema:"description=Filter by claim status: active, contested, or deprecated"`
	Limit  int    `json:"limit,omitempty" jsonschema:"description=Max number of claims to return (default 50, cap 200)"`
	Offset int    `json:"offset,omitempty" jsonschema:"description=Number of claims to skip"`
}

type mcpListClaimsOutput struct {
	Claims []domain.Claim `json:"claims"`
	Total  int            `json:"total"`
	Limit  int            `json:"limit"`
	Offset int            `json:"offset"`
}

type mcpListContradictionsInput struct {
	Limit  int `json:"limit,omitempty" jsonschema:"description=Max number of contradictions to return (default 50, cap 200)"`
	Offset int `json:"offset,omitempty" jsonschema:"description=Number of contradictions to skip"`
}

type mcpContradictionPair struct {
	RelationshipID string `json:"relationshipId"`
	FromClaimID    string `json:"fromClaimId"`
	FromClaimText  string `json:"fromClaimText"`
	ToClaimID      string `json:"toClaimId"`
	ToClaimText    string `json:"toClaimText"`
	CreatedAt      string `json:"createdAt"`
}

type mcpListContradictionsOutput struct {
	Contradictions []mcpContradictionPair `json:"contradictions"`
	Total          int                    `json:"total"`
	Limit          int                    `json:"limit"`
	Offset         int                    `json:"offset"`
}

func mcpRunListClaims(ctx context.Context, input mcpListClaimsInput) (mcpListClaimsOutput, error) {
	limit, offset := normalizePagination(input.Limit, input.Offset)

	if input.Type != "" && !validClaimType(input.Type) {
		return mcpListClaimsOutput{}, fmt.Errorf("invalid type %q (want fact, hypothesis, or decision)", input.Type)
	}
	if input.Status != "" && !validClaimStatus(input.Status) {
		return mcpListClaimsOutput{}, fmt.Errorf("invalid status %q (want active, contested, or deprecated)", input.Status)
	}

	db, err := sqlite.Open(resolveDBPath())
	if err != nil {
		return mcpListClaimsOutput{}, err
	}
	defer func() { _ = db.Close() }()

	claims, total, err := listClaimsFiltered(ctx, db, input.Type, input.Status, limit, offset)
	if err != nil {
		return mcpListClaimsOutput{}, err
	}
	return mcpListClaimsOutput{
		Claims: claims,
		Total:  total,
		Limit:  limit,
		Offset: offset,
	}, nil
}

func mcpRunListContradictions(ctx context.Context, input mcpListContradictionsInput) (mcpListContradictionsOutput, error) {
	limit, offset := normalizePagination(input.Limit, input.Offset)

	db, err := sqlite.Open(resolveDBPath())
	if err != nil {
		return mcpListContradictionsOutput{}, err
	}
	defer func() { _ = db.Close() }()

	pairs, total, err := listContradictionPairs(ctx, db, limit, offset)
	if err != nil {
		return mcpListContradictionsOutput{}, err
	}
	return mcpListContradictionsOutput{
		Contradictions: pairs,
		Total:          total,
		Limit:          limit,
		Offset:         offset,
	}, nil
}

func listClaimsFiltered(ctx context.Context, db *sql.DB, claimType, status string, limit, offset int) ([]domain.Claim, int, error) {
	var (
		where []string
		args  []any
	)
	if claimType != "" {
		where = append(where, "type = ?")
		args = append(args, claimType)
	}
	if status != "" {
		where = append(where, "status = ?")
		args = append(args, status)
	}
	whereClause := ""
	if len(where) > 0 {
		whereClause = " WHERE " + strings.Join(where, " AND ")
	}

	var total int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM claims"+whereClause, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count claims: %w", err)
	}

	rowsArgs := append(append([]any{}, args...), limit, offset)
	rows, err := db.QueryContext(ctx,
		"SELECT id, text, type, confidence, status, created_at FROM claims"+whereClause+" ORDER BY created_at DESC LIMIT ? OFFSET ?",
		rowsArgs...,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("list claims: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var claims []domain.Claim
	for rows.Next() {
		var (
			c          domain.Claim
			typeStr    string
			statusStr  string
			createdStr string
		)
		if err := rows.Scan(&c.ID, &c.Text, &typeStr, &c.Confidence, &statusStr, &createdStr); err != nil {
			return nil, 0, fmt.Errorf("scan claim: %w", err)
		}
		c.Type = domain.ClaimType(typeStr)
		c.Status = domain.ClaimStatus(statusStr)
		// Timestamps stay as strings on the wire — domain.Claim's CreatedAt
		// type isn't worth round-tripping for browse output.
		_ = createdStr
		claims = append(claims, c)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate claims: %w", err)
	}
	return claims, total, nil
}

func listContradictionPairs(ctx context.Context, db *sql.DB, limit, offset int) ([]mcpContradictionPair, int, error) {
	var total int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM relationships WHERE type = 'contradicts'`).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count contradictions: %w", err)
	}

	rows, err := db.QueryContext(ctx, `
		SELECT r.id, r.from_claim_id, COALESCE(cf.text, ''), r.to_claim_id, COALESCE(ct.text, ''), r.created_at
		FROM relationships r
		LEFT JOIN claims cf ON cf.id = r.from_claim_id
		LEFT JOIN claims ct ON ct.id = r.to_claim_id
		WHERE r.type = 'contradicts'
		ORDER BY r.created_at DESC
		LIMIT ? OFFSET ?
	`, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list contradictions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var pairs []mcpContradictionPair
	for rows.Next() {
		var p mcpContradictionPair
		if err := rows.Scan(&p.RelationshipID, &p.FromClaimID, &p.FromClaimText, &p.ToClaimID, &p.ToClaimText, &p.CreatedAt); err != nil {
			return nil, 0, fmt.Errorf("scan contradiction: %w", err)
		}
		pairs = append(pairs, p)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate contradictions: %w", err)
	}
	return pairs, total, nil
}

func normalizePagination(limit, offset int) (int, int) {
	if limit <= 0 {
		limit = defaultListLimit
	}
	if limit > maxListLimit {
		limit = maxListLimit
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

func validClaimType(s string) bool {
	switch domain.ClaimType(s) {
	case domain.ClaimTypeFact, domain.ClaimTypeHypothesis, domain.ClaimTypeDecision:
		return true
	}
	return false
}

func validClaimStatus(s string) bool {
	switch domain.ClaimStatus(s) {
	case domain.ClaimStatusActive, domain.ClaimStatusContested, domain.ClaimStatusDeprecated:
		return true
	}
	return false
}
