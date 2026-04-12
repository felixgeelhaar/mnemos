package extract

import (
	"strconv"
	"testing"
	"time"

	"github.com/felixgeelhaar/mnemos/internal/domain"
)

func TestEngineExtractCreatesClaimAndEvidencePerEvent(t *testing.T) {
	engine := Engine{
		now: func() time.Time {
			return time.Date(2026, 4, 12, 13, 0, 0, 0, time.UTC)
		},
		nextID: seqClaimIDs(),
	}

	events := []domain.Event{
		{ID: "ev_1", Content: "We decided to pause the rollout."},
		{ID: "ev_2", Content: "Revenue might recover next quarter."},
		{ID: "ev_3", Content: "The churn rate increased to 7%."},
	}

	claims, evidence, err := engine.Extract(events)
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if len(claims) != 3 {
		t.Fatalf("Extract() claims len = %d, want 3", len(claims))
	}
	if len(evidence) != 3 {
		t.Fatalf("Extract() evidence len = %d, want 3", len(evidence))
	}
	if claims[0].Type != domain.ClaimTypeDecision {
		t.Fatalf("claim[0] type = %q, want decision", claims[0].Type)
	}
	if claims[1].Type != domain.ClaimTypeHypothesis {
		t.Fatalf("claim[1] type = %q, want hypothesis", claims[1].Type)
	}
	if claims[2].Type != domain.ClaimTypeFact {
		t.Fatalf("claim[2] type = %q, want fact", claims[2].Type)
	}
	if evidence[0].EventID != "ev_1" || evidence[0].ClaimID != claims[0].ID {
		t.Fatalf("evidence[0] mismatch: %+v claim=%s", evidence[0], claims[0].ID)
	}
}

func seqClaimIDs() func() (string, error) {
	i := 0
	return func() (string, error) {
		id := "cl_test_" + strconv.Itoa(i)
		i++
		return id, nil
	}
}
