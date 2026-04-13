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
	claims []domain.Claim
}

func (f fakeClaimRepo) Upsert(_ context.Context, _ []domain.Claim) error { return nil }
func (f fakeClaimRepo) ListByEventIDs(_ context.Context, _ []string) ([]domain.Claim, error) {
	return f.claims, nil
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
