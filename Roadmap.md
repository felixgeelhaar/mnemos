# Mnemos Roadmap

*Core Principle: Validate each phase before expanding.*

---

## Phase 1: Developer Primitive (COMPLETE)

**Status:** Complete (v0.1)
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
- [x] LLM-powered extraction
- [x] Embeddings for semantic search

---

## Phase 2: Team Knowledge Engine

**Status:** Planned (v0.2)
**Goal:** Enable non-technical knowledge workers to get value from Mnemos without CLI proficiency.

### Outcomes

- **Outcome 1: Accessible querying** — Non-technical users can query knowledge without JSON knowledge
  - [ ] Web interface for non-technical users
  - [ ] Human-readable query output mode
- **Outcome 2: Team knowledge retention** — Knowledge persists across project lifecycles and team changes
  - [ ] Team collaboration (shared knowledge bases)
  - [ ] Decision tracking with status workflows
- **Outcome 3: Zero-friction ingestion** — Users can connect existing document sources
  - [ ] Cloud document ingestion (Drive, Notion, Confluence)
- **Outcome 4: Trust and governance** — Teams can verify and approve extracted knowledge
  - [ ] Evidence review and approval UI
  - [ ] Programmatic REST API

### Success Metrics

- Time-to-first-query for non-technical users < 2 minutes
- 50% reduction in "when did we decide X?" questions within teams
- 80% of ingested documents produce actionable claims

---

## Phase 3: Cognitive Infrastructure

**Status:** Future (v1.0)
**Goal:** Backend standard for enterprise AI and decision systems.

### Milestones

- [ ] GraphRAG integration (multi-hop queries)
- [x] Semantic search with embeddings (shipped in Phase 1)
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
