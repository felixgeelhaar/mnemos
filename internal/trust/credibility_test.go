package trust

import (
	"strings"
	"testing"
	"time"

	"github.com/felixgeelhaar/mnemos/internal/domain"
)

func TestScoreCredibility_MoreSignalsIncreaseScore(t *testing.T) {
	now := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	base := CredibilityInputs{
		CurrentTrust:    0.6,
		SourceAuthority: 0.2,
		Liveness:        domain.LivenessDead,
		CitationCount:   0,
		CreatedAt:       now.Add(-360 * 24 * time.Hour),
		Now:             now,
	}
	high := CredibilityInputs{
		CurrentTrust:    0.6,
		SourceAuthority: 0.9,
		Liveness:        domain.LivenessLive,
		CitationCount:   8,
		LastExecuted:    now.Add(-3 * 24 * time.Hour),
		CreatedAt:       now.Add(-360 * 24 * time.Hour),
		Now:             now,
	}

	baseScore, _ := ScoreCredibility(base)
	highScore, rationale := ScoreCredibility(high)
	if highScore <= baseScore {
		t.Fatalf("expected enriched signals to increase score: base=%.3f high=%.3f", baseScore, highScore)
	}
	if !strings.Contains(rationale, "authority=") || !strings.Contains(rationale, "citations=") {
		t.Fatalf("rationale missing expected fields: %q", rationale)
	}
}

func TestScoreCredibility_AgentAuthority_DeflatesScore(t *testing.T) {
	now := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	in := CredibilityInputs{
		CurrentTrust:    0.8,
		SourceAuthority: 0.8,
		Liveness:        domain.LivenessLive,
		CitationCount:   3,
		LastExecuted:    now.Add(-1 * 24 * time.Hour),
		CreatedAt:       now.Add(-30 * 24 * time.Hour),
		Now:             now,
	}

	withoutAgent, _ := ScoreCredibility(in)

	in.AgentAuthority = 0.3 // low-authority agent
	withLowAgent, _ := ScoreCredibility(in)

	if withLowAgent >= withoutAgent {
		t.Fatalf("low agent authority should deflate score: without=%.3f with=%.3f", withoutAgent, withLowAgent)
	}
}

func TestScoreCredibility_AgentAuthority_ZeroIsNeutral(t *testing.T) {
	now := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	in := CredibilityInputs{
		CurrentTrust:    0.7,
		SourceAuthority: 0.7,
		Liveness:        domain.LivenessLive,
		CitationCount:   2,
		LastExecuted:    now.Add(-5 * 24 * time.Hour),
		CreatedAt:       now.Add(-20 * 24 * time.Hour),
		Now:             now,
		AgentAuthority:  0, // unknown — should not change score
	}

	scoreZero, _ := ScoreCredibility(in)
	in.AgentAuthority = 0 // explicit zero
	scoreExplicit, _ := ScoreCredibility(in)

	if scoreZero != scoreExplicit {
		t.Fatalf("zero agent authority must be neutral: got %v vs %v", scoreZero, scoreExplicit)
	}
}

func TestScoreCredibility_AgentAuthority_RationaleIncludesField(t *testing.T) {
	now := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	in := CredibilityInputs{
		CurrentTrust:   0.6,
		Liveness:       domain.LivenessLive,
		AgentAuthority: 0.85,
		Now:            now,
	}
	_, rationale := ScoreCredibility(in)
	if !strings.Contains(rationale, "agent_authority=") {
		t.Fatalf("rationale should include agent_authority field: %q", rationale)
	}
}
