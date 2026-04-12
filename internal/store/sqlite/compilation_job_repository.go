package sqlite

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/felixgeelhaar/mnemos/internal/domain"
)

type CompilationJobRepository struct {
	db *sql.DB
}

func NewCompilationJobRepository(db *sql.DB) CompilationJobRepository {
	return CompilationJobRepository{db: db}
}

func (r CompilationJobRepository) Upsert(job domain.CompilationJob) error {
	scopeJSON, err := json.Marshal(job.Scope)
	if err != nil {
		return fmt.Errorf("marshal job scope: %w", err)
	}

	const upsert = `
INSERT INTO compilation_jobs (id, kind, status, scope_json, started_at, updated_at, error)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
	kind = excluded.kind,
	status = excluded.status,
	scope_json = excluded.scope_json,
	started_at = excluded.started_at,
	updated_at = excluded.updated_at,
	error = excluded.error`

	_, err = r.db.Exec(
		upsert,
		job.ID,
		job.Kind,
		job.Status,
		string(scopeJSON),
		job.StartedAt.UTC().Format(time.RFC3339Nano),
		job.UpdatedAt.UTC().Format(time.RFC3339Nano),
		job.Error,
	)
	if err != nil {
		return fmt.Errorf("upsert compilation job %s: %w", job.ID, err)
	}

	return nil
}

func (r CompilationJobRepository) GetByID(id string) (domain.CompilationJob, error) {
	const query = `
SELECT id, kind, status, scope_json, started_at, updated_at, error
FROM compilation_jobs
WHERE id = ?`

	var (
		job          domain.CompilationJob
		scopeJSON    string
		startedAtRaw string
		updatedAtRaw string
	)

	if err := r.db.QueryRow(query, id).Scan(
		&job.ID,
		&job.Kind,
		&job.Status,
		&scopeJSON,
		&startedAtRaw,
		&updatedAtRaw,
		&job.Error,
	); err != nil {
		return domain.CompilationJob{}, fmt.Errorf("get compilation job %s: %w", id, err)
	}

	if err := json.Unmarshal([]byte(scopeJSON), &job.Scope); err != nil {
		return domain.CompilationJob{}, fmt.Errorf("unmarshal job scope: %w", err)
	}
	startedAt, err := time.Parse(time.RFC3339Nano, startedAtRaw)
	if err != nil {
		return domain.CompilationJob{}, fmt.Errorf("parse job started_at: %w", err)
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, updatedAtRaw)
	if err != nil {
		return domain.CompilationJob{}, fmt.Errorf("parse job updated_at: %w", err)
	}
	job.StartedAt = startedAt
	job.UpdatedAt = updatedAt

	return job, nil
}
