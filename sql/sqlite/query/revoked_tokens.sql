-- name: AddRevokedToken :exec
INSERT INTO revoked_tokens (jti, revoked_at, expires_at)
VALUES (?, ?, ?)
ON CONFLICT(jti) DO NOTHING;

-- name: IsTokenRevoked :one
SELECT 1 AS present
FROM revoked_tokens
WHERE jti = ?
LIMIT 1;

-- name: PurgeExpiredRevokedTokens :execrows
DELETE FROM revoked_tokens WHERE expires_at < ?;
