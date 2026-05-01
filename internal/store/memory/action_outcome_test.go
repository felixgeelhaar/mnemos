package memory

import (
	"context"
	"testing"
	"time"

	"github.com/felixgeelhaar/mnemos/internal/domain"
	"github.com/felixgeelhaar/mnemos/internal/store"
)

func openTestConn(t *testing.T) *store.Conn {
	t.Helper()
	conn, err := store.Open(context.Background(), "memory://")
	if err != nil {
		t.Fatalf("open memory store: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func TestActionRepository_AppendListIdempotent(t *testing.T) {
	conn := openTestConn(t)
	ctx := context.Background()
	at := time.Date(2026, 4, 30, 9, 0, 0, 0, time.UTC)
	a := domain.Action{
		ID:      "ac_1",
		RunID:   "run_a",
		Kind:    domain.ActionKindDeploy,
		Subject: "payments",
		At:      at,
	}
	if err := conn.Actions.Append(ctx, a); err != nil {
		t.Fatalf("append action: %v", err)
	}
	// Idempotent re-append.
	if err := conn.Actions.Append(ctx, a); err != nil {
		t.Fatalf("re-append action: %v", err)
	}
	count, err := conn.Actions.CountAll(ctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("want count=1, got %d", count)
	}
	gotByRun, err := conn.Actions.ListByRunID(ctx, "run_a")
	if err != nil {
		t.Fatalf("list by run: %v", err)
	}
	if len(gotByRun) != 1 || gotByRun[0].ID != "ac_1" {
		t.Fatalf("list by run: want ac_1, got %#v", gotByRun)
	}
	gotBySubject, err := conn.Actions.ListBySubject(ctx, "payments")
	if err != nil {
		t.Fatalf("list by subject: %v", err)
	}
	if len(gotBySubject) != 1 {
		t.Fatalf("list by subject: want 1, got %d", len(gotBySubject))
	}
}

func TestOutcomeRepository_AppendListByAction(t *testing.T) {
	conn := openTestConn(t)
	ctx := context.Background()
	at := time.Date(2026, 4, 30, 9, 0, 0, 0, time.UTC)
	if err := conn.Actions.Append(ctx, domain.Action{
		ID: "ac_1", Kind: domain.ActionKindRollback, Subject: "payments", At: at,
	}); err != nil {
		t.Fatalf("seed action: %v", err)
	}
	out := domain.Outcome{
		ID:         "oc_1",
		ActionID:   "ac_1",
		Result:     domain.OutcomeResultSuccess,
		Metrics:    map[string]float64{"latency_after_ms": 240, "latency_before_ms": 1200},
		ObservedAt: at.Add(2 * time.Minute),
	}
	if err := conn.Outcomes.Append(ctx, out); err != nil {
		t.Fatalf("append outcome: %v", err)
	}
	got, err := conn.Outcomes.ListByActionID(ctx, "ac_1")
	if err != nil {
		t.Fatalf("list by action: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 outcome, got %d", len(got))
	}
	if got[0].Result != domain.OutcomeResultSuccess {
		t.Fatalf("result: want success, got %s", got[0].Result)
	}
	if got[0].Metrics["latency_after_ms"] != 240 {
		t.Fatalf("metric latency_after_ms: want 240, got %v", got[0].Metrics["latency_after_ms"])
	}
	if got[0].Source != "push" {
		t.Fatalf("source default: want push, got %q", got[0].Source)
	}
}

func TestClaimRepository_MarkVerified(t *testing.T) {
	conn := openTestConn(t)
	ctx := context.Background()
	createdAt := time.Date(2026, 4, 15, 9, 0, 0, 0, time.UTC)
	if err := conn.Claims.Upsert(ctx, []domain.Claim{{
		ID:         "cl_1",
		Text:       "deploy succeeded",
		Type:       domain.ClaimTypeFact,
		Confidence: 0.8,
		Status:     domain.ClaimStatusActive,
		CreatedAt:  createdAt,
		ValidFrom:  createdAt,
	}}); err != nil {
		t.Fatalf("seed claim: %v", err)
	}
	verifiedAt := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	if err := conn.Claims.MarkVerified(ctx, "cl_1", verifiedAt, 30); err != nil {
		t.Fatalf("mark verified: %v", err)
	}
	got, err := conn.Claims.ListByIDs(ctx, []string{"cl_1"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 claim, got %d", len(got))
	}
	if !got[0].LastVerified.Equal(verifiedAt.UTC()) {
		t.Fatalf("LastVerified: want %v, got %v", verifiedAt.UTC(), got[0].LastVerified)
	}
	if got[0].VerifyCount != 1 {
		t.Fatalf("VerifyCount: want 1, got %d", got[0].VerifyCount)
	}
	if got[0].HalfLifeDays != 30 {
		t.Fatalf("HalfLifeDays: want 30, got %v", got[0].HalfLifeDays)
	}

	// Idempotent re-verification preserves half_life override when caller passes 0.
	verifiedAt2 := verifiedAt.Add(time.Hour)
	if err := conn.Claims.MarkVerified(ctx, "cl_1", verifiedAt2, 0); err != nil {
		t.Fatalf("re-mark: %v", err)
	}
	got, _ = conn.Claims.ListByIDs(ctx, []string{"cl_1"})
	if got[0].VerifyCount != 2 {
		t.Fatalf("VerifyCount after re-mark: want 2, got %d", got[0].VerifyCount)
	}
	if got[0].HalfLifeDays != 30 {
		t.Fatalf("HalfLifeDays should not be reset by zero arg, got %v", got[0].HalfLifeDays)
	}
}
