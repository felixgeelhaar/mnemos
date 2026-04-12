-- name: CreateEvent :exec
INSERT INTO events (id, run_id, schema_version, content, source_input_id, timestamp, metadata_json, ingested_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetEventByID :one
SELECT id, run_id, schema_version, content, source_input_id, timestamp, metadata_json, ingested_at
FROM events
WHERE id = ?;

-- name: ListAllEvents :many
SELECT id, run_id, schema_version, content, source_input_id, timestamp, metadata_json, ingested_at
FROM events
ORDER BY timestamp ASC;

-- name: ListEventsByRunID :many
SELECT id, run_id, schema_version, content, source_input_id, timestamp, metadata_json, ingested_at
FROM events
WHERE run_id = ?
ORDER BY timestamp ASC;
