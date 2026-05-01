package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// LessonScope narrows a Lesson to a specific operational context. The
// synthesis layer clusters strictly within a scope so a "rollback
// works for payments" lesson cannot quietly contaminate "rollback
// works for search". Empty scope means "applies everywhere" — used
// for the few cross-cutting truths that survive every cluster.
type LessonScope struct {
	Service string
	Env     string
	Team    string
}

// IsEmpty reports whether all scope fields are unset.
func (s LessonScope) IsEmpty() bool {
	return s.Service == "" && s.Env == "" && s.Team == ""
}

// Equal compares two scopes by value. Used by the cluster grouping
// pass so two actions with the same (service, env, team) sort into
// the same bucket regardless of map ordering.
func (s LessonScope) Equal(o LessonScope) bool {
	return s.Service == o.Service && s.Env == o.Env && s.Team == o.Team
}

// Key returns a stable string form for map indexing during synthesis.
func (s LessonScope) Key() string {
	return s.Service + "|" + s.Env + "|" + s.Team
}

// Lesson is a validated operational truth derived from one or more
// Action -> Outcome chains. Lessons are the synthesis layer's output:
// they answer "what have we learned?" rather than "what do we
// believe?" (claims) or "what happened?" (events / actions).
//
// Evidence is the list of Action ids that corroborated the lesson at
// derivation time. Confidence is in [0,1] and reflects corroboration
// count, outcome consistency, and recency — see internal/synthesize.
//
// LastVerified ticks forward when a fresh action+outcome pair re-
// confirms the lesson, supporting the temporal-validity hardening of
// Phase 4 (decay-aware trust).
type Lesson struct {
	ID           string
	Statement    string
	Scope        LessonScope
	Trigger      string   // optional — short label like "latency_spike_after_deploy" used for clustering and playbook lookup
	Kind         string   // optional free-form classifier (e.g. "rollback", "scale-up"), preserved verbatim
	Evidence     []string // Action ids that corroborated this lesson
	Confidence   float64
	DerivedAt    time.Time
	LastVerified time.Time
	Source       string // "synthesize" for engine-derived, "human" for hand-authored
	CreatedBy    string
}

// LessonConfidenceMin is the floor below which the synthesis layer
// drops a candidate lesson rather than emitting it. Tuned so a 3/3
// success cluster lands above the floor and a 2/3 success / 1/3
// contradiction cluster lands below.
const LessonConfidenceMin = 0.55

// LessonMinCorroboration is the smallest number of distinct
// corroborating actions required before the synthesis layer will
// emit a lesson. Lower than 3 produces noisy folklore; higher slows
// the system's ability to learn from a thin corpus.
const LessonMinCorroboration = 3

// Validate enforces minimum invariants for persistence. Empty scope
// is permitted (cross-cutting lessons) but evidence must have at
// least one entry — a lesson without provenance is folklore.
func (l Lesson) Validate() error {
	if strings.TrimSpace(l.ID) == "" {
		return errors.New("lesson id is required")
	}
	if strings.TrimSpace(l.Statement) == "" {
		return errors.New("lesson statement is required")
	}
	if l.Confidence < 0 || l.Confidence > 1 {
		return fmt.Errorf("lesson confidence must be in [0, 1], got %v", l.Confidence)
	}
	if len(l.Evidence) == 0 {
		return errors.New("lesson evidence must contain at least one action id")
	}
	for _, e := range l.Evidence {
		if strings.TrimSpace(e) == "" {
			return errors.New("lesson evidence entries must be non-empty")
		}
	}
	if l.DerivedAt.IsZero() {
		return errors.New("lesson derived_at is required")
	}
	return nil
}
