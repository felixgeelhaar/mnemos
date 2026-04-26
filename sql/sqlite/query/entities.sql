-- name: UpsertEntity :exec
-- Insert a new entity or noop if (normalized_name, type) already exists.
-- The id is supplied by the caller so concurrent insert races still hit
-- the UNIQUE constraint and DO NOTHING; FindEntityByNormalizedName then
-- returns the canonical row.
INSERT INTO entities (id, name, normalized_name, type, created_at, created_by)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(normalized_name, type) DO NOTHING;

-- name: FindEntityByNormalizedName :one
SELECT id, name, normalized_name, type, created_at, created_by
FROM entities
WHERE normalized_name = ? AND type = ?;

-- name: GetEntityByID :one
SELECT id, name, normalized_name, type, created_at, created_by
FROM entities
WHERE id = ?;

-- name: ListEntities :many
SELECT id, name, normalized_name, type, created_at, created_by
FROM entities
ORDER BY name ASC;

-- name: ListEntitiesByType :many
SELECT id, name, normalized_name, type, created_at, created_by
FROM entities
WHERE type = ?
ORDER BY name ASC;

-- name: SearchEntitiesByNamePrefix :many
SELECT id, name, normalized_name, type, created_at, created_by
FROM entities
WHERE normalized_name LIKE CAST(? AS TEXT) || '%'
ORDER BY name ASC;

-- name: DeleteEntityByID :exec
DELETE FROM entities WHERE id = ?;

-- name: UpsertClaimEntity :exec
INSERT INTO claim_entities (claim_id, entity_id, role)
VALUES (?, ?, ?)
ON CONFLICT(claim_id, entity_id, role) DO NOTHING;

-- name: DeleteClaimEntitiesByClaimID :exec
DELETE FROM claim_entities WHERE claim_id = ?;

-- name: DeleteClaimEntitiesByEntityID :exec
DELETE FROM claim_entities WHERE entity_id = ?;

-- name: ReassignClaimEntitiesEntity :exec
-- Used by entity merge: redirect every claim_entities row from the
-- losing entity to the winner. INSERT OR IGNORE prevents the unique
-- constraint from firing when both entities already linked the same
-- claim under the same role.
INSERT OR IGNORE INTO claim_entities (claim_id, entity_id, role)
SELECT ce.claim_id, ?, ce.role FROM claim_entities ce WHERE ce.entity_id = ?;

-- name: ListClaimsByEntityID :many
SELECT c.id, c.text, c.type, c.confidence, c.status, c.created_at, c.created_by, c.trust_score, c.valid_from, c.valid_to
FROM claims c
JOIN claim_entities ce ON ce.claim_id = c.id
WHERE ce.entity_id = ?
ORDER BY c.created_at ASC;

-- name: ListEntitiesByClaimID :many
SELECT e.id, e.name, e.normalized_name, e.type, e.created_at, e.created_by, ce.role
FROM entities e
JOIN claim_entities ce ON ce.entity_id = e.id
WHERE ce.claim_id = ?
ORDER BY e.name ASC;

-- name: CountEntities :one
SELECT COUNT(*) AS n FROM entities;

-- name: ClaimIDsMissingEntityLinks :many
-- Used by `mnemos extract-entities` to find claims that have no entity
-- links yet (post v0.9 backfill candidates).
SELECT id FROM claims
WHERE id NOT IN (SELECT DISTINCT claim_id FROM claim_entities)
ORDER BY created_at ASC;
