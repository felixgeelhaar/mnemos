package sqlite

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// Open creates or opens a SQLite database at the given path, ensuring the
// parent directory and schema exist.
func Open(path string) (*sql.DB, error) {
	if path == "" {
		return nil, fmt.Errorf("database path is required")
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}

	// Reliability PRAGMAs are passed via the DSN so they apply to
	// every connection the pool opens, not just the first one.
	// Setting them with db.Exec only affects whichever pooled conn
	// happened to handle that statement — every subsequent goroutine
	// might land on a fresh conn without WAL or busy_timeout, which
	// breaks concurrent writes (we hit this in the auth stress test).
	//
	//   foreign_keys=ON: schema-level FK constraints aren't enforced
	//     without this — SQLite's default is OFF for back-compat.
	//   journal_mode=WAL: lets readers and a single writer coexist;
	//     without it the whole file serialises and concurrent token
	//     issuance fails with SQLITE_BUSY.
	//   busy_timeout=5000: wait up to 5s for the writer lock before
	//     returning SQLITE_BUSY. Friendlier than immediate failure
	//     for short bursts of contention.
	dsn := path + "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
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
	run_id TEXT NOT NULL,
	schema_version TEXT NOT NULL,
	content TEXT NOT NULL,
	source_input_id TEXT NOT NULL,
	timestamp TEXT NOT NULL,
	metadata_json TEXT NOT NULL,
	ingested_at TEXT NOT NULL,
	created_by TEXT NOT NULL DEFAULT '<system>'
);

CREATE INDEX IF NOT EXISTS idx_events_timestamp ON events(timestamp);
CREATE INDEX IF NOT EXISTS idx_events_source_input_id ON events(source_input_id);
CREATE INDEX IF NOT EXISTS idx_events_run_id ON events(run_id);

