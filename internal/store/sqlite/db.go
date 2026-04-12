package sqlite

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

func Open(path string) (*sql.DB, error) {
	if path == "" {
		return nil, fmt.Errorf("database path is required")
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}

	if err := ensureSchema(db); err != nil {
		_ = db.Close()
		return nil, err
	}

	return db, nil
}

func ensureSchema(db *sql.DB) error {
	const schema = `
CREATE TABLE IF NOT EXISTS events (
	id TEXT PRIMARY KEY,
	schema_version TEXT NOT NULL,
	content TEXT NOT NULL,
	source_input_id TEXT NOT NULL,
	timestamp TEXT NOT NULL,
	metadata_json TEXT NOT NULL,
	ingested_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_events_timestamp ON events(timestamp);
CREATE INDEX IF NOT EXISTS idx_events_source_input_id ON events(source_input_id);

CREATE TABLE IF NOT EXISTS claims (
	id TEXT PRIMARY KEY,
	text TEXT NOT NULL,
	type TEXT NOT NULL,
	confidence REAL NOT NULL,
	status TEXT NOT NULL,
	created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS claim_evidence (
	claim_id TEXT NOT NULL,
	event_id TEXT NOT NULL,
	PRIMARY KEY (claim_id, event_id),
	FOREIGN KEY (claim_id) REFERENCES claims(id)
);

CREATE INDEX IF NOT EXISTS idx_claim_evidence_event_id ON claim_evidence(event_id);

CREATE TABLE IF NOT EXISTS relationships (
	id TEXT PRIMARY KEY,
	type TEXT NOT NULL,
	from_claim_id TEXT NOT NULL,
	to_claim_id TEXT NOT NULL,
	created_at TEXT NOT NULL,
	FOREIGN KEY (from_claim_id) REFERENCES claims(id),
	FOREIGN KEY (to_claim_id) REFERENCES claims(id)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_relationships_unique_edge
	ON relationships(type, from_claim_id, to_claim_id);
CREATE INDEX IF NOT EXISTS idx_relationships_from_claim ON relationships(from_claim_id);
CREATE INDEX IF NOT EXISTS idx_relationships_to_claim ON relationships(to_claim_id);

CREATE TABLE IF NOT EXISTS compilation_jobs (
	id TEXT PRIMARY KEY,
	kind TEXT NOT NULL,
	status TEXT NOT NULL,
	scope_json TEXT NOT NULL,
	started_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	error TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_compilation_jobs_kind ON compilation_jobs(kind);
CREATE INDEX IF NOT EXISTS idx_compilation_jobs_status ON compilation_jobs(status);
`

	if _, err := db.Exec(schema); err != nil {
		return fmt.Errorf("ensure events schema: %w", err)
	}

	return nil
}
