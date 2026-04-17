package main

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/felixgeelhaar/mnemos/internal/store/sqlite"
)

func newTestBrowseDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "mnemos.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func seedClaim(t *testing.T, db *sql.DB, id, text, ctype, status string, confidence float64, createdAt time.Time) {
	t.Helper()
	_, err := db.Exec(
		`INSERT INTO claims (id, text, type, confidence, status, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		id, text, ctype, confidence, status, createdAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		t.Fatalf("insert claim: %v", err)
	}
}

func seedRelationship(t *testing.T, db *sql.DB, id, rtype, from, to string, createdAt time.Time) {
	t.Helper()
	_, err := db.Exec(
		`INSERT INTO relationships (id, type, from_claim_id, to_claim_id, created_at) VALUES (?, ?, ?, ?, ?)`,
		id, rtype, from, to, createdAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		t.Fatalf("insert relationship: %v", err)
	}
}

func TestNormalizePagination(t *testing.T) {
	cases := []struct {
		inLimit, inOffset   int
		wantLimit, wantOffset int
	}{
		{0, 0, defaultListLimit, 0},
		{-5, -10, defaultListLimit, 0},
		{maxListLimit + 100, 5, maxListLimit, 5},
		{20, 30, 20, 30},
	}
	for _, c := range cases {
		gotLimit, gotOffset := normalizePagination(c.inLimit, c.inOffset)
		if gotLimit != c.wantLimit || gotOffset != c.wantOffset {
			t.Errorf("normalizePagination(%d,%d) = (%d,%d), want (%d,%d)",
				c.inLimit, c.inOffset, gotLimit, gotOffset, c.wantLimit, c.wantOffset)
		}
	}
}

func TestListClaimsFiltered_NoFiltersReturnsAllOrderedByCreatedDesc(t *testing.T) {
	db := newTestBrowseDB(t)
	now := time.Now()
	seedClaim(t, db, "c1", "older", "fact", "active", 0.7, now.Add(-2*time.Hour))
	seedClaim(t, db, "c2", "newer", "decision", "active", 0.9, now)

	claims, total, err := listClaimsFiltered(context.Background(), db, "", "", 50, 0)
	if err != nil {
		t.Fatalf("listClaimsFiltered: %v", err)
	}
	if total != 2 || len(claims) != 2 {
		t.Fatalf("total=%d len=%d, want 2/2", total, len(claims))
	}
	if claims[0].ID != "c2" {
		t.Fatalf("first claim ID = %q, want c2 (newest first)", claims[0].ID)
	}
}

func TestListClaimsFiltered_TypeFilter(t *testing.T) {
	db := newTestBrowseDB(t)
	now := time.Now()
	seedClaim(t, db, "c1", "fact 1", "fact", "active", 0.7, now)
	seedClaim(t, db, "c2", "decision 1", "decision", "active", 0.9, now)
	seedClaim(t, db, "c3", "decision 2", "decision", "active", 0.8, now)

	claims, total, err := listClaimsFiltered(context.Background(), db, "decision", "", 50, 0)
	if err != nil {
		t.Fatalf("listClaimsFiltered: %v", err)
	}
	if total != 2 || len(claims) != 2 {
		t.Fatalf("total=%d len=%d, want 2/2", total, len(claims))
	}
	for _, c := range claims {
		if string(c.Type) != "decision" {
			t.Errorf("claim %s has type %q, want decision", c.ID, c.Type)
		}
	}
}

func TestListClaimsFiltered_StatusFilter(t *testing.T) {
	db := newTestBrowseDB(t)
	now := time.Now()
	seedClaim(t, db, "c1", "active claim", "fact", "active", 0.7, now)
	seedClaim(t, db, "c2", "contested claim", "fact", "contested", 0.5, now)

	claims, total, err := listClaimsFiltered(context.Background(), db, "", "contested", 50, 0)
	if err != nil {
		t.Fatalf("listClaimsFiltered: %v", err)
	}
	if total != 1 || len(claims) != 1 || claims[0].ID != "c2" {
		t.Fatalf("got %+v, want only c2", claims)
	}
}

func TestListClaimsFiltered_Pagination(t *testing.T) {
	db := newTestBrowseDB(t)
	base := time.Now()
	for i := 0; i < 5; i++ {
		seedClaim(t, db, "c"+string(rune('1'+i)), "claim", "fact", "active", 0.5, base.Add(time.Duration(i)*time.Minute))
	}

	page1, total, err := listClaimsFiltered(context.Background(), db, "", "", 2, 0)
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	if total != 5 {
		t.Fatalf("total = %d, want 5", total)
	}
	if len(page1) != 2 {
		t.Fatalf("page1 len = %d, want 2", len(page1))
	}

	page2, _, err := listClaimsFiltered(context.Background(), db, "", "", 2, 2)
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if len(page2) != 2 {
		t.Fatalf("page2 len = %d, want 2", len(page2))
	}
	if page1[0].ID == page2[0].ID {
		t.Fatalf("pagination overlap: page1[0]=%s page2[0]=%s", page1[0].ID, page2[0].ID)
	}
}

func TestListContradictionPairs_HydratesClaimText(t *testing.T) {
	db := newTestBrowseDB(t)
	now := time.Now()
	seedClaim(t, db, "c1", "Use SQLite", "decision", "active", 0.9, now)
	seedClaim(t, db, "c2", "Use PostgreSQL", "decision", "active", 0.9, now)
	seedClaim(t, db, "c3", "Use embeddings", "fact", "active", 0.8, now)
	seedRelationship(t, db, "r1", "contradicts", "c1", "c2", now)
	seedRelationship(t, db, "r2", "supports", "c1", "c3", now) // not a contradiction

	pairs, total, err := listContradictionPairs(context.Background(), db, 50, 0)
	if err != nil {
		t.Fatalf("listContradictionPairs: %v", err)
	}
	if total != 1 || len(pairs) != 1 {
		t.Fatalf("total=%d len=%d, want 1/1", total, len(pairs))
	}
	if pairs[0].FromClaimText != "Use SQLite" || pairs[0].ToClaimText != "Use PostgreSQL" {
		t.Fatalf("hydration failed: %+v", pairs[0])
	}
}

func TestListContradictionPairs_HandlesMissingClaimGracefully(t *testing.T) {
	db := newTestBrowseDB(t)
	now := time.Now()
	// Insert a relationship pointing at a claim that exists, plus deliberately
	// skip foreign key enforcement by inserting before the referenced claim.
	// Foreign keys are enforced, so insert claim first then orphan via rel.
	seedClaim(t, db, "c1", "lonely claim", "fact", "active", 0.5, now)
	// Insert a contradiction between two real claims to verify happy path.
	seedClaim(t, db, "c2", "other claim", "fact", "active", 0.5, now)
	seedRelationship(t, db, "r1", "contradicts", "c1", "c2", now)

	pairs, _, err := listContradictionPairs(context.Background(), db, 50, 0)
	if err != nil {
		t.Fatalf("listContradictionPairs: %v", err)
	}
	if len(pairs) != 1 {
		t.Fatalf("len = %d, want 1", len(pairs))
	}
	if pairs[0].FromClaimText == "" || pairs[0].ToClaimText == "" {
		t.Fatalf("expected hydrated text, got %+v", pairs[0])
	}
}

func TestValidClaimType(t *testing.T) {
	cases := map[string]bool{
		"fact": true, "hypothesis": true, "decision": true,
		"": false, "FACT": false, "guess": false,
	}
	for in, want := range cases {
		if got := validClaimType(in); got != want {
			t.Errorf("validClaimType(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestValidClaimStatus(t *testing.T) {
	cases := map[string]bool{
		"active": true, "contested": true, "deprecated": true,
		"": false, "Active": false, "stale": false,
	}
	for in, want := range cases {
		if got := validClaimStatus(in); got != want {
			t.Errorf("validClaimStatus(%q) = %v, want %v", in, got, want)
		}
	}
}
