package query

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/felixgeelhaar/mnemos/internal/domain"
)

type fakeEventRepo struct {
	events []domain.Event
}

func (f fakeEventRepo) Append(_ context.Context, _ domain.Event) error { return nil }
func (f fakeEventRepo) GetByID(_ context.Context, _ string) (domain.Event, error) {
	return domain.Event{}, nil
}
func (f fakeEventRepo) ListByIDs(_ context.Context, _ []string) ([]domain.Event, error) {
	return nil, nil
}
func (f fakeEventRepo) ListAll(_ context.Context) ([]domain.Event, error) { return f.events, nil }
func (f fakeEventRepo) ListByRunID(_ context.Context, runID string) ([]domain.Event, error) {
	filtered := make([]domain.Event, 0)
	for _, event := range f.events {
		if event.RunID == runID {
			filtered = append(filtered, event)
		}
	}
	return filtered, nil
}

type fakeClaimRepo struct {
	claims   []domain.Claim
	evidence []domain.ClaimEvidence
}

func (f fakeClaimRepo) Upsert(_ context.Context, _ []domain.Claim) error { return nil }
func (f fakeClaimRepo) ListByEventIDs(_ context.Context, _ []string) ([]domain.Claim, error) {
	return f.claims, nil
}
func (f fakeClaimRepo) ListEvidenceByClaimIDs(_ context.Context, claimIDs []string) ([]domain.ClaimEvidence, error) {
	wanted := map[string]struct{}{}
	for _, id := range claimIDs {
		wanted[id] = struct{}{}
	}
	out := make([]domain.ClaimEvidence, 0, len(f.evidence))
	for _, e := range f.evidence {
		if _, ok := wanted[e.ClaimID]; ok {
			out = append(out, e)
		}
	}
	return out, nil
}

type fakeRelationshipRepo struct {
	rels map[string][]domain.Relationship
}

func (f fakeRelationshipRepo) Upsert(_ context.Context, _ []domain.Relationship) error { return nil }
func (f fakeRelationshipRepo) ListByClaim(_ context.Context, claimID string) ([]domain.Relationship, error) {
	return f.rels[claimID], nil
}

func TestAnswerIncludesClaimsAndContradictions(t *testing.T) {
	now := time.Date(2026, 4, 12, 17, 0, 0, 0, time.UTC)

	events := fakeEventRepo{events: []domain.Event{
		{ID: "ev_1", RunID: "run_1", Content: "Revenue decreased after launch", Timestamp: now},
		{ID: "ev_2", RunID: "run_2", Content: "Churn increased in Q2", Timestamp: now.Add(time.Minute)},
	}}

	claims := []domain.Claim{
		{ID: "cl_1", Text: "Revenue decreased after launch", Type: domain.ClaimTypeFact, Status: domain.ClaimStatusActive, Confidence: 0.8, CreatedAt: now},
		{ID: "cl_2", Text: "Revenue did not decrease after launch", Type: domain.ClaimTypeFact, Status: domain.ClaimStatusActive, Confidence: 0.8, CreatedAt: now},
	}

	rels := fakeRelationshipRepo{rels: map[string][]domain.Relationship{
		"cl_1": {{ID: "rl_1", Type: domain.RelationshipTypeContradicts, FromClaimID: "cl_1", ToClaimID: "cl_2", CreatedAt: now}},
		"cl_2": {{ID: "rl_1", Type: domain.RelationshipTypeContradicts, FromClaimID: "cl_1", ToClaimID: "cl_2", CreatedAt: now}},
	}}

	engine := NewEngine(events, fakeClaimRepo{claims: claims}, rels)
	answer, err := engine.Answer("what happened to revenue after launch")
	if err != nil {
		t.Fatalf("Answer() error = %v", err)
	}

	if len(answer.Claims) != 2 {
		t.Fatalf("Claims len = %d, want 2", len(answer.Claims))
	}
	if len(answer.Contradictions) != 1 {
		t.Fatalf("Contradictions len = %d, want 1", len(answer.Contradictions))
	}
	if len(answer.TimelineEventIDs) == 0 {
		t.Fatal("TimelineEventIDs should not be empty")
	}
	if !strings.Contains(answer.AnswerText, "strongest signal") {
		t.Fatalf("AnswerText = %q, expected strongest signal narrative", answer.AnswerText)
	}
	if !strings.Contains(answer.AnswerText, "contested") {
		t.Fatalf("AnswerText = %q, expected contradiction context", answer.AnswerText)
	}
}

