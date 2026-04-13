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

func seqRelationshipIDs() func() (string, error) {
	i := 0
	return func() (string, error) {
		id := "rl_test_" + strconv.Itoa(i)
		i++
		return id, nil
	}
}
