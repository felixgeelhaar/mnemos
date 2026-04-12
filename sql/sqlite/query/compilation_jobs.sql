-- name: UpsertCompilationJob :exec
INSERT INTO compilation_jobs (id, kind, status, scope_json, started_at, updated_at, error)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  kind = excluded.kind,
  status = excluded.status,
  scope_json = excluded.scope_json,
  started_at = excluded.started_at,
  updated_at = excluded.updated_at,
  error = excluded.error;

-- name: GetCompilationJobByID :one
SELECT id, kind, status, scope_json, started_at, updated_at, error
FROM compilation_jobs
WHERE id = ?;
