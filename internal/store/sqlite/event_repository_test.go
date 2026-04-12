package sqlite

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/felixgeelhaar/mnemos/internal/domain"
)

func TestEventRepositoryAppendAndGetByID(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	repo := NewEventRepository(db)
	now := time.Date(2026, 4, 12, 12, 30, 0, 0, time.UTC)
	event := domain.Event{
		ID:            "ev_1",
		SchemaVersion: "v1",
		Content:       "Claim moved from active to contested",
		SourceInputID: "in_1",
		Timestamp:     now,
		IngestedAt:    now,
		Metadata: map[string]string{
			"chunk_kind": "text",
		},
	}

	if err := repo.Append(event); err != nil {
		t.Fatalf("Append() error = %v", err)
	}

	got, err := repo.GetByID("ev_1")
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if got.ID != event.ID {
		t.Fatalf("GetByID() id = %q, want %q", got.ID, event.ID)
	}
	if got.Metadata["chunk_kind"] != "text" {
		t.Fatalf("GetByID() metadata chunk_kind = %q, want text", got.Metadata["chunk_kind"])
	}
}

func TestEventRepositoryListByIDsOrder(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	repo := NewEventRepository(db)
	now := time.Now().UTC()
	seed := []domain.Event{
		{ID: "ev_a", SchemaVersion: "v1", Content: "a", SourceInputID: "in_1", Timestamp: now, IngestedAt: now, Metadata: map[string]string{}},
		{ID: "ev_b", SchemaVersion: "v1", Content: "b", SourceInputID: "in_1", Timestamp: now, IngestedAt: now, Metadata: map[string]string{}},
		{ID: "ev_c", SchemaVersion: "v1", Content: "c", SourceInputID: "in_1", Timestamp: now, IngestedAt: now, Metadata: map[string]string{}},
	}
	for _, event := range seed {
		if err := repo.Append(event); err != nil {
			t.Fatalf("Append() error = %v", err)
		}
	}

	got, err := repo.ListByIDs([]string{"ev_c", "ev_a", "ev_missing"})
	if err != nil {
		t.Fatalf("ListByIDs() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListByIDs() len = %d, want 2", len(got))
	}
	if got[0].ID != "ev_c" || got[1].ID != "ev_a" {
		t.Fatalf("ListByIDs() order = [%s, %s], want [ev_c, ev_a]", got[0].ID, got[1].ID)
	}
}

func TestEventRepositoryListAll(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	repo := NewEventRepository(db)
	now := time.Now().UTC()
	seed := []domain.Event{
		{ID: "ev_1", SchemaVersion: "v1", Content: "one", SourceInputID: "in_1", Timestamp: now, IngestedAt: now, Metadata: map[string]string{}},
		{ID: "ev_2", SchemaVersion: "v1", Content: "two", SourceInputID: "in_1", Timestamp: now.Add(time.Second), IngestedAt: now, Metadata: map[string]string{}},
	}
	for _, event := range seed {
		if err := repo.Append(event); err != nil {
			t.Fatalf("Append() error = %v", err)
		}
	}

	events, err := repo.ListAll()
	if err != nil {
		t.Fatalf("ListAll() error = %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("ListAll() len = %d, want 2", len(events))
	}
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	return db
}
