# Changelog

All notable changes to Mnemos are documented here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Releases are tagged and published via GoReleaser; this file is the human-readable summary.

## [Unreleased]

Phase 1-8 follow-ups since the v0.12.0 gRPC release.

### Added
- **Phase 1 — causal edges** (`feat(relate)`): `causes`, `caused_by`, `action_of`, `outcome_of`, `validates`, `refutes`, `derived_from` extend the relationship graph beyond logical agreement. `relate.DetectCausal` infers from event-time + shared-entity signals; optional `relate.DetectCausalLLM` augments borderline pairs.
- **Phase 2 — actions + outcomes**: `mnemos action record` / `mnemos outcome record`; Prometheus pull adapter (`internal/adapters/outcomes/prometheus.go`) emits Outcomes from PromQL instant queries.
- **Phase 3 — lessons synthesis**: `mnemos synthesize` clusters action→outcome chains into validated Lessons; `mnemos lessons` lists them.
- **Phase 4 — temporal hardening**: per-claim `last_verified`, `verify_count`, `half_life_days`; `mnemos verify` re-confirms; `Answer.StaleClaimIDs` surfaces decay below the trust floor.
- **Phase 5 — decisions**: `mnemos decision record/list/show/attach-outcome` audits agent reasoning with belief claims, alternatives, risk level, and observed outcomes.
- **Phase 6 — playbooks**: `mnemos playbook synthesize/list/show/<trigger>` derives Praxis-ready response steps from Lesson clusters.
- **Phase 7 — markdown round-trip + history**: `mnemos export/import` round-trips Lessons + Playbooks to YAML-frontmatter markdown; `mnemos history` lists snapshots from system-versioned `*_versions` tables.
- **Phase 8 — multi-tenant scope**: `Scope{Service, Env, Team}` on Claims, Lessons, Decisions, Playbooks; `mnemos query --service X --env prod --team Y` filters answers.
- **Polymorphic cross-entity edges**: `entity_relationships` table + `internal/autoedge` package auto-fires `action_of`/`outcome_of`/`validates`/`refutes`/`derived_from` edges. Documented in [ADR-0002](docs/adr/0002-cross-entity-edges-and-outcome-pull-adapters.md).
- **gRPC API expansion**: `feat(grpc): expand API to Phase 2-7 entities` — `List*/Append*` for Actions, Outcomes, Lessons, Decisions, Playbooks, EntityRelationships alongside the v0.12 core surface.
- **API documentation**: `api/openapi.yaml` covers the full HTTP registry surface; `proto/mnemos/v1/mnemos.proto` is the gRPC schema. OpenAPI `bearerAuth` scheme corrected to `JWT` (HS256) — same verifier as gRPC; `MNEMOS_REGISTRY_TOKEN` clarified as client-side only.
- **Glob-pattern run scopes** — `auth.Claims.AllowsRun` accepts `*` (wildcard), exact match, and shell-glob patterns (`prod-*`, `nightly-?-2026`, `release/[0-9]*`). Patterns that fail `path.Match` fall back to exact compare so a malformed glob can't grant unintended access.
- **Agent quotas** — `domain.AgentQuota` (rolling-window write count + token cap). `auth.QuotaTracker` enforces in-memory; `Charge` returns `ErrQuotaExceeded` on overflow. Counters reset on process restart (durable variant deferred).
- **Federated agent sync** — `AgentRepository.Upsert(batch)` shipped on every backend (sqlite + memory + postgres + mysql + libsql via sqlite). Registries can mirror peers' agents alongside events / claims / relationships / embeddings.
- **Bias detection** — `internal/bias` package ships four explainable indicators with operator-tunable thresholds: source concentration, polarity skew, temporal clustering, single-source-of-truth pathology. `Analyse(input, thresholds)` returns a `bias.Report` with per-finding explanation. No auto-action — operators decide what to do.
- **sqlc coverage parity** — moved `users`, `revoked_tokens`, `agents`, and `entity_relationships` from hand-written SQL to sqlc-generated queries (see `sql/sqlite/query/`). Every fixed-shape sqlite query now flows through `internal/store/sqlite/sqlcgen`. Dynamic-filter queries (claim/event search) keep raw SQL because sqlc doesn't model them well.
- **Server-side TLS + mTLS** — `MNEMOS_TLS_CERT_FILE` + `MNEMOS_TLS_KEY_FILE` enable TLS on the HTTP registry and gRPC server. `MNEMOS_MTLS_CLIENT_CA_FILE` upgrades to mutual TLS. Both transports share the same cert; helpers in `cmd/mnemos/serve_tls.go`.
- **Dual-key JWT verifier** — `auth.NewVerifierWithPrevious(active, previous, revoked)` accepts tokens signed under either active or previous secret, supporting zero-downtime key rotation. Resolution: `MNEMOS_JWT_PREV_SECRET` env, then `<auth-dir>/jwt-secret.previous` file. `serve` wires this automatically; rotation is a copy-and-restart procedure documented in `SECURITY.md`.
- **In-tree security baseline** — `make nox-scan` invokes [`nox`](https://github.com/felixgeelhaar/nox) v0.7.0; `findings.json` baseline committed in-tree (3 856 findings, all `Status: "baselined"`). New scans diff against the baseline; unbaselined findings fail CI. Categories tracked in `SECURITY.md`.
- `SECURITY.md` — auth surfaces, threat model, container hardening, secret management, known gaps.
- `CHANGELOG.md`.

### Fixed
- `fix(mysql)`: backtick-quote `trigger` reserved word in schema and DML.
- `fix(mysql)`: tolerate vanilla MySQL rejecting `ALTER ... IF NOT EXISTS`.
- `fix(sqlite)`: wire Phase 2-6 repositories into the SQLite Conn.

### Changed
- `README.md`, `CLAUDE.md`, `AGENTS.md`, `TDD.md`, `Product Brief.md` updated to cover Phase 1-8 surfaces, multi-backend storage, and gRPC.
- `docs/adr/005-scripted-llm-extractor.md` (Mnemos has no equivalent — this entry intentionally absent).

## [0.12.0] — 2026-04

- **gRPC API server** alongside HTTP REST. Schema in `proto/mnemos/v1/mnemos.proto`. Auth via the existing JWT verifier (`MNEMOS_JWT_SECRET` / `MNEMOS_AUTH_DIR`).

## [0.11.0] — 2026-04

- **Phase 7 legacy cleanup**: dropped `sqlite.Open`; ported all tests to `store.Open`.
- **Multi-backend storage** ([ADR 0001](docs/adr/0001-multi-backend-storage.md)) GA. Providers: `sqlite://` (default), `memory://`, `postgres://`/`postgresql://`, `mysql://`/`mariadb://`, `libsql://`. Postgres-wire-compatible engines (CockroachDB, YugabyteDB, Neon, Crunchy Bridge, TimescaleDB, AlloyDB Omni) and MySQL-wire-compatible engines (PlanetScale, TiDB, Vitess) work through native providers unchanged.
- Namespace isolation across all backends.

## [0.10.1] — 2026-03

- Retrieval-quality eval suite + v0.10 baseline.

## [0.10.0] — 2026-03

- **Hybrid retrieval** (Obvious Choice, part 2): BM25 over FTS5 keyword index + cosine over embeddings. Equal-weighted, max-normalised composite. Auto-creates / backfills `events_fts` and `claims_fts`.

## [0.9.0] — 2026-03

- **Entity layer** (Obvious Choice, part 1): canonicalised noun-phrases ("Felix Geelhaar", "Acme", "PostgreSQL", ...) as first-class entity nodes. New commands: `mnemos entities list/show/merge`, `mnemos extract-entities`, `mnemos query --entity`.

## [0.8.0] — 2026-03

- **Temporal validity** (Living Truth): `valid_from` / `valid_to` per claim; default queries hide superseded claims; `mnemos query --at YYYY-MM-DD` for point-in-time answers; `mnemos resolve <new> --supersedes <old>` to close one claim's interval when a new one takes its place.

## [0.7.0] — 2026-02

- **Trust scoring**: `trust = confidence × corroboration × freshness`. Auto-recomputed after every `process` run; `mnemos recompute-trust` for manual rebuild; `mnemos query --min-trust X` filters; `mnemos metrics` reports `avg_trust` and `low_trust_count`.
- Semantic dedupe via `mnemos dedup`.

## [0.6.1] — 2026-02

- v0.5 → v0.6 schema migration; junk/dedup filters; ops UX polish.

## [0.6.0] — 2026-02

- Local-LLM (Ollama) UX sweep: timeouts, reasoning-block tolerance, JSON-mode forgiveness, ops commands.

## [0.5.0 and earlier]

Tagged on GitHub. Notable themes: rule-based + LLM-powered extraction, contradiction detection, MCP server, CLI, embeddings, registry push/pull, JWT auth, claim lifecycle.
