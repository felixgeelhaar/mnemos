package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/felixgeelhaar/mnemos/internal/domain"
	"github.com/felixgeelhaar/mnemos/internal/store/sqlite/sqlcgen"
)

type CompilationJobRepository struct {
	db *sql.DB
	q  *sqlcgen.Queries
}

func NewCompilationJobRepository(db *sql.DB) CompilationJobRepository {
	return CompilationJobRepository{db: db, q: sqlcgen.New(db)}
}

func (r CompilationJobRepository) Upsert(job domain.CompilationJob) error {
	scopeJSON, err := json.Marshal(job.Scope)
	if err != nil {
		return fmt.Errorf("marshal job scope: %w", err)
	}

	err = r.q.UpsertCompilationJob(context.Background(), sqlcgen.UpsertCompilationJobParams{
		ID:        job.ID,
		Kind:      job.Kind,
		Status:    job.Status,
		ScopeJson: string(scopeJSON),
		StartedAt: job.StartedAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt: job.UpdatedAt.UTC().Format(time.RFC3339Nano),
		Error:     job.Error,
	})
	if err != nil {
		return fmt.Errorf("upsert compilation job %s: %w", job.ID, err)
	}

	return nil
}

func (r CompilationJobRepository) GetByID(id string) (domain.CompilationJob, error) {
	row, err := r.q.GetCompilationJobByID(context.Background(), id)
	if err != nil {
		return domain.CompilationJob{}, fmt.Errorf("get compilation job %s: %w", id, err)
	}

	job := domain.CompilationJob{
		ID:     row.ID,
		Kind:   row.Kind,
		Status: row.Status,
		Error:  row.Error,
	}

	if err := json.Unmarshal([]byte(row.ScopeJson), &job.Scope); err != nil {
		return domain.CompilationJob{}, fmt.Errorf("unmarshal job scope: %w", err)
	}
	startedAt, err := time.Parse(time.RFC3339Nano, row.StartedAt)
	if err != nil {
		return domain.CompilationJob{}, fmt.Errorf("parse job started_at: %w", err)
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, row.UpdatedAt)
	if err != nil {
		return domain.CompilationJob{}, fmt.Errorf("parse job updated_at: %w", err)
	}
	job.StartedAt = startedAt
	job.UpdatedAt = updatedAt

	return job, nil
}
