-- name: CreateAgent :exec
INSERT INTO agents (id, name, owner_id, scopes_json, allowed_runs_json, status, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: GetAgentByID :one
SELECT id, name, owner_id, scopes_json, allowed_runs_json, status, created_at
FROM agents
WHERE id = ?;

-- name: ListAgents :many
SELECT id, name, owner_id, scopes_json, allowed_runs_json, status, created_at
FROM agents
ORDER BY created_at ASC;

-- name: UpdateAgentStatus :exec
UPDATE agents SET status = ? WHERE id = ?;

-- name: UpdateAgentScopes :exec
UPDATE agents SET scopes_json = ? WHERE id = ?;

-- name: UpdateAgentAllowedRuns :exec
UPDATE agents SET allowed_runs_json = ? WHERE id = ?;

-- name: UpsertAgent :exec
INSERT INTO agents (id, name, owner_id, scopes_json, allowed_runs_json, status, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    name = excluded.name,
    owner_id = excluded.owner_id,
    scopes_json = excluded.scopes_json,
    allowed_runs_json = excluded.allowed_runs_json,
    status = excluded.status;
