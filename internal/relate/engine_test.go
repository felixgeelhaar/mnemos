package relate

import (
	"strconv"
	"testing"
	"time"

	"github.com/felixgeelhaar/mnemos/internal/domain"
)

func TestDetectFindsSupportAndContradiction(t *testing.T) {
	engine := Engine{
		now: func() time.Time {
			return time.Date(2026, 4, 12, 15, 0, 0, 0, time.UTC)
		},
		nextID: seqRelationshipIDs(),
	}

	claims := []domain.Claim{
		{ID: "cl_1", Text: "Revenue increased in Q2"},
		{ID: "cl_2", Text: "Revenue increased in Q2 after release"},
		{ID: "cl_3", Text: "Revenue did not increase in Q2"},
	}

	rels, err := engine.Detect(claims)
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if len(rels) == 0 {
		t.Fatal("Detect() expected relationships, got none")
	}

	hasSupport := false
	hasContradiction := false
	for _, rel := range rels {
		if rel.Type == domain.RelationshipTypeSupports {
			hasSupport = true
		}
		if rel.Type == domain.RelationshipTypeContradicts {
			hasContradiction = true
		}
	}

	if !hasSupport {
		t.Fatal("Detect() expected at least one supports relationship")
	}
	if !hasContradiction {
		t.Fatal("Detect() expected at least one contradicts relationship")
	}
}

func TestDetectIgnoresUnrelatedClaims(t *testing.T) {
	engine := Engine{
		now: func() time.Time {
			return time.Date(2026, 4, 12, 15, 0, 0, 0, time.UTC)
		},
		nextID: seqRelationshipIDs(),
	}

	claims := []domain.Claim{
		{ID: "cl_1", Text: "Revenue increased in Q2"},
		{ID: "cl_2", Text: "The team increased headcount"},
	}

	rels, err := engine.Detect(claims)
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}

	// "Revenue increased in Q2" and "The team increased headcount" share
	// "increased" after stop-word filtering. But overlap ratio is below
	// threshold since each has multiple unique content tokens.
	// With content tokens: {revenue, increas, q2} vs {team, increas, headcount}
	// Overlap = 1 (increas), which is below minContentTokenOverlap of 2.
	if len(rels) != 0 {
		t.Fatalf("Detect() expected 0 relationships for unrelated claims, got %d", len(rels))
	}
}

func TestDetectContractionNegation(t *testing.T) {
	engine := Engine{
		now: func() time.Time {
			return time.Date(2026, 4, 12, 15, 0, 0, 0, time.UTC)
		},
		nextID: seqRelationshipIDs(),
	}

	claims := []domain.Claim{
		{ID: "cl_1", Text: "The service deployment succeeded in production"},
		{ID: "cl_2", Text: "The service deployment didn't succeed in production"},
	}

	rels, err := engine.Detect(claims)
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}

	hasContradiction := false
	for _, rel := range rels {
		if rel.Type == domain.RelationshipTypeContradicts {
			hasContradiction = true
		}
	}
	if !hasContradiction {
		t.Fatal("Detect() expected contradiction for contraction negation (didn't)")
	}
}

func TestDetectIncrementalFindsRelationships(t *testing.T) {
	engine := Engine{
		now: func() time.Time {
			return time.Date(2026, 4, 12, 15, 0, 0, 0, time.UTC)
		},
		nextID: seqRelationshipIDs(),
	}

	existing := []domain.Claim{
		{ID: "cl_old_1", Text: "Revenue increased in Q2"},
	}

	newClaims := []domain.Claim{
		{ID: "cl_new_1", Text: "Revenue increased in Q2 after the product launch"},
		{ID: "cl_new_2", Text: "Revenue did not increase in Q2"},
	}

	rels, err := engine.DetectIncremental(newClaims, existing)
	if err != nil {
		t.Fatalf("DetectIncremental() error = %v", err)
	}
	if len(rels) == 0 {
		t.Fatal("DetectIncremental() expected relationships, got none")
	}

	hasSupport := false
	hasContradiction := false
	for _, rel := range rels {
		// All relationships should be from new claims to existing claims.
		if rel.FromClaimID != "cl_new_1" && rel.FromClaimID != "cl_new_2" {
			t.Fatalf("DetectIncremental() unexpected FromClaimID %q, expected a new claim", rel.FromClaimID)
		}
		if rel.ToClaimID != "cl_old_1" {
			t.Fatalf("DetectIncremental() unexpected ToClaimID %q, expected cl_old_1", rel.ToClaimID)
		}
		if rel.Type == domain.RelationshipTypeSupports {
			hasSupport = true
		}
		if rel.Type == domain.RelationshipTypeContradicts {
			hasContradiction = true
		}
	}

	if !hasSupport {
		t.Fatal("DetectIncremental() expected at least one supports relationship")
	}
	if !hasContradiction {
		t.Fatal("DetectIncremental() expected at least one contradicts relationship")
	}
}

func TestDetectIncrementalDoesNotCompareExistingPairs(t *testing.T) {
	engine := Engine{
		now: func() time.Time {
			return time.Date(2026, 4, 12, 15, 0, 0, 0, time.UTC)
		},
		nextID: seqRelationshipIDs(),
	}

	existing := []domain.Claim{
		{ID: "cl_old_1", Text: "Revenue increased in Q2"},
		{ID: "cl_old_2", Text: "Revenue increased in Q2 significantly"},
	}

	// New claim is unrelated to existing claims.
	newClaims := []domain.Claim{
		{ID: "cl_new_1", Text: "The weather was sunny today"},
	}

	rels, err := engine.DetectIncremental(newClaims, existing)
	if err != nil {
		t.Fatalf("DetectIncremental() error = %v", err)
	}
	// The two existing claims are related to each other, but DetectIncremental
	// should NOT detect that — only new vs existing comparisons.
	if len(rels) != 0 {
		t.Fatalf("DetectIncremental() expected 0 relationships (only new vs existing), got %d", len(rels))
	}
}

func TestDetectIncrementalEmptyInputs(t *testing.T) {
	engine := Engine{
		now: func() time.Time {
			return time.Date(2026, 4, 12, 15, 0, 0, 0, time.UTC)
		},
		nextID: seqRelationshipIDs(),
	}

	// Empty new claims.
	rels, err := engine.DetectIncremental(nil, []domain.Claim{{ID: "cl_1", Text: "test"}})
	if err != nil {
		t.Fatalf("DetectIncremental() error = %v", err)
	}
	if rels != nil {
		t.Fatalf("DetectIncremental() with empty newClaims expected nil, got %v", rels)
	}

	// Empty existing claims.
	rels, err = engine.DetectIncremental([]domain.Claim{{ID: "cl_1", Text: "test"}}, nil)
	if err != nil {
		t.Fatalf("DetectIncremental() error = %v", err)
	}
	if rels != nil {
		t.Fatalf("DetectIncremental() with empty existingClaims expected nil, got %v", rels)
	}
}

func seqRelationshipIDs() func() (string, error) {
	i := 0
	return func() (string, error) {
		id := "rl_test_" + strconv.Itoa(i)
		i++
		return id, nil
	}
}
