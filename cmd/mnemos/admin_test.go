package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/felixgeelhaar/mnemos/internal/store/sqlite"
	"github.com/felixgeelhaar/mnemos/internal/store/sqlite/sqlcgen"
)

func TestResetDB_WipesEverything(t *testing.T) {
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "reset.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	now := time.Now().UTC()
	seedEvent(t, db, "ev1", "r", "content", "in1", `{}`, now)
	seedClaim(t, db, "cl1", "claim 1", "fact", "active", 0.8, now)
	seedClaim(t, db, "cl2", "claim 2", "fact", "active", 0.7, now)
	seedRelationship(t, db, "rl1", "supports", "cl1", "cl2", now)

	counts, err := resetDB(context.Background(), db, false)
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
	if counts.Claims != 2 || counts.Relationships != 1 || counts.Events != 1 {
		t.Fatalf("counts not captured pre-delete: %+v", counts)
	}

	q := sqlcgen.New(db)
	gotClaims, _ := q.ListAllClaims(context.Background())
	if len(gotClaims) != 0 {
		t.Fatalf("claims not deleted: %d remaining", len(gotClaims))
	}
	gotEvents, _ := q.ListAllEvents(context.Background())
	if len(gotEvents) != 0 {
		t.Fatalf("events not deleted: %d remaining", len(gotEvents))
	}
}

func TestResetDB_KeepEvents(t *testing.T) {
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "reset.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	now := time.Now().UTC()
	seedEvent(t, db, "ev1", "r", "content", "in1", `{}`, now)
	seedClaim(t, db, "cl1", "x", "fact", "active", 0.8, now)

	if _, err := resetDB(context.Background(), db, true); err != nil {
		t.Fatalf("reset: %v", err)
	}

	q := sqlcgen.New(db)
	gotEvents, _ := q.ListAllEvents(context.Background())
	if len(gotEvents) != 1 {
		t.Fatalf("events should be kept, got %d", len(gotEvents))
	}
	gotClaims, _ := q.ListAllClaims(context.Background())
	if len(gotClaims) != 0 {
		t.Fatalf("claims should be deleted, got %d", len(gotClaims))
	}
}

func TestDeleteClaim_RemovesDerivedRows(t *testing.T) {
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "del.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	now := time.Now().UTC()
	seedEvent(t, db, "ev1", "r", "content", "in1", `{}`, now)
	seedClaim(t, db, "cl1", "doomed", "fact", "active", 0.8, now)
	seedClaim(t, db, "cl2", "neighbor", "fact", "active", 0.8, now)
	seedRelationship(t, db, "rl1", "supports", "cl1", "cl2", now)

	ctx := context.Background()
	err = withTx(ctx, db, func(q *sqlcgen.Queries) error {
		if err := q.DeleteRelationshipsByClaimID(ctx, sqlcgen.DeleteRelationshipsByClaimIDParams{
			FromClaimID: "cl1", ToClaimID: "cl1",
		}); err != nil {
			return err
		}
		if err := q.DeleteClaimEvidenceByClaimID(ctx, "cl1"); err != nil {
			return err
		}
		if err := q.DeleteClaimStatusHistoryByClaimID(ctx, "cl1"); err != nil {
			return err
		}
		return q.DeleteClaimByID(ctx, "cl1")
	})
	if err != nil {
		t.Fatalf("delete tx: %v", err)
	}

	q := sqlcgen.New(db)
	claims, _ := q.ListAllClaims(ctx)
	if len(claims) != 1 || claims[0].ID != "cl2" {
		t.Fatalf("expected cl2 to remain, got %+v", claims)
	}
	rels, _ := q.ListRelationshipsByClaim(ctx, sqlcgen.ListRelationshipsByClaimParams{
		FromClaimID: "cl2", ToClaimID: "cl2",
	})
	if len(rels) != 0 {
		t.Fatalf("relationships referencing cl1 should be gone, got %d", len(rels))
	}
}

func TestListEntityIDsMissingEmbedding(t *testing.T) {
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "miss.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	now := time.Now().UTC()
	seedClaim(t, db, "with", "has embedding", "fact", "active", 0.8, now)
	seedClaim(t, db, "without", "no embedding", "fact", "active", 0.8, now)

	if _, err := db.Exec(
		`INSERT INTO embeddings (entity_id, entity_type, vector, model, dimensions, created_at) VALUES (?, 'claim', ?, 'm', 3, ?)`,
		"with", []byte{0, 0, 0}, now.Format(time.RFC3339),
	); err != nil {
		t.Fatalf("seed embedding: %v", err)
	}

	q := sqlcgen.New(db)
	missing, err := q.ListEntityIDsMissingEmbedding(context.Background())
	if err != nil {
		t.Fatalf("list missing: %v", err)
	}
	if len(missing) != 1 || missing[0] != "without" {
		t.Fatalf("expected [without], got %v", missing)
	}
}
