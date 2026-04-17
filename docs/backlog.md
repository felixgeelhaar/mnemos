
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
