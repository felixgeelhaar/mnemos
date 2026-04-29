
## Multi-Format Input Service

Support ingestion of raw text and file-based inputs (TXT, MD, JSON, CSV), preserving source metadata and timestamps for downstream traceability.

---

## Parser and Event Normalization

Normalize heterogeneous inputs into immutable events with schema versioning and source linkage, forming the canonical Input -> Event transformation.

---

## Append-Only Event Store

Implement a local-first SQLite-backed append-only event store with indexes for timestamp and source input ID to support reliable replay and query foundations.

---

## Claim Extraction Engine

Derive structured claims (fact, hypothesis, decision) from events, enforce validation rules, and map each claim to at least one evidence event.

---

## Relationship Detection Engine

Detect support and contradiction relationships between claims within scoped comparisons and persist relationship edges for truth evolution analysis.

---

## CLI Query Interface

Provide a CLI query interface that returns structured answers with claims, surfaced contradictions, and an evidence-backed timeline.

---

## Workflow Orchestration and Observability

Track ingestion/extraction/relationship jobs through explicit workflow states with structured logging, retries, timeouts, and failure handling.

---

## Claim Extraction Heuristics v2

Improve claim extraction quality by splitting event text into sentence-level candidate claims, deduplicating near-identical claims, and applying stronger heuristic scoring for fact/decision/hypothesis classification while preserving evidence traceability.

---

## SQLC Typed Data Access

Adopt sqlc for SQLite data-access to replace handwritten SQL in repositories with generated, typed query methods; include schema/queries layout, generation command, and initial migration of core event and claim reads/writes.

---

## SQLC Migration Phase 2

Complete sqlc adoption by migrating relationship and compilation job repositories to generated typed queries, reducing handwritten SQL and unifying data access patterns.

---

## One-Step Process Command

Add a CLI `process` command that runs ingest, extract, and relate in one workflow for either file input or raw text, then prints IDs/counts so users can immediately run query without manual event ID lookups.

---

## Run-Scoped Querying

Add run-scoped processing and querying so each process/ingest flow emits a run_id and query can restrict retrieval to a specific run, preventing cross-run context contamination in answers.

---

## LLM-Powered Claim Extraction

Replace/augment rule-based extraction with LLM-powered claim extraction. Supports Anthropic, OpenAI, Google Gemini, Ollama, and any OpenAI-compatible provider. Activated via --llm flag with env var configuration. Falls back to rule-based extraction on LLM failure.

---

## Markdown Preprocessing and Structural Noise Filters

Strip markdown formatting (bold, italic, strikethrough, links, bullets, checkboxes, headers) before claim extraction, and reject structural noise like label-value metadata rows, pipe-separated table rows, JSON fragments, short title-case headers, and email salutations. Raises extraction F1 on real-world documents from 79.9% to 87.8%.

---

## Grounded LLM Answer Generation

When an LLM provider is configured, synthesize query answers from retrieved claims with inline citations instead of returning fixed templates. Falls back to template answers on LLM failure so offline/zero-config queries still work.

---

## Claim-Level Embeddings and Cosine Reranking

Generate vector embeddings for events and claims, store as little-endian float32 BLOBs in SQLite, and rerank query results by cosine similarity against the question embedding. Falls back to token-overlap ranking when no embedding provider is configured.

---

## Project-Scoped Database with Init Subcommand

Add `mnemos init` to create a `.mnemos/` directory in the working directory. Database resolution walks up from CWD looking for `.mnemos/mnemos.db`, falling back to the XDG global default. MNEMOS_DB_PATH still wins outright. Discovery stops at the user's HOME to avoid adopting an unrelated parent project's DB.

---

## MCP Project Document Auto-Ingest

When the MCP server starts inside a project (`.mnemos/` exists), bulk-ingest standard project documents — README, PRD, CHANGELOG, Roadmap, CLAUDE.md, ARCHITECTURE — plus the top level of `docs/` and recursive ADR conventions. Deduped via SQLite json_extract on event metadata source paths.

---

## File Watch MCP Tool

Expose a `watch_file` MCP tool that registers a path for re-ingestion when its content changes. Polling-based (5s, sha256 content comparison), in-memory state. Re-ingest reuses the same extract+relate pipeline as auto-ingest so agent edits flow into queryable claims with no manual step.

