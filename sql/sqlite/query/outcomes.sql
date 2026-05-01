-- name: CreateOutcome :exec
-- Idempotent - outcomes are append-only.
INSERT INTO outcomes (id, action_id, result, metrics_json, notes, observed_at, source, created_by, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO NOTHING;

-- name: GetOutcomeByID :one
SELECT id, action_id, result, metrics_json, notes, observed_at, source, created_by, created_at
FROM outcomes
WHERE id = ?;

-- name: ListOutcomesByActionID :many
SELECT id, action_id, result, metrics_json, notes, observed_at, source, created_by, created_at
FROM outcomes
WHERE action_id = ?
ORDER BY observed_at ASC;

-- name: ListAllOutcomes :many
SELECT id, action_id, result, metrics_json, notes, observed_at, source, created_by, created_at
FROM outcomes
ORDER BY observed_at ASC;

-- name: CountOutcomes :one
SELECT COUNT(*) FROM outcomes;

-- name: DeleteAllOutcomes :exec
DELETE FROM outcomes;
