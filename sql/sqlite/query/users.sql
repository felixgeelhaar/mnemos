-- name: CreateUser :exec
INSERT INTO users (id, name, email, status, scopes_json, created_at)
VALUES (?, ?, ?, ?, ?, ?);

-- name: GetUserByID :one
SELECT id, name, email, status, scopes_json, created_at
FROM users
WHERE id = ?;

-- name: GetUserByEmail :one
SELECT id, name, email, status, scopes_json, created_at
FROM users
WHERE email = ?;

-- name: ListUsers :many
SELECT id, name, email, status, scopes_json, created_at
FROM users
ORDER BY created_at ASC;

-- name: UpdateUserStatus :exec
UPDATE users SET status = ? WHERE id = ?;

-- name: UpdateUserScopes :exec
UPDATE users SET scopes_json = ? WHERE id = ?;
