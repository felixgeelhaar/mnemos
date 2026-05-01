-- name: CreatePlaybook :exec
-- Idempotent on id; UPSERT refreshes statement, steps, confidence,
-- derived_at, last_verified so re-synthesis ratchets identity-stably.
INSERT INTO playbooks (id, trigger, statement, scope_service, scope_env, scope_team, steps_json, confidence, derived_at, last_verified, source, created_by)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  trigger = excluded.trigger,
  statement = excluded.statement,
  steps_json = excluded.steps_json,
  confidence = excluded.confidence,
  derived_at = excluded.derived_at,
  last_verified = excluded.last_verified;

-- name: GetPlaybookByID :one
SELECT id, trigger, statement, scope_service, scope_env, scope_team, steps_json, confidence, derived_at, last_verified, source, created_by
FROM playbooks
WHERE id = ?;

-- name: ListPlaybooksByTrigger :many
SELECT id, trigger, statement, scope_service, scope_env, scope_team, steps_json, confidence, derived_at, last_verified, source, created_by
FROM playbooks
WHERE trigger = ?
ORDER BY confidence DESC, derived_at DESC;

-- name: ListPlaybooksByService :many
SELECT id, trigger, statement, scope_service, scope_env, scope_team, steps_json, confidence, derived_at, last_verified, source, created_by
FROM playbooks
WHERE scope_service = ?
ORDER BY confidence DESC, derived_at DESC;

-- name: ListAllPlaybooks :many
SELECT id, trigger, statement, scope_service, scope_env, scope_team, steps_json, confidence, derived_at, last_verified, source, created_by
FROM playbooks
ORDER BY confidence DESC, derived_at DESC;

-- name: CountPlaybooks :one
SELECT COUNT(*) FROM playbooks;

-- name: DeleteAllPlaybooks :exec
DELETE FROM playbooks;

-- name: AppendPlaybookLesson :exec
INSERT INTO playbook_lessons (playbook_id, lesson_id)
VALUES (?, ?)
ON CONFLICT(playbook_id, lesson_id) DO NOTHING;

-- name: ListPlaybookLessons :many
SELECT playbook_id, lesson_id
FROM playbook_lessons
WHERE playbook_id = ?;

-- name: DeleteAllPlaybookLessons :exec
DELETE FROM playbook_lessons;
