-- name: CreateEvent :exec
-- Idempotent so retried pushes (and re-runs of best-effort
-- pipelines) don't fail with PRIMARY KEY constraint violations.
-- Events are append-only by domain contract; once an id is in the
-- table its content is fixed, so the conflict path is a no-op.
INSERT INTO events (id, run_id, schema_version, content, source_input_id, timestamp, metadata_json, ingested_at, created_by)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO NOTHING;

-- name: GetEventByID :one
SELECT id, run_id, schema_version, content, source_input_id, timestamp, metadata_json, ingested_at, created_by
FROM events
WHERE id = ?;

-- name: ListAllEvents :many
SELECT id, run_id, schema_version, content, source_input_id, timestamp, metadata_json, ingested_at, created_by
FROM events
ORDER BY timestamp ASC;

-- name: ListEventsByRunID :many
SELECT id, run_id, schema_version, content, source_input_id, timestamp, metadata_json, ingested_at, created_by
FROM events
WHERE run_id = ?
ORDER BY timestamp ASC;
