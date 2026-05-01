package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// PlaybookStep is one ordered instruction in a Playbook. Steps are
// the contract Praxis (the execution layer) consumes; Mnemos returns
// them and steps aside for execution. Condition is optional and free-
// form — Praxis interprets it; Mnemos preserves the string.
type PlaybookStep struct {
	Order       int    // 1-indexed position in the playbook
	Action      string // short imperative verb (e.g. "rollback", "scale_up", "page_oncall")
	Description string // human-readable expansion
	Condition   string // optional precondition or guard, free-form
}

// Validate enforces minimum invariants on a step.
func (s PlaybookStep) Validate() error {
	if s.Order < 1 {
		return fmt.Errorf("playbook step order must be >= 1, got %d", s.Order)
	}
	if strings.TrimSpace(s.Action) == "" {
		return errors.New("playbook step action is required")
	}
	return nil
}

// Playbook is structured operational intelligence: a trigger pattern
// (the situation the playbook responds to) plus an ordered list of
// steps. Mnemos returns playbooks; Praxis executes them. The
// separation is load-bearing — Mnemos cannot mutate the world, so
// playbooks must be steps-only.
//
// Playbooks come from two paths: synthesis (derived from clustered
// Lessons) and human authoring. Source distinguishes the two so the
// trust formula can weight them differently. DerivedFromLessons
// preserves provenance back to the lessons that justified the
// synthesised playbook.
type Playbook struct {
	ID                 string
	Trigger            string // canonical trigger label, e.g. "latency_spike_after_deploy"
	Statement          string // short summary of what this playbook does
	Scope              LessonScope
	Steps              []PlaybookStep
	DerivedFromLessons []string // lesson ids
	Confidence         float64  // 0..1
	DerivedAt          time.Time
	LastVerified       time.Time
	Source             string // "synthesize" | "human"
	CreatedBy          string
}

// PlaybookConfidenceMin is the floor below which the synthesis layer
// drops a candidate playbook. Tuned so a single high-confidence
// lesson can seed a playbook (a one-lesson cluster scores ~0.55 with
// LessonConfidenceMin=0.55), while two thin lessons cannot.
const PlaybookConfidenceMin = 0.55

// PlaybookMinLessons is the smallest number of corroborating lessons
// the synthesis layer requires before emitting a synthesised
// playbook. One is enough — a single high-confidence lesson with
// recurring evidence is a credible playbook seed; the lesson layer
// already filters for corroboration upstream.
const PlaybookMinLessons = 1

// Validate enforces minimum invariants for persistence. Steps may be
// empty for placeholder/in-progress playbooks; ID, Trigger,
// Statement, Confidence, and DerivedAt are required.
func (p Playbook) Validate() error {
	if strings.TrimSpace(p.ID) == "" {
		return errors.New("playbook id is required")
	}
	if strings.TrimSpace(p.Trigger) == "" {
		return errors.New("playbook trigger is required")
	}
	if strings.TrimSpace(p.Statement) == "" {
		return errors.New("playbook statement is required")
	}
	if p.Confidence < 0 || p.Confidence > 1 {
		return fmt.Errorf("playbook confidence must be in [0, 1], got %v", p.Confidence)
	}
	if p.DerivedAt.IsZero() {
		return errors.New("playbook derived_at is required")
	}
	for i, s := range p.Steps {
		if err := s.Validate(); err != nil {
			return fmt.Errorf("playbook step %d: %w", i, err)
		}
	}
	return nil
}
