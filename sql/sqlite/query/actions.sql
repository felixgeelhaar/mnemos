-- name: CreateAction :exec
-- Idempotent - actions are append-only and immutable once written.
INSERT INTO actions (id, run_id, kind, subject, actor, at, metadata_json, created_by, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO NOTHING;

-- name: GetActionByID :one
SELECT id, run_id, kind, subject, actor, at, metadata_json, created_by, created_at
FROM actions
WHERE id = ?;

-- name: ListActionsByRunID :many
SELECT id, run_id, kind, subject, actor, at, metadata_json, created_by, created_at
FROM actions
WHERE run_id = ?
ORDER BY at ASC;

-- name: ListActionsBySubject :many
SELECT id, run_id, kind, subject, actor, at, metadata_json, created_by, created_at
FROM actions
WHERE subject = ?
ORDER BY at ASC;

-- name: ListAllActions :many
SELECT id, run_id, kind, subject, actor, at, metadata_json, created_by, created_at
FROM actions
ORDER BY at ASC;

-- name: CountActions :one
SELECT COUNT(*) FROM actions;

-- name: DeleteAllActions :exec
DELETE FROM actions;
