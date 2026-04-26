-- name: UpsertEmbedding :exec
INSERT INTO embeddings (entity_id, entity_type, vector, model, dimensions, created_at, created_by)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (entity_id, entity_type) DO UPDATE SET
  vector = excluded.vector,
  model = excluded.model,
  dimensions = excluded.dimensions,
  created_at = excluded.created_at,
  created_by = excluded.created_by;

-- name: GetEmbeddingByEntityID :one
SELECT entity_id, entity_type, vector, model, dimensions, created_at, created_by
FROM embeddings
WHERE entity_id = ? AND entity_type = ?;

-- name: ListEmbeddingsByEntityType :many
SELECT entity_id, entity_type, vector, model, dimensions, created_at, created_by
FROM embeddings
WHERE entity_type = ?
ORDER BY created_at ASC;

-- name: DeleteEmbeddingByEntity :exec
DELETE FROM embeddings WHERE entity_id = ? AND entity_type = ?;

-- name: DeleteEmbeddingsByEntityType :exec
DELETE FROM embeddings WHERE entity_type = ?;

-- name: DeleteAllEmbeddings :exec
DELETE FROM embeddings;

-- name: ListEntityIDsMissingEmbedding :many
-- Returns claim ids that don't yet have an embedding (entity_type='claim').
SELECT id FROM claims
WHERE id NOT IN (SELECT entity_id FROM embeddings WHERE entity_type = 'claim')
ORDER BY created_at ASC;