CREATE TABLE IF NOT EXISTS claims (
	id TEXT PRIMARY KEY,
	text TEXT NOT NULL,
	type TEXT NOT NULL,
	confidence REAL NOT NULL,
	status TEXT NOT NULL,
	created_at TEXT NOT NULL,
	created_by TEXT NOT NULL DEFAULT '<system>'
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
	created_by TEXT NOT NULL DEFAULT '<system>',
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

CREATE TABLE IF NOT EXISTS claim_status_history (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	claim_id TEXT NOT NULL,
	from_status TEXT NOT NULL,
	to_status TEXT NOT NULL,
	changed_at TEXT NOT NULL,
	reason TEXT NOT NULL,
	changed_by TEXT NOT NULL DEFAULT '<system>',
	FOREIGN KEY (claim_id) REFERENCES claims(id)
);

CREATE INDEX IF NOT EXISTS idx_claim_status_history_claim_id ON claim_status_history(claim_id);
CREATE INDEX IF NOT EXISTS idx_claim_status_history_changed_at ON claim_status_history(changed_at);

CREATE TABLE IF NOT EXISTS embeddings (
	entity_id TEXT NOT NULL,
	entity_type TEXT NOT NULL,
	vector BLOB NOT NULL,
	model TEXT NOT NULL,
	dimensions INTEGER NOT NULL,
	created_at TEXT NOT NULL,
	created_by TEXT NOT NULL DEFAULT '<system>',
	PRIMARY KEY (entity_id, entity_type)
);

CREATE INDEX IF NOT EXISTS idx_embeddings_entity_type ON embeddings(entity_type);

CREATE TABLE IF NOT EXISTS users (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	email TEXT NOT NULL UNIQUE,
	status TEXT NOT NULL DEFAULT 'active',
	scopes_json TEXT NOT NULL DEFAULT '["*"]',
	created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_users_status ON users(status);

CREATE TABLE IF NOT EXISTS revoked_tokens (
	jti TEXT PRIMARY KEY,
	revoked_at TEXT NOT NULL,
	expires_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_revoked_tokens_expires_at ON revoked_tokens(expires_at);

CREATE TABLE IF NOT EXISTS agents (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	owner_id TEXT NOT NULL,
	scopes_json TEXT NOT NULL DEFAULT '[]',
	allowed_runs_json TEXT NOT NULL DEFAULT '[]',
	status TEXT NOT NULL DEFAULT 'active',
	created_at TEXT NOT NULL,
	FOREIGN KEY (owner_id) REFERENCES users(id)
);

CREATE INDEX IF NOT EXISTS idx_agents_owner_id ON agents(owner_id);
CREATE INDEX IF NOT EXISTS idx_agents_status ON agents(status);
`

	if _, err := db.Exec(schema); err != nil {
		return fmt.Errorf("ensure schema: %w", err)
	}

	if err := migrate(db); err != nil {
		return fmt.Errorf("schema migration: %w", err)
	}

	return nil
}

// currentSchemaVersion is the schema generation this binary expects.
// Bump whenever a column or table is added; pair the bump with a step
// in addMissingColumns so existing DBs upgrade in place.
const currentSchemaVersion = 1

// addMissingColumn declares one defensive column-add. Each entry is
// idempotent: if the column already exists in the table we skip it,
// so re-running the migration on an up-to-date DB is a no-op.
type addMissingColumn struct {
	table  string
	column string
	def    string // full column definition appended after ADD COLUMN
}

// v1Columns is the set of columns introduced after the v0.5 baseline.
// They are added defensively because v0.5 DBs created the parent
// tables without these columns; CREATE TABLE IF NOT EXISTS in the
// bootstrap above doesn't touch existing tables, so a v0.5→v0.6+
// upgrade would otherwise fail with the cryptic
// "table events has no column named created_by" on the next write.
var v1Columns = []addMissingColumn{
	{"events", "created_by", "TEXT NOT NULL DEFAULT '<system>'"},
	{"claims", "created_by", "TEXT NOT NULL DEFAULT '<system>'"},
	{"relationships", "created_by", "TEXT NOT NULL DEFAULT '<system>'"},
	{"claim_status_history", "changed_by", "TEXT NOT NULL DEFAULT '<system>'"},
	{"embeddings", "created_by", "TEXT NOT NULL DEFAULT '<system>'"},
}

// migrate applies every column-add this binary knows about to bring an
// older DB up to currentSchemaVersion. It is invoked after ensureSchema
// runs the CREATE TABLE statements; new tables added to the schema
// take care of themselves via CREATE TABLE IF NOT EXISTS, but added
// columns on pre-existing tables need ALTER TABLE.
//
// Strategy: for every (table, column) the binary expects, query
// PRAGMA table_info and ALTER TABLE ADD COLUMN only if missing. This
// avoids tracking a brittle linear sequence of migrations — the only
// state we need is "what columns does this DB have right now". After
// every ALTER succeeds we bump PRAGMA user_version so future binaries
// can spot a baseline and skip the column probes when possible.
func migrate(db *sql.DB) error {
	var userVersion int
	if err := db.QueryRow("PRAGMA user_version").Scan(&userVersion); err != nil {
		return fmt.Errorf("read user_version: %w", err)
	}

	if userVersion >= currentSchemaVersion {
		return nil
	}

	for _, c := range v1Columns {
		has, err := columnExists(db, c.table, c.column)
		if err != nil {
			return fmt.Errorf("inspect %s.%s: %w", c.table, c.column, err)
		}
		if has {
			continue
		}
		stmt := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", c.table, c.column, c.def)
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("add %s.%s: %w", c.table, c.column, err)
		}
	}

	if _, err := db.Exec(fmt.Sprintf("PRAGMA user_version = %d", currentSchemaVersion)); err != nil {
		return fmt.Errorf("set user_version: %w", err)
	}
	return nil
}

// columnExists asks SQLite which columns a table currently has.
// Cheap (PRAGMA table_info is O(columns)) and the only reliable way
// to keep the migration idempotent across SQLite versions that don't
// support ALTER TABLE ... ADD COLUMN IF NOT EXISTS.
func columnExists(db *sql.DB, table, column string) (bool, error) {
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return false, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var (
			cid     int
			name    string
			ctype   string
			notnull int
			dflt    sql.NullString
			pk      int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}