---

## Knowledge Browsing MCP Tools

Expose `list_claims`, `list_decisions`, and `list_contradictions` MCP tools for paginated read-only browsing of the knowledge base. Supports type/status filtering, pagination with sensible defaults, and claim-text hydration on the contradiction list via SQL JOIN.

---

## Git Commit Context Auto-Ingest

When the project root has a `.git` directory, read the most recent 50 commits via `git log` on MCP startup and persist each as an event with metadata (SHA, author, committed_at, subject). Commits flow through extract+relate so subjects and bodies become queryable claims. Deduped by SHA. Explicit `ingest_git_log` MCP tool exposes deeper history via limit/since.

---

## Merged PR Auto-Ingest via gh CLI

When the gh CLI is installed and authenticated for github.com, auto-ingest the 20 most recent merged pull requests on MCP startup. PR titles and bodies flow through extract+relate so they become queryable claims. Deduped by PR number. Explicit `ingest_git_prs` MCP tool exposes configurable limit for deeper history. Silent no-op when gh is missing, unauth, or the repo is not on GitHub.

---

## Knowledge Registry Server (mnemos serve)

Add a `mnemos serve` subcommand that starts an HTTP API registry server backed by the same SQLite schema as the local client. The registry is the "remote origin" to each local project's "local repo" — users can push knowledge to a shared registry and pull from it at query time. Core endpoints cover event append, claim lookup, and relationship reads. Runs standalone or in a container.

---

## Push/Pull Synchronization with Remote Registry

Add `mnemos registry connect <url>` to wire a local project to a remote registry, plus push and pull semantics that mirror git's model. Push sends local events/claims/relationships to the registry; pull fetches remote knowledge and merges by source hash + run_id. Automatic sync hooks into process/query when enabled. Depends on Knowledge Registry Server.

---

## Cross-Project Query Federation

Extend the query engine to federate across the local database and one or more connected registries. Results include source provenance (local vs registry-name) so claims can be weighed by trust. Ranks cross-project results using the same cosine + token-overlap logic as local queries. Depends on Push/Pull Synchronization.

---

## Registry Namespace and Scope Isolation

Add namespace/scope primitives to the registry (team, org, project) so multiple tenants share a single server without leaking claims across boundaries. Push and pull operations carry a namespace context. Query federation respects the caller's accessible namespaces. Depends on Knowledge Registry Server.

---

## GraphRAG Multi-Hop Query Support

Extend the query engine to traverse relationship edges at query time, enabling multi-hop questions like "what decisions contradict the current approach to auth?" that require walking from a seed claim through supports/contradicts chains. Reports the traversal path as part of the evidence trail.

---

## Governance and Compliance Layer

Add governance features to Mnemos as an enterprise-ready evidence layer: bias detection on claim sources, audit trails tracking who/when/why for every event and claim change, retention policies for aging out or archiving old events, and compliance export formats (CSV/JSON) for regulated environments.

---

## Enterprise Integration Adapters

Adapters that ingest content from common team knowledge surfaces: Slack threads, Microsoft Teams channels, Jira tickets, and Notion pages. Each adapter maps the source's identifiers into Mnemos event metadata so traceability survives the round trip. Adapters run as periodic jobs rather than real-time listeners to avoid webhook complexity.

---

## Web Interface on Registry API

Build a web UI that sits on top of the Phase 2B registry HTTP API — not a standalone app. Surfaces query, browsing, contradictions, and timeline views. Deferred until the registry API is proven with the CLI and MCP surfaces, per the "API before UI" principle established in the Phase 2A rewrite.

---

## Multi-Backend Storage Foundation

Replace the hard-wired SQLite construction with a pluggable storage layer per ADR 0001. Phase 1a: add `internal/store` driver registry with URL-scheme dispatch (`store.Open(ctx, dsn)`), repackage the existing SQLite implementation behind the registry as a `sqlite://` provider, ship a `memory://` in-process provider implementing the same `ports.*` interfaces (forces port purity, unblocks fast tests + Nous embedding), and widen `ports.EventRepository`/`ports.ClaimRepository` to the union of methods callers actually reach for. Subsequent phases (separate features): migrate cmd/mnemos and internal/pipeline call sites onto `store.Open`, add `MNEMOS_DB_URL`, add the Postgres provider with `pgvector`/`tsvector` and namespace isolation. Source: docs/adr/0001-multi-backend-storage.md.

