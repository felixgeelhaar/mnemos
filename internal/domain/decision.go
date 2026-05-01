package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// RiskLevel labels how risky an agent considered a Decision at the
// time it was made. The set is intentionally short and ordinal so
// downstream filters ("show me the high-risk decisions whose
// outcomes failed") stay simple.
type RiskLevel string

// Supported RiskLevel values. Ordering low < medium < high < critical
// is used by callers that want a comparable ordinal rather than a
// pure string match.
const (
	RiskLevelLow      RiskLevel = "low"
	RiskLevelMedium   RiskLevel = "medium"
	RiskLevelHigh     RiskLevel = "high"
	RiskLevelCritical RiskLevel = "critical"
)

// Decision records what an agent (or human) believed, what they
// chose, and why. Coupled with Phase 2 outcomes it closes the loop:
// belief -> chosen action -> observed outcome -> validate/refute the
// belief claims.
//
// Beliefs are claim ids that the actor took as load-bearing inputs
// when choosing the Plan. Alternatives is the set of other plans
// that were considered but not chosen — explicit "we knew about Y
// and X" provenance is one of the things that lets the agent improve
// its reasoning later.
//
// OutcomeID is optional at insert time: a decision can be recorded
// before its outcome is known. Once the outcome is observed and
// linked, the auto-edge layer wires Beliefs <-> Outcome through the
// validates/refutes relationship kinds added in Phase 1.
type Decision struct {
	ID           string
	Statement    string
	Plan         string
	Reasoning    string
	RiskLevel    RiskLevel
	Beliefs      []string // claim ids
	Alternatives []string // human-readable alternatives that were considered
	OutcomeID    string   // optional; empty until an outcome is observed and attached
	Scope        Scope    // optional operational context
	ChosenAt     time.Time
	CreatedBy    string
	CreatedAt    time.Time
}

// Validate enforces minimum invariants for persistence. Beliefs and
// Alternatives may be empty (a snap decision with no recorded
// reasoning is still a valid record), but ID, Statement, RiskLevel,
// and ChosenAt are required.
func (d Decision) Validate() error {
	if strings.TrimSpace(d.ID) == "" {
		return errors.New("decision id is required")
	}
	if strings.TrimSpace(d.Statement) == "" {
		return errors.New("decision statement is required")
	}
	if strings.TrimSpace(string(d.RiskLevel)) == "" {
		return errors.New("decision risk_level is required")
	}
	switch d.RiskLevel {
	case RiskLevelLow, RiskLevelMedium, RiskLevelHigh, RiskLevelCritical:
	default:
		return fmt.Errorf("invalid risk_level %q", d.RiskLevel)
	}
	if d.ChosenAt.IsZero() {
		return errors.New("decision chosen_at is required")
	}
	for _, b := range d.Beliefs {
		if strings.TrimSpace(b) == "" {
			return errors.New("decision belief entries must be non-empty")
		}
	}
	return nil
}
