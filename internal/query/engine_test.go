package query

import (
	"testing"
	"time"

	"github.com/felixgeelhaar/mnemos/internal/domain"
)

type fakeEventRepo struct {
	events []domain.Event
}

func (f fakeEventRepo) Append(domain.Event) error                  { return nil }
func (f fakeEventRepo) GetByID(string) (domain.Event, error)       { return domain.Event{}, nil }
func (f fakeEventRepo) ListByIDs([]string) ([]domain.Event, error) { return nil, nil }
func (f fakeEventRepo) ListAll() ([]domain.Event, error)           { return f.events, nil }

type fakeClaimRepo struct {
	claims []domain.Claim
}

func (f fakeClaimRepo) Upsert([]domain.Claim) error { return nil }
func (f fakeClaimRepo) ListByEventIDs([]string) ([]domain.Claim, error) {
	return f.claims, nil
}

type fakeRelationshipRepo struct {
	rels map[string][]domain.Relationship
}

func (f fakeRelationshipRepo) Upsert([]domain.Relationship) error { return nil }
func (f fakeRelationshipRepo) ListByClaim(claimID string) ([]domain.Relationship, error) {
	return f.rels[claimID], nil
}

func TestAnswerIncludesClaimsAndContradictions(t *testing.T) {
	now := time.Date(2026, 4, 12, 17, 0, 0, 0, time.UTC)

	events := fakeEventRepo{events: []domain.Event{
		{ID: "ev_1", Content: "Revenue decreased after launch", Timestamp: now},
		{ID: "ev_2", Content: "Churn increased in Q2", Timestamp: now.Add(time.Minute)},
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
}
