# Mnemos Roadmap

*Core Principle: Validate each phase before expanding.*

---

## Phase 1: Developer Primitive (COMPLETE)

**Status:** Complete (v0.2)
**Goal:** Establish Mnemos as a local-first, open-source knowledge engine for AI agents and developer tooling.

### Milestones

- [x] Core domain model (Event, Claim, Relationship)
- [x] Multi-format input ingestion (TXT, MD, JSON, CSV, raw text)
- [x] Append-only SQLite event store
- [x] Claim extraction engine with evidence mapping
- [x] Relationship detection (supports/contradicts)
- [x] CLI query interface with BM25 ranking
- [x] Workflow orchestration with structured logs
- [x] SQLC typed data access
- [x] 102 eval test cases with precision/recall metrics
- [x] LLM-powered extraction with few-shot prompt
- [x] Embeddings for semantic search (event + claim level)
- [x] Query-time grounded generation (--llm)
- [x] Incremental relationship detection
- [x] Ollama auto-detect for zero-config LLM/embeddings
- [x] MCP server (`mnemos mcp`) with 3 tools
- [x] Distribution: Homebrew, Docker, go install

---

## Phase 2A: MCP Project Memory (SHIPPED)

**Status:** Shipped on `main`, awaiting v0.4 tag
**Goal:** Make Mnemos the default persistent knowledge layer for AI coding agents.

### Milestones

- [x] Project-scoped DB (`.mnemos/mnemos.db` in working directory) — `mnemos init` + git-style discovery walking up from CWD
- [x] Auto-ingest project docs on MCP startup — README, PRD, CHANGELOG, Roadmap, CLAUDE.md, ARCHITECTURE, top-level `docs/`, recursive ADR conventions
- [x] File watch MCP tool (`watch_file`) — polling-based, sha256 content comparison, in-memory state
- [x] Browsing MCP tools (`list_claims`, `list_decisions`, `list_contradictions`) — paginated, filtered, hydrated
- [x] Git-aware context — commit auto-ingest at MCP startup + `ingest_git_log` tool. PR descriptions deferred (needs gh CLI auth)

### Success Metrics

- AI agent correctly answers project decision questions
- Zero manual `mnemos process` commands after MCP setup
- Knowledge persists across agent sessions

---

## Phase 2B: Knowledge Registry (FUTURE)

**Status:** Planned (v0.5)
**Goal:** Enable knowledge to flow across projects and teams through a shared registry.

### Concept

```
Local DB    = local repo     (per-project, local-first)
Registry    = remote origin  (shared team knowledge)
mnemos push = share knowledge to registry
mnemos pull = query team knowledge alongside local
```

### Milestones

- [ ] `mnemos serve` — HTTP API registry server
- [ ] `mnemos registry connect <url>` — wire local to registry
- [ ] Automatic sync on process/query
- [ ] Cross-project queries
- [ ] REST API for programmatic access
- [ ] Namespace/scope isolation (team, org, project)

### Success Metrics

- Cross-project query returns relevant answers with source provenance
- 50% reduction in "when did we decide X?" questions within teams

---

## Phase 3: Cognitive Infrastructure (FUTURE)

**Status:** Future (v1.0)
**Goal:** Backend standard for enterprise AI and decision systems.

### Milestones

- [ ] GraphRAG integration (multi-hop queries)
- [ ] Governance and bias detection
- [ ] Enterprise integrations (Slack, Teams, Jira)
- [ ] Compliance and audit trails
- [ ] Web interface (built on Phase 2B API)

---

## Development Principles

1. **Validate before scaling** — Each phase must prove value before expanding
2. **Local-first** — Data stays on user's machine until explicitly shared
3. **Evidence-backed** — Every claim traces to source material
4. **No magic** — Explicit over implicit; simple over complex

## Contributing

See [CONTRIBUTING.md](./CONTRIBUTING.md) for development guidelines.
