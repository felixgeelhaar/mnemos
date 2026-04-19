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
  status TEXT NOT NULL DEFAULT 'active',
  created_at TEXT NOT NULL,
  FOREIGN KEY (owner_id) REFERENCES users(id)
);

CREATE INDEX IF NOT EXISTS idx_agents_owner_id ON agents(owner_id);
CREATE INDEX IF NOT EXISTS idx_agents_status ON agents(status);