func TestAnswerForRunScopesEvents(t *testing.T) {
	now := time.Date(2026, 4, 12, 18, 0, 0, 0, time.UTC)
	events := fakeEventRepo{events: []domain.Event{
		{ID: "ev_run_a", RunID: "run_a", Content: "Revenue decreased after launch", Timestamp: now},
		{ID: "ev_run_b", RunID: "run_b", Content: "Churn increased in Q2", Timestamp: now.Add(time.Minute)},
	}}
	claims := []domain.Claim{
		{ID: "cl_1", Text: "Revenue decreased after launch", Type: domain.ClaimTypeFact, Status: domain.ClaimStatusActive, Confidence: 0.8, CreatedAt: now},
	}
	engine := NewEngine(events, fakeClaimRepo{claims: claims}, fakeRelationshipRepo{rels: map[string][]domain.Relationship{}})

	answer, err := engine.AnswerForRun("what happened to revenue", "run_a")
	if err != nil {
		t.Fatalf("AnswerForRun() error = %v", err)
	}
	if len(answer.TimelineEventIDs) != 1 {
		t.Fatalf("TimelineEventIDs len = %d, want 1", len(answer.TimelineEventIDs))
	}
	if answer.TimelineEventIDs[0] != "ev_run_a" {
		t.Fatalf("TimelineEventIDs[0] = %q, want ev_run_a", answer.TimelineEventIDs[0])
	}
}

func TestAnswer_AttributesProvenanceFromPulledEvent(t *testing.T) {
	now := time.Date(2026, 4, 18, 9, 0, 0, 0, time.UTC)

	events := fakeEventRepo{events: []domain.Event{
		{ID: "ev_local", RunID: "r", Content: "Local fact about cache eviction policy", Timestamp: now,
			Metadata: map[string]string{}},
		{ID: "ev_remote", RunID: "r", Content: "Remote claim about cache eviction policy", Timestamp: now.Add(time.Minute),
			Metadata: map[string]string{"pulled_from_registry": "https://reg.example.com"}},
	}}
	claims := []domain.Claim{
		{ID: "cl_local", Text: "We use LRU for cache eviction policy", Type: domain.ClaimTypeFact, Status: domain.ClaimStatusActive, Confidence: 0.8, CreatedAt: now},
		{ID: "cl_remote", Text: "Cache eviction policy is FIFO", Type: domain.ClaimTypeFact, Status: domain.ClaimStatusActive, Confidence: 0.8, CreatedAt: now.Add(time.Minute)},
	}
	repo := fakeClaimRepo{
		claims: claims,
		evidence: []domain.ClaimEvidence{
			{ClaimID: "cl_local", EventID: "ev_local"},
			{ClaimID: "cl_remote", EventID: "ev_remote"},
		},
	}

	engine := NewEngine(events, repo, fakeRelationshipRepo{rels: map[string][]domain.Relationship{}})
	answer, err := engine.Answer("cache eviction policy")
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}

	if got := answer.ClaimProvenance["cl_local"]; got != "local" {
		t.Errorf("cl_local provenance = %q, want 'local'", got)
	}
	if got := answer.ClaimProvenance["cl_remote"]; got != "https://reg.example.com" {
		t.Errorf("cl_remote provenance = %q, want registry URL", got)
	}
	if !strings.Contains(answer.AnswerText, "from https://reg.example.com") &&
		!strings.Contains(answer.AnswerText, "from a connected registry") {
		t.Errorf("AnswerText does not surface provenance: %q", answer.AnswerText)
	}
}
