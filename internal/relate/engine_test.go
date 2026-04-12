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

func seqRelationshipIDs() func() (string, error) {
	i := 0
	return func() (string, error) {
		id := "rl_test_" + strconv.Itoa(i)
		i++
		return id, nil
	}
}
