package extract

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/felixgeelhaar/mnemos/internal/domain"
)

type Engine struct {
	now    func() time.Time
	nextID func() (string, error)
}

func NewEngine() Engine {
	return Engine{
		now:    time.Now,
		nextID: newClaimID,
	}
}

func (e Engine) Extract(events []domain.Event) ([]domain.Claim, []domain.ClaimEvidence, error) {
	claims := make([]domain.Claim, 0, len(events))
	evidence := make([]domain.ClaimEvidence, 0, len(events))

	for _, event := range events {
		content := strings.TrimSpace(event.Content)
		if content == "" {
			continue
		}

		claimID, err := e.nextID()
		if err != nil {
			return nil, nil, fmt.Errorf("generate claim id: %w", err)
		}

		claimType := inferClaimType(content)
		claim := domain.Claim{
			ID:         claimID,
			Text:       content,
			Type:       claimType,
			Confidence: inferConfidence(claimType),
			Status:     domain.ClaimStatusActive,
			CreatedAt:  e.now().UTC(),
		}
		if err := claim.Validate(); err != nil {
			return nil, nil, fmt.Errorf("validate extracted claim: %w", err)
		}

		ce := domain.ClaimEvidence{ClaimID: claim.ID, EventID: event.ID}
		if err := ce.Validate(); err != nil {
			return nil, nil, fmt.Errorf("validate claim evidence: %w", err)
		}

		claims = append(claims, claim)
		evidence = append(evidence, ce)
	}

	return claims, evidence, nil
}

func (e Engine) ExtractClaims(events []domain.Event) ([]domain.Claim, error) {
	claims, _, err := e.Extract(events)
	if err != nil {
		return nil, err
	}
	return claims, nil
}

func inferClaimType(text string) domain.ClaimType {
	lower := strings.ToLower(text)
	if strings.Contains(lower, "decide") || strings.Contains(lower, "decision") || strings.Contains(lower, "we will") {
		return domain.ClaimTypeDecision
	}
	if strings.Contains(lower, "might") || strings.Contains(lower, "maybe") || strings.Contains(lower, "hypothesis") || strings.Contains(lower, "assume") {
		return domain.ClaimTypeHypothesis
	}
	return domain.ClaimTypeFact
}

func inferConfidence(claimType domain.ClaimType) float64 {
	switch claimType {
	case domain.ClaimTypeDecision:
		return 0.9
	case domain.ClaimTypeHypothesis:
		return 0.6
	default:
		return 0.8
	}
}

func newClaimID() (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "cl_" + hex.EncodeToString(buf), nil
}
