package trust

import (
	"fmt"
	"math"
	"time"

	"github.com/felixgeelhaar/mnemos/internal/domain"
)

// CredibilityInputs contains provenance signals for source credibility scoring.
type CredibilityInputs struct {
	CurrentTrust    float64
	SourceAuthority float64
	// AgentAuthority is the authority score of the agent that submitted
	// the claim (domain.Agent.AuthorityScore). Zero means unknown — no
	// penalty is applied so existing callers that don't pass an agent
	// continue to behave as before.
	AgentAuthority float64
	Liveness       domain.LivenessStatus
	CitationCount  int
	LastExecuted   time.Time
	LastVerified   time.Time
	ValidFrom      time.Time
	CreatedAt      time.Time
	Now            time.Time
}

// ScoreCredibility combines trust + provenance signals into a score and
// human-readable rationale.
func ScoreCredibility(in CredibilityInputs) (float64, string) {
	now := in.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}

	base := clamp01(in.CurrentTrust)
	if base == 0 {
		base = 0.5
	}

	authority := clamp01(in.SourceAuthority)
	if in.SourceAuthority == 0 {
		authority = 0.5
	}

	citationSignal := clamp01(math.Log1p(float64(maxInt(0, in.CitationCount))) / math.Log(11))

	ref := EffectiveExecutionTime(in.LastExecuted, in.LastVerified, in.ValidFrom, in.CreatedAt)
	recencySignal := 0.5
	if !ref.IsZero() {
		days := now.Sub(ref).Hours() / 24
		if days < 0 {
			days = 0
		}
		recencySignal = clamp01(math.Exp(-days / 180.0))
	}

	livenessSignal := livenessWeight(in.Liveness)

	score := clamp01(
		base*0.55 +
			authority*0.15 +
			citationSignal*0.15 +
			recencySignal*0.10 +
			livenessSignal*0.05,
	)

	// AgentAuthority is a multiplicative final factor: an agent with a
	// known poor track record (low AuthorityScore) deflates the score;
	// a zero value means "unknown" — no penalty, treated as neutral 1.0.
	agentFactor := 1.0
	if in.AgentAuthority > 0 {
		agentFactor = clamp01(in.AgentAuthority)
	}
	score = clamp01(score * agentFactor)

	rationale := fmt.Sprintf(
		"base=%.2f authority=%.2f citations=%d(%.2f) recency=%.2f liveness=%s agent_authority=%.2f",
		base,
		authority,
		in.CitationCount,
		citationSignal,
		recencySignal,
		in.Liveness,
		agentFactor,
	)

	return score, rationale
}

func livenessWeight(s domain.LivenessStatus) float64 {
	switch s {
	case domain.LivenessLive:
		return 1.0
	case domain.LivenessStale:
		return 0.75
	case domain.LivenessZombie:
		return 0.65
	case domain.LivenessDead:
		return 0.25
	default:
		return 0.5
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
