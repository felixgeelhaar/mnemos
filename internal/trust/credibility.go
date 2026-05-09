package trust

import (
	"fmt"
	"math"
	"sort"
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

	// Test provenance — populated when the underlying claim is a
	// test_result. When TestLastRunAt is non-zero it overrides claim-level
	// recency: a test claim's recency should reflect when the test last
	// ran, not when the claim row was last touched. PassCount/FailCount
	// drive a separate decisiveness signal: a test that passed 50/50 is
	// less decisive than one that passed 50/0, even at equal recency.
	IsTest        bool
	TestLastRunAt time.Time
	TestPassCount int
	TestFailCount int
}

// Signal weights. Single source of truth for both ScoreCredibility (the
// numeric output) and BuildReport (the structured per-signal breakdown).
// Weights sum to 1.0 across the additive signals; AgentAuthority applies
// multiplicatively after the weighted sum.
const (
	wBase      = 0.50
	wAuthority = 0.15
	wCitation  = 0.13
	wRecency   = 0.10
	wLiveness  = 0.05
	wTest      = 0.07
)

// BuildReport computes score, structured per-signal breakdown, and a
// compact rationale string from CredibilityInputs in a single pass —
// the canonical implementation. ScoreCredibility is a thin wrapper that
// drops the signals slice; callers needing the breakdown (the query
// engine's WhyTrustClaim) call BuildReport directly. Keeping one
// implementation kills the historical drift between this package and
// internal/query/engine.go.
func BuildReport(in CredibilityInputs) (score float64, signals []domain.ProvenanceSignal, rationale string) {
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

	// Recency: for test_result claims with a recorded run timestamp, prefer
	// that over claim-level timestamps — a test that ran yesterday is more
	// trustworthy than one whose claim row was created yesterday but ran a
	// year ago. Falls back to EffectiveExecutionTime otherwise.
	var ref time.Time
	if in.IsTest && !in.TestLastRunAt.IsZero() {
		ref = in.TestLastRunAt
	} else {
		ref = EffectiveExecutionTime(in.LastExecuted, in.LastVerified, in.ValidFrom, in.CreatedAt)
	}
	recencySignal := 0.5
	if !ref.IsZero() {
		days := now.Sub(ref).Hours() / 24
		if days < 0 {
			days = 0
		}
		recencySignal = clamp01(math.Exp(-days / 180.0))
	}

	livenessSignal := livenessWeight(in.Liveness)

	// Test decisiveness: |pass-fail|/total. 50/50 → 0 (flaky); 10/0 → 1.
	// Only contributes for test claims; non-tests get 0.5 (neutral).
	testDecisiveness := 0.5
	if in.IsTest {
		total := in.TestPassCount + in.TestFailCount
		if total > 0 {
			diff := in.TestPassCount - in.TestFailCount
			if diff < 0 {
				diff = -diff
			}
			testDecisiveness = float64(diff) / float64(total)
		} else {
			testDecisiveness = 0
		}
	}

	weightedSum := base*wBase +
		authority*wAuthority +
		citationSignal*wCitation +
		recencySignal*wRecency +
		livenessSignal*wLiveness +
		testDecisiveness*wTest

	// AgentAuthority is a multiplicative final factor: an agent with a
	// known poor track record (low AuthorityScore) deflates the score;
	// a zero value means "unknown" — no penalty, treated as neutral 1.0.
	agentFactor := 1.0
	if in.AgentAuthority > 0 {
		agentFactor = clamp01(in.AgentAuthority)
	}
	score = clamp01(clamp01(weightedSum) * agentFactor)

	signals = []domain.ProvenanceSignal{
		{
			Name:         "base_trust",
			Value:        base,
			Weight:       wBase,
			Contribution: base * wBase,
			Detail:       fmt.Sprintf("stored trust score %.2f (0.5 when unset)", in.CurrentTrust),
		},
		{
			Name:         "authority",
			Value:        authority,
			Weight:       wAuthority,
			Contribution: authority * wAuthority,
			Detail:       fmt.Sprintf("source authority %.2f (0.5 when unset)", in.SourceAuthority),
		},
		{
			Name:         "citations",
			Value:        citationSignal,
			Weight:       wCitation,
			Contribution: citationSignal * wCitation,
			Detail:       fmt.Sprintf("%d citation(s)", in.CitationCount),
		},
		{
			Name:         "recency",
			Value:        recencySignal,
			Weight:       wRecency,
			Contribution: recencySignal * wRecency,
			Detail:       recencyDetail(ref, now),
		},
		{
			Name:         "liveness",
			Value:        livenessSignal,
			Weight:       wLiveness,
			Contribution: livenessSignal * wLiveness,
			Detail:       string(in.Liveness),
		},
	}

	if in.IsTest {
		signals = append(signals, domain.ProvenanceSignal{
			Name:         "test_decisiveness",
			Value:        testDecisiveness,
			Weight:       wTest,
			Contribution: testDecisiveness * wTest,
			Detail:       fmt.Sprintf("%d pass / %d fail", in.TestPassCount, in.TestFailCount),
		})
	}

	if agentFactor != 1.0 {
		signals = append(signals, domain.ProvenanceSignal{
			Name:         "agent_authority",
			Value:        agentFactor,
			Weight:       0, // multiplicative, not additive — weight doesn't apply
			Contribution: 0,
			Detail:       fmt.Sprintf("multiplicative factor %.2f from agent authority score", agentFactor),
		})
	}

	// Sort by contribution descending so the most influential signal is first.
	sort.Slice(signals, func(i, j int) bool {
		return signals[i].Contribution > signals[j].Contribution
	})

	rationale = fmt.Sprintf(
		"base=%.2f authority=%.2f citations=%d(%.2f) recency=%.2f liveness=%s agent_authority=%.2f",
		base,
		authority,
		in.CitationCount,
		citationSignal,
		recencySignal,
		in.Liveness,
		agentFactor,
	)
	if in.IsTest {
		rationale += fmt.Sprintf(
			" test_decisiveness=%d/%d(%.2f)",
			in.TestPassCount,
			in.TestPassCount+in.TestFailCount,
			testDecisiveness,
		)
	}

	return score, signals, rationale
}

// ScoreCredibility combines trust + provenance signals into a score and
// human-readable rationale. Thin wrapper over BuildReport for callers
// that don't need the structured per-signal breakdown.
func ScoreCredibility(in CredibilityInputs) (float64, string) {
	score, _, rationale := BuildReport(in)
	return score, rationale
}

func recencyDetail(ref, now time.Time) string {
	if ref.IsZero() {
		return "no reference timestamp available"
	}
	days := now.Sub(ref).Hours() / 24
	if days < 0 {
		days = 0
	}
	return fmt.Sprintf("%.0f days since last evidence", days)
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