---

## Multi-Backend Storage MNEMOS_DB_URL

Phase 1b of ADR 0001: introduce a `MNEMOS_DB_URL` environment variable that takes precedence over `MNEMOS_DB_PATH` and is the canonical way to point Mnemos at any registered storage backend. Add a `resolveDSN()` helper in cmd/mnemos that returns the URL when set, otherwise wraps the legacy resolved file path as `sqlite:///<path>`. Add an `openConn(ctx)` helper that calls `store.Open(ctx, resolveDSN())` so future call sites can switch by replacing two lines. Migrate the `mnemos doctor` deep-probe to use `openConn` as the proof-of-life consumer (it already exercises the deepest paths and is a natural smoke test for DSN resolution). Update help text + CLAUDE.md so MNEMOS_DB_URL appears alongside MNEMOS_DB_PATH. Mass call-site migration stays in a separate later phase.

---

## Multi-Backend Storage cmd/mnemos call site migration

Phase 1c of ADR 0001: migrate every production cmd/mnemos call site that does sqlite.Open(resolveDBPath()) onto a registry-mediated open. Add an openDB(ctx) helper that returns (*sql.DB, *store.Conn, error) — most cmd/mnemos surfaces still need the raw *sql.DB for entity/job repos and raw SQL paginations that aren't on the ports yet. Migration is mechanical: replace the open call, defer conn.Close instead of closeDB(db). Tests using fixed temp paths via sqlite.Open(filepath.Join(t.TempDir(), ...)) stay untouched — they're not on the resolveDBPath path. Result: every operator-facing CLI/MCP/server command honours MNEMOS_DB_URL (including future SQLite DSN options like ?busy_timeout=). Out of scope: lifting EntityRepository/CompilationJobRepository into ports (separate later phase), refactoring pipeline functions that take *sql.DB (separate later phase), Postgres provider (separate later phase).

---

## Multi-Backend Storage port lift Entity Job

Phase 2a of ADR 0001: lift EntityRepository and CompilationJobRepository from concrete SQLite types into ports interfaces, populate them on the Conn struct, and add memory provider implementations of both. After this phase, callers can use conn.Entities and conn.Jobs without reaching into the SQLite package, and memory:// can persist canonicalised entities and compilation_jobs the same way SQLite does. Pipeline refactor (PersistArtifacts / MaterializeEntities switching from *sql.DB to *store.Conn) and Postgres provider remain separate later phases.

---

## Multi-Backend Storage pipeline port refactor

Phase 2b of ADR 0001: refactor pipeline.PersistArtifacts, pipeline.MaterializeEntities, pipeline.GenerateEmbeddings, pipeline.GenerateClaimEmbeddings to take *store.Conn instead of *sql.DB. Replace the cross-table SQLite transaction inside PersistArtifacts with sequential port-typed repository calls; per-table writes are still atomic in each backend. Trust scoring becomes optional via ports.TrustScorer type assertion. Update every caller in cmd/mnemos. Status-history attribution per claim is preserved by grouping claims by CreatedBy and calling UpsertWithReasonAs once per group. After this phase memory:// can run end-to-end ingest/process/embed paths. semantic_dedupe.go remains SQLite-specific (raw SQL probes) and is out of scope.

---

## Multi-Backend Storage call site cleanup

Phase 2c of ADR 0001: drop the remaining sqlite.NewXxxRepository(db) constructions across cmd/mnemos in favor of conn.Xxx port-typed access. Trust scoring callers use ports.TrustScorer assertion. semantic_dedupe.go stays SQLite-specific (raw SQL probes) until a separate dedicated phase. Goal: cmd/mnemos imports internal/store/sqlite ONLY for blank-import provider registration after this phase.

---

## Multi-Backend Storage Postgres provider

Phase 3 of ADR 0001: add internal/store/postgres/ provider implementing every port interface. Uses pgx/v5 + database/sql. Translates ?namespace= into CREATE SCHEMA IF NOT EXISTS + SET search_path. pgvector for VectorSearcher capability, tsvector for TextSearcher. Migrations live alongside the provider and run on Open. CI gains a Postgres job (docker-compose). Mirrors the contract validated by SQLite + memory in Phases 1-2.

