package sqlite

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// TestMigrate_AddsCreatedByOnLegacyV05Schema reproduces the v0.5.1 →
// v0.6.0 upgrade path that issue #20 surfaced: an existing DB whose
// events/claims/relationships/embeddings tables predate the auth
// columns. After Open() runs migrate(), every previously-missing
// column should be present and an INSERT exercising created_by must
// succeed.
func TestMigrate_AddsCreatedByOnLegacyV05Schema(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "legacy.db")

	// Hand-build a v0.5-shaped DB without created_by / changed_by.
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open raw: %v", err)
	}
	legacy := `
CREATE TABLE events (
  id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL,
  schema_version TEXT NOT NULL,
  content TEXT NOT NULL,
  source_input_id TEXT NOT NULL,
  timestamp TEXT NOT NULL,
  metadata_json TEXT NOT NULL,
  ingested_at TEXT NOT NULL
);
CREATE TABLE claims (
  id TEXT PRIMARY KEY,
  text TEXT NOT NULL,
  type TEXT NOT NULL,
  confidence REAL NOT NULL,
  status TEXT NOT NULL,
  created_at TEXT NOT NULL
);
CREATE TABLE relationships (
  id TEXT PRIMARY KEY,
  type TEXT NOT NULL,
  from_claim_id TEXT NOT NULL,
  to_claim_id TEXT NOT NULL,
  created_at TEXT NOT NULL
);
CREATE TABLE embeddings (
  entity_id TEXT NOT NULL,
  entity_type TEXT NOT NULL,
  vector BLOB NOT NULL,
  model TEXT NOT NULL,
  dimensions INTEGER NOT NULL,
  created_at TEXT NOT NULL,
  PRIMARY KEY (entity_id, entity_type)
);
CREATE TABLE claim_status_history (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  claim_id TEXT NOT NULL,
  from_status TEXT NOT NULL,
  to_status TEXT NOT NULL,
  changed_at TEXT NOT NULL,
  reason TEXT NOT NULL
);
`
	if _, err := raw.Exec(legacy); err != nil {
		t.Fatalf("seed legacy schema: %v", err)
	}
	if err := raw.Close(); err != nil {
		t.Fatalf("close legacy: %v", err)
	}

	// Open via the production path, which should migrate.
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open after legacy seed: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	for _, c := range v1Columns {
		has, err := columnExists(db, c.table, c.column)
		if err != nil {
			t.Fatalf("columnExists(%s,%s): %v", c.table, c.column, err)
		}
		if !has {
			t.Fatalf("after migrate, %s.%s still missing", c.table, c.column)
		}
	}

	// The actual symptom from #20: a write that touches created_by
	// must succeed. Use the same column set the production INSERT uses.
	if _, err := db.Exec(
		`INSERT INTO events (id, run_id, schema_version, content, source_input_id, timestamp, metadata_json, ingested_at, created_by)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"ev1", "r", "v1", "hi", "src1", "2026-04-26T00:00:00Z", "{}", "2026-04-26T00:00:00Z", "alice",
	); err != nil {
		t.Fatalf("post-migrate INSERT failed (the original #20 symptom): %v", err)
	}

	var v int
	if err := db.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		t.Fatalf("read user_version: %v", err)
	}
	if v != currentSchemaVersion {
		t.Fatalf("user_version = %d, want %d", v, currentSchemaVersion)
	}
}

func TestMigrate_IsIdempotentOnFreshDB(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "fresh.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// Re-running migrate must be a no-op.
	if err := migrate(db); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	for _, c := range v1Columns {
		has, err := columnExists(db, c.table, c.column)
		if err != nil || !has {
			t.Fatalf("expected %s.%s to exist on fresh DB; has=%v err=%v", c.table, c.column, has, err)
		}
	}
}
