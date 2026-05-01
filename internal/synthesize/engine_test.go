package synthesize

import (
	"context"
	"testing"
	"time"

	"github.com/felixgeelhaar/mnemos/internal/domain"
	"github.com/felixgeelhaar/mnemos/internal/store"

	_ "github.com/felixgeelhaar/mnemos/internal/store/memory"
)

func openTestStore(t *testing.T) *store.Conn {
	t.Helper()
	conn, err := store.Open(context.Background(), "memory://")
	if err != nil {
		t.Fatalf("open memory store: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// seedClusterSuccess inserts a (subject, kind) cluster with N
// successful outcomes. Returns the action ids in insertion order.
func seedClusterSuccess(t *testing.T, conn *store.Conn, subject string, kind domain.ActionKind, n int, base time.Time) []string {
	t.Helper()
	ctx := context.Background()
	ids := make([]string, 0, n)
	for i := 0; i < n; i++ {
		aid := actionIDForTest(subject, string(kind), i)
		oid := outcomeIDForTest(subject, string(kind), i)
		at := base.Add(time.Duration(i) * time.Hour)
		if err := conn.Actions.Append(ctx, domain.Action{
			ID: aid, Kind: kind, Subject: subject, At: at,
		}); err != nil {
			t.Fatalf("seed action: %v", err)
		}
		if err := conn.Outcomes.Append(ctx, domain.Outcome{
			ID: oid, ActionID: aid, Result: domain.OutcomeResultSuccess,
			ObservedAt: at.Add(5 * time.Minute),
		}); err != nil {
			t.Fatalf("seed outcome: %v", err)
		}
		ids = append(ids, aid)
	}
	return ids
}

func actionIDForTest(subject, kind string, idx int) string {
	return "ac_" + subject + "_" + kind + "_" + itoa(idx)
}
func outcomeIDForTest(subject, kind string, idx int) string {
	return "oc_" + subject + "_" + kind + "_" + itoa(idx)
}
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	digits := []byte{}
	for ; i > 0; i /= 10 {
		digits = append([]byte{byte('0' + i%10)}, digits...)
	}
	return string(digits)
}

func TestSynthesize_EmitsLessonOnConsistentSuccess(t *testing.T) {
	conn := openTestStore(t)
	now := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	seedClusterSuccess(t, conn, "payments", domain.ActionKindRollback, 3, now.Add(-12*time.Hour))

	res, err := Synthesize(context.Background(), conn.Actions, conn.Outcomes, conn.Lessons, Options{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("synthesize: %v", err)
	}
	if res.LessonsEmitted != 1 {
		t.Fatalf("want 1 lesson, got %d (clusters=%d skipped=%d)", res.LessonsEmitted, res.Clusters, res.Skipped)
	}
	got, err := conn.Lessons.ListAll(context.Background())
	if err != nil {
		t.Fatalf("list lessons: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 stored lesson, got %d", len(got))
	}
	l := got[0]
	if l.Scope.Service != "payments" || l.Kind != string(domain.ActionKindRollback) {
		t.Fatalf("scope/kind mismatch: %+v", l)
	}
	if l.Confidence < domain.LessonConfidenceMin {
		t.Fatalf("confidence below floor: %v", l.Confidence)
	}
	if len(l.Evidence) != 3 {
		t.Fatalf("want 3 evidence ids, got %d", len(l.Evidence))
	}
}

func TestSynthesize_SkipsBelowMinCorroboration(t *testing.T) {
	conn := openTestStore(t)
	now := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	seedClusterSuccess(t, conn, "search", domain.ActionKindDeploy, 2, now.Add(-1*time.Hour))

	res, err := Synthesize(context.Background(), conn.Actions, conn.Outcomes, conn.Lessons, Options{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("synthesize: %v", err)
	}
	if res.LessonsEmitted != 0 {
		t.Fatalf("want 0 lessons (below MinCorroboration), got %d", res.LessonsEmitted)
	}
	if res.Skipped != 1 {
		t.Fatalf("want 1 skipped, got %d", res.Skipped)
	}
}

func TestSynthesize_DropsContradictoryCluster(t *testing.T) {
	conn := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	base := now.Add(-6 * time.Hour)

	// 3 actions, 2 success, 2 failure across them = 50/50 split = below 2/3 consistency.
	for i := 0; i < 3; i++ {
		aid := actionIDForTest("orders", "deploy", i)
		at := base.Add(time.Duration(i) * time.Hour)
		if err := conn.Actions.Append(ctx, domain.Action{
			ID: aid, Kind: domain.ActionKindDeploy, Subject: "orders", At: at,
		}); err != nil {
			t.Fatalf("seed action: %v", err)
		}
		// Pair each action with one success + one failure to force 50/50.
		if err := conn.Outcomes.Append(ctx, domain.Outcome{
			ID: "oc_s_" + itoa(i), ActionID: aid, Result: domain.OutcomeResultSuccess, ObservedAt: at.Add(5 * time.Minute),
		}); err != nil {
			t.Fatalf("seed outcome s: %v", err)
		}
		if err := conn.Outcomes.Append(ctx, domain.Outcome{
			ID: "oc_f_" + itoa(i), ActionID: aid, Result: domain.OutcomeResultFailure, ObservedAt: at.Add(10 * time.Minute),
		}); err != nil {
			t.Fatalf("seed outcome f: %v", err)
		}
	}

	res, err := Synthesize(ctx, conn.Actions, conn.Outcomes, conn.Lessons, Options{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("synthesize: %v", err)
	}
	if res.LessonsEmitted != 0 {
		t.Fatalf("want 0 lessons (contradictory), got %d", res.LessonsEmitted)
	}
}

func TestSynthesize_IsIdempotent(t *testing.T) {
	conn := openTestStore(t)
	now := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	seedClusterSuccess(t, conn, "payments", domain.ActionKindRollback, 4, now.Add(-2*time.Hour))

	for i := 0; i < 3; i++ {
		if _, err := Synthesize(context.Background(), conn.Actions, conn.Outcomes, conn.Lessons, Options{Now: func() time.Time { return now }}); err != nil {
			t.Fatalf("synthesize iter %d: %v", i, err)
		}
	}
	got, err := conn.Lessons.ListAll(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 lesson after 3 runs, got %d", len(got))
	}
	count, err := conn.Lessons.CountAll(context.Background())
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("count: want 1, got %d", count)
	}
}

func TestSynthesize_ConfidenceDecaysWithAge(t *testing.T) {
	conn := openTestStore(t)
	now := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	// 3 actions a year ago — recency factor should drop confidence
	// below threshold even with perfect corroboration + consistency.
	seedClusterSuccess(t, conn, "legacy", domain.ActionKindRestart, 3, now.AddDate(-1, 0, 0))

	res, err := Synthesize(context.Background(), conn.Actions, conn.Outcomes, conn.Lessons, Options{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("synthesize: %v", err)
	}
	if res.LessonsEmitted != 0 {
		t.Fatalf("expected aged-out cluster to be skipped, got %d emitted", res.LessonsEmitted)
	}
}
