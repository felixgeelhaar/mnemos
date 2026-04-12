package sqlite

import (
	"testing"
	"time"

	"github.com/felixgeelhaar/mnemos/internal/domain"
)

func TestClaimRepositoryUpsertAndListByEventIDs(t *testing.T) {
	db := openTestDB(t)
	defer db.Close() //nolint:errcheck

	eventRepo := NewEventRepository(db)
	claimRepo := NewClaimRepository(db)

	now := time.Date(2026, 4, 12, 14, 0, 0, 0, time.UTC)
	event := domain.Event{
		ID:            "ev_for_claim",
		SchemaVersion: "v1",
		Content:       "Team decided to postpone launch",
		SourceInputID: "in_1",
		Timestamp:     now,
		IngestedAt:    now,
		Metadata:      map[string]string{"chunk_kind": "text"},
	}
	if err := eventRepo.Append(event); err != nil {
		t.Fatalf("Append() event error = %v", err)
	}

	claim := domain.Claim{
		ID:         "cl_1",
		Text:       "Team decided to postpone launch",
		Type:       domain.ClaimTypeDecision,
		Confidence: 0.9,
		Status:     domain.ClaimStatusActive,
		CreatedAt:  now,
	}
	if err := claimRepo.Upsert([]domain.Claim{claim}); err != nil {
		t.Fatalf("Upsert() claim error = %v", err)
	}

	if err := claimRepo.UpsertEvidence([]domain.ClaimEvidence{{ClaimID: "cl_1", EventID: "ev_for_claim"}}); err != nil {
		t.Fatalf("UpsertEvidence() error = %v", err)
	}

	claims, err := claimRepo.ListByEventIDs([]string{"ev_for_claim"})
	if err != nil {
		t.Fatalf("ListByEventIDs() error = %v", err)
	}
	if len(claims) != 1 {
		t.Fatalf("ListByEventIDs() len = %d, want 1", len(claims))
	}
	if claims[0].ID != "cl_1" {
		t.Fatalf("ListByEventIDs() claim id = %q, want cl_1", claims[0].ID)
	}
}
