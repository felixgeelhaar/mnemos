-- name: UpsertEmbedding :exec
INSERT INTO embeddings (entity_id, entity_type, vector, model, dimensions, created_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT (entity_id, entity_type) DO UPDATE SET
  vector = excluded.vector,
  model = excluded.model,
  dimensions = excluded.dimensions,
  created_at = excluded.created_at;

-- name: GetEmbeddingByEntityID :one
SELECT entity_id, entity_type, vector, model, dimensions, created_at
FROM embeddings
WHERE entity_id = ? AND entity_type = ?;

-- name: ListEmbeddingsByEntityType :many
SELECT entity_id, entity_type, vector, model, dimensions, created_at
FROM embeddings
WHERE entity_type = ?
ORDER BY created_at ASC;
