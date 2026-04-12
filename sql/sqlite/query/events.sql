-- name: CreateEvent :exec
INSERT INTO events (id, schema_version, content, source_input_id, timestamp, metadata_json, ingested_at)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: GetEventByID :one
SELECT id, schema_version, content, source_input_id, timestamp, metadata_json, ingested_at
FROM events
WHERE id = ?;

-- name: ListAllEvents :many
SELECT id, schema_version, content, source_input_id, timestamp, metadata_json, ingested_at
FROM events
ORDER BY timestamp ASC;
