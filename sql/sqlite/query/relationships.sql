-- name: UpsertRelationship :exec
INSERT INTO relationships (id, type, from_claim_id, to_claim_id, created_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(type, from_claim_id, to_claim_id) DO UPDATE SET
  created_at = excluded.created_at;

-- name: ListRelationshipsByClaim :many
SELECT id, type, from_claim_id, to_claim_id, created_at
FROM relationships
WHERE from_claim_id = ? OR to_claim_id = ?
ORDER BY created_at ASC;
