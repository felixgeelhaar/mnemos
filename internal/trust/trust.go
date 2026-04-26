// Package trust derives a single, comparable trust_score for each
// claim from three independent signals: the LLM-assigned confidence,
// the number of distinct events that corroborate the claim, and the
// freshness of the most recent corroborating evidence.
//
// Why this exists. A raw LLM "confidence" field treats a verbatim
// citation and a hallucination identically — both come back as ~0.9.
// Ranking by it produces nonsense. The trust_score below adds two
// signals the LLM cannot fake: how many independent events back the
// claim, and how recently any of them was observed. Together they
// approximate the question a human reader would actually ask: "do I
// have any reason to believe this is still true?"
package trust

import (
	"math"
	"time"
)

// FreshnessHalfLifeDays controls how quickly a claim's trust decays
// in the absence of new corroborating evidence. At one half-life the
// freshness factor is e^-1 ≈ 0.37; at two half-lives ≈ 0.13. A floor
// (FreshnessFloor) prevents very old facts from collapsing to zero —
// "Paris is the capital of France" should not score 0 just because the
// last corroborating event was a year ago.
const FreshnessHalfLifeDays = 90.0

// FreshnessFloor caps how low the freshness factor can drop. Without
// this, the multiplicative formula would let any old-but-true claim
// fall arbitrarily close to zero.
const FreshnessFloor = 0.3

// CorroborationCoefficient scales ln(evidenceCount) into the
// corroboration multiplier. With coefficient 0.2:
//   - 1 source  → 1.00 (neutral, no boost; ln(1) = 0)
//   - 5 sources → 1.32
//   - 20 sources→ 1.60
//
// Logarithmic so adding the 100th source doesn't dwarf the 10th.
const CorroborationCoefficient = 0.2

// Score returns a trust_score in [0, 1] from the three signals.
// `now` is injected so callers can test against a fixed clock; in
// production code pass time.Now().UTC().
//
// Inputs:
//
//	confidence     — LLM-assigned, expected in [0, 1]; clamped if outside
//	evidenceCount  — number of distinct events linked to this claim;
//	                 a claim with zero evidence is treated as having one
//	                 (its source event), since claims cannot exist without
//	                 at least one anchoring event in the domain model
//	latestEvidence — timestamp of the freshest evidence event; the zero
//	                 value disables the freshness factor (set to 1.0)
func Score(confidence float64, evidenceCount int, latestEvidence, now time.Time) float64 {
	c := clamp01(confidence)
	if evidenceCount < 1 {
		evidenceCount = 1
	}

	corroboration := 1 + math.Log(float64(evidenceCount))*CorroborationCoefficient
	freshness := freshnessFactor(latestEvidence, now)

	return clamp01(c * corroboration * freshness)
}

// freshnessFactor returns a multiplier in [FreshnessFloor, 1].
// Exponential decay with half-life FreshnessHalfLifeDays. A zero
// timestamp short-circuits to 1.0 so callers that lack a timestamp
// (e.g., backfill before evidence is loaded) don't get penalised.
func freshnessFactor(latest, now time.Time) float64 {
	if latest.IsZero() {
		return 1.0
	}
	days := now.Sub(latest).Hours() / 24
	if days <= 0 {
		return 1.0
	}
	f := math.Exp(-days / FreshnessHalfLifeDays)
	if f < FreshnessFloor {
		return FreshnessFloor
	}
	return f
}

func clamp01(x float64) float64 {
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}