---

## Multi-Backend Storage backend-agnostic serve and dedupe

Phase 4a of ADR 0001: migrate the last two SQLite-bound surfaces in mnemos to ports — `cmd/mnemos/serve.go` HTTP handlers (events/claims/relationships/embeddings/metrics) and `internal/pipeline/semantic_dedupe.go` (PlanSemanticDedupe + ApplySemanticDedupe). Use port-typed repositories where they exist; keep raw SQL paths only when no port-level alternative is available, and clearly mark them as SQLite-specific. After this phase no production cmd/mnemos or pipeline code reaches for sqlite.NewXxxRepository directly outside its own package.

---

## Multi-Backend Storage MySQL MariaDB provider

Phase 4b of ADR 0001: add internal/store/mysql/ provider implementing every port interface, registered for `mysql://` and `mariadb://` schemes. Uses github.com/go-sql-driver/mysql + database/sql. Per ADR 0001 §3, MySQL has no per-tenant schemas — namespace translates to "use a separate database (CREATE DATABASE IF NOT EXISTS <ns>; USE <ns>)". Schema SQL adapted: jsonb → JSON, bytea → LONGBLOB, bigserial → BIGINT AUTO_INCREMENT, timestamptz → DATETIME(6), now() → CURRENT_TIMESTAMP. Integration tests gated on TEST_MYSQL_DSN. MariaDB shares the wire protocol so the same provider serves both.

---

## Multi-Backend Storage CI integration job

Phase 5 of ADR 0001: lock in the Postgres and MySQL integration tests with a CI job. Add a `database-providers` GitHub Actions job that runs alongside the existing `Build & Test` job, spinning up postgres:16 + mysql:8 services and running `go test -race -count=1 ./internal/store/postgres/ ./internal/store/mysql/` with TEST_POSTGRES_DSN + TEST_MYSQL_DSN populated. Add a `make test-integration` target so developers can run the same suite locally.

---

## Multi-Backend Storage libSQL provider

Phase 6 of ADR 0001: ship a libSQL/Turso provider for cloud-replicated and edge-deployable SQLite-compatible storage. Since libSQL is wire-compatible with SQLite, the provider reuses the existing sql/sqlite/schema.sql and the existing SQLite repository implementations — only the registration, DSN parsing, and sql.Open driver name change. Pure-Go driver (github.com/tursodatabase/libsql-client-go) keeps CGO_ENABLED=0. Supports both remote DSNs (libsql://my-db.turso.io?authToken=xyz) and local file DSNs (libsql:///tmp/test.db). Namespace param is ignored — each Turso database is its own tenant boundary, like plain SQLite. Plus a CLAUDE.md note that the existing postgres:// provider already serves any Postgres wire-protocol-compatible engine (CockroachDB, Yugabyte, Neon, Crunchy Bridge), no extra code needed.

---

## Multi-Backend Storage everything-on-ports legacy cleanup

Phase 7 of ADR 0001: every production surface is backend-agnostic; no legacy. Add the port methods needed to express semantic dedupe and the serve.go HTTP handlers without raw SQL — Claims.RepointEvidence, Claims.DeleteCascade, Relationships.RepointEndpoint, Relationships.DeleteByClaim, Embeddings.Delete, plus paginated list methods on Events/Claims/Relationships/Embeddings. Implement in every native provider (sqlite, memory, postgres, mysql; libsql inherits sqlite). Migrate pipeline.ApplySemanticDedupe and the serve.go handlers to ports. Then drop legacy: MNEMOS_DB_PATH env var, openDB helper (keep openConn only), sqlite.Open public function, any remaining sqlite-only call sites in cmd/mnemos. Pre-launch posture — no backwards-compat stubs.

---

## gRPC API Server

Add a gRPC API surface to Mnemos alongside the existing HTTP REST API. Define proto schemas for events, claims, relationships, and embeddings that mirror the HTTP API contract. Generate Go code with protoc-gen-go-grpc. Implement a gRPC server that reuses the existing port-typed repositories and bearer-token auth from serve.go. Wire into the CLI via `mnemos serve --grpc-port` or `mnemos grpc`. This enables high-performance service-to-service communication for the cognitive stack (Chronos, Praxis, Nous) and supports streaming for large dataset operations.

---
