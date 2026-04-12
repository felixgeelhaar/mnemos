# Mnemos Roadmap

*Core Principle: Validate each phase before expanding.*

---

## Phase 1: Developer Primitive (CURRENT)

**Status:** In Progress (v0.1)
**Goal:** Establish Mnemos as a local-first, open-source knowledge engine for AI agents and developer tooling.

### Milestones

- [x] Core domain model (Event, Claim, Relationship)
- [x] Multi-format input ingestion (TXT, MD, JSON, CSV, raw text)
- [x] Append-only SQLite event store
- [x] Claim extraction engine with evidence mapping
- [x] Relationship detection (supports/contradicts)
- [x] CLI query interface
- [x] Workflow orchestration with structured logs
- [x] SQLC typed data access
- [x] 68 eval test cases
- [x] First-run UX polish
- [ ] LLM-powered extraction
- [ ] Embeddings for semantic search

---

## Phase 2: Team Knowledge Engine

**Status:** Planned (v0.2)
**Goal:** Expand to auditable, traceable institutional memory for knowledge workers.

### Milestones

- [ ] Web interface for non-technical users
- [ ] Human-readable query output mode
- [ ] Team collaboration (shared knowledge bases)
- [ ] Cloud document ingestion (Drive, Notion, Confluence)
- [ ] Decision tracking with status workflows
- [ ] Evidence review and approval UI
- [ ] Programmatic REST API

---

## Phase 3: Cognitive Infrastructure

**Status:** Future (v1.0)
**Goal:** Backend standard for enterprise AI and decision systems.

### Milestones

- [ ] GraphRAG integration (multi-hop queries)
- [ ] Semantic search with embeddings
- [ ] Multi-agent pipeline orchestration
- [ ] Governance and bias detection
- [ ] Enterprise integrations (Slack, Teams, Jira)
- [ ] Compliance and audit trails

---

## Development Principles

1. **Validate before scaling** — Each phase must prove value before expanding
2. **Local-first** — Data stays on user's machine until explicitly shared
3. **Evidence-backed** — Every claim traces to source material
4. **No magic** — Explicit over implicit; simple over complex

## Contributing

See [CONTRIBUTING.md](./CONTRIBUTING.md) for development guidelines.
