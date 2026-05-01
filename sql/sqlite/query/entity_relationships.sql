-- name: UpsertEntityRelationship :exec
INSERT INTO entity_relationships (id, kind, from_id, from_type, to_id, to_type, created_at, created_by)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(kind, from_type, from_id, to_type, to_id) DO NOTHING;

-- name: ListEntityRelationshipsByEntity :many
SELECT id, kind, from_id, from_type, to_id, to_type, created_at, created_by
FROM entity_relationships
WHERE (from_id = ?1 AND from_type = ?2) OR (to_id = ?1 AND to_type = ?2)
ORDER BY created_at ASC;

-- name: ListEntityRelationshipsByKind :many
SELECT id, kind, from_id, from_type, to_id, to_type, created_at, created_by
FROM entity_relationships
WHERE kind = ?
ORDER BY created_at ASC;

-- name: ListEntityRelationships :many
SELECT id, kind, from_id, from_type, to_id, to_type, created_at, created_by
FROM entity_relationships
ORDER BY created_at ASC;

-- name: CountEntityRelationships :one
SELECT COUNT(*) FROM entity_relationships;

-- name: DeleteAllEntityRelationships :exec
DELETE FROM entity_relationships;
