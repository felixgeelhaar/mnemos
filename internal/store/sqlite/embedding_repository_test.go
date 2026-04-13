package sqlite

import (
	"context"
	"testing"
)

func TestEmbeddingRepositoryUpsertAndGet(t *testing.T) {
	db := openTestDB(t)
	defer closeDB(db)

	repo := NewEmbeddingRepository(db)
	vector := []float32{0.1, 0.2, 0.3, 0.4}

	if err := repo.Upsert(context.Background(), "ev_1", "event", vector, "test-model"); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	got, err := repo.GetByEntityID(context.Background(), "ev_1", "event")
	if err != nil {
		t.Fatalf("GetByEntityID() error = %v", err)
	}
	if got.EntityID != "ev_1" {
		t.Errorf("EntityID = %q, want %q", got.EntityID, "ev_1")
	}
	if got.EntityType != "event" {
		t.Errorf("EntityType = %q, want %q", got.EntityType, "event")
	}
	if got.Model != "test-model" {
		t.Errorf("Model = %q, want %q", got.Model, "test-model")
	}
	if got.Dimensions != 4 {
		t.Errorf("Dimensions = %d, want 4", got.Dimensions)
	}
	if len(got.Vector) != 4 {
		t.Fatalf("Vector length = %d, want 4", len(got.Vector))
	}
	for i, v := range vector {
		if got.Vector[i] != v {
			t.Errorf("Vector[%d] = %f, want %f", i, got.Vector[i], v)
		}
	}
}

func TestEmbeddingRepositoryUpsertOverwrite(t *testing.T) {
	db := openTestDB(t)
	defer closeDB(db)

	repo := NewEmbeddingRepository(db)

	if err := repo.Upsert(context.Background(), "ev_1", "event", []float32{0.1, 0.2}, "model-a"); err != nil {
		t.Fatalf("first Upsert() error = %v", err)
	}
	if err := repo.Upsert(context.Background(), "ev_1", "event", []float32{0.9, 0.8, 0.7}, "model-b"); err != nil {
		t.Fatalf("second Upsert() error = %v", err)
	}

	got, err := repo.GetByEntityID(context.Background(), "ev_1", "event")
	if err != nil {
		t.Fatalf("GetByEntityID() error = %v", err)
	}
	if got.Model != "model-b" {
		t.Errorf("Model = %q, want %q (should be overwritten)", got.Model, "model-b")
	}
	if len(got.Vector) != 3 {
		t.Errorf("Vector length = %d, want 3 (should be overwritten)", len(got.Vector))
	}
}

func TestEmbeddingRepositoryListByEntityType(t *testing.T) {
	db := openTestDB(t)
	defer closeDB(db)

	repo := NewEmbeddingRepository(db)

	if err := repo.Upsert(context.Background(), "ev_1", "event", []float32{0.1}, "model"); err != nil {
		t.Fatalf("Upsert ev_1 error = %v", err)
	}
	if err := repo.Upsert(context.Background(), "ev_2", "event", []float32{0.2}, "model"); err != nil {
		t.Fatalf("Upsert ev_2 error = %v", err)
	}
	if err := repo.Upsert(context.Background(), "cl_1", "claim", []float32{0.3}, "model"); err != nil {
		t.Fatalf("Upsert cl_1 error = %v", err)
	}

	events, err := repo.ListByEntityType(context.Background(), "event")
	if err != nil {
		t.Fatalf("ListByEntityType(event) error = %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("got %d event embeddings, want 2", len(events))
	}

	claims, err := repo.ListByEntityType(context.Background(), "claim")
	if err != nil {
		t.Fatalf("ListByEntityType(claim) error = %v", err)
	}
	if len(claims) != 1 {
		t.Fatalf("got %d claim embeddings, want 1", len(claims))
	}
}

func TestEmbeddingRepositoryGetNotFound(t *testing.T) {
	db := openTestDB(t)
	defer closeDB(db)

	repo := NewEmbeddingRepository(db)
	_, err := repo.GetByEntityID(context.Background(), "nonexistent", "event")
	if err == nil {
		t.Fatal("expected error for nonexistent embedding")
	}
}
