# Mnemos — Product Requirements Document (PRD)

## 1. Overview

Mnemos transforms arbitrary inputs into structured, evolving, evidence-backed knowledge. It enables users to understand what is true and why—eliminating AI hallucination through traceable claims.

**Core Differentiator:** Epistemic provenance — Mnemos doesn't just store claims, it explains *why* a claim should be trusted over alternatives through source credibility signals, citation graphs, and liveness detection.

## 2. Problem Statement

### 2.1 The Hallucination Problem

AI systems are becoming decision-makers, but they forget context, invent facts, and contradict themselves. Without a system of truth, AI cannot be reliable.

**The cost is real:**
- $67.4B annual cost of AI hallucination (2026)
- 51% of enterprise AI responses contain fabrications on RAG data
- 52% of organizations report significant negative consequences from AI inaccuracies
- 62% remain in "Pilot Purgatory" because they can't guarantee reliability

**The root cause:** RAG alone reduces hallucination by 40-71%, but RAG on ungoverned data = 52% fabrication rate. The problem isn't the model—it's the data layer.

### 2.2 Knowledge Decay

- **Fragmented:** Scattered across siloed tools, documents, and message threads
- **Contradictory:** No version control to know which information is current
- **Ungrounded:** Essential context, evidence, and history constantly lost

## 3. Strategic Vision

**Mnemos is to knowledge what Git is to code.**

- versioned
- traceable
- auditable
- collaborative
- evolving

**The Epistemic Layer:** Beyond storage, Mnemos answers *"Why should I trust this claim over that one?"* — resolving conflicts through provenance signals (citation density, liveness, authority, recency) rather than treating all claims as equal.

## 4. Phased Roadmap

### Phase 1: Developer Primitive (COMPLETE) — v0.1

**Goal:** Establish Mnemos as a local-first, open-source knowledge engine for AI agents and developer tooling.

**Target Users:** AI Engineers building RAG systems, developers needing persistent memory

**Deliverables:**
- [x] CLI scaffold with ingestion for files and raw text
- [x] SQLite-backed append-only event persistence
- [x] Claim extraction with event-to-claim evidence mapping
- [x] Relationship detection (supports/contradicts)
- [x] CLI query interface with structured JSON output
- [x] Workflow jobs with status transitions and structured logs
- [x] SQLC-based typed data-access layer
- [x] 102 eval test cases for extraction accuracy
- [x] First-run experience and UX polish
- [x] LLM-powered extraction (Anthropic, OpenAI, Gemini, Ollama, OpenAI-compatible)
- [x] Embeddings for semantic search (multi-provider)

**Excluded:**
- GUI / Web interface
- Real-time streaming
- Multi-modal inputs
- Collaboration features

**Success Metrics:**
- Time-to-value < 5 minutes
- ≥70% claim extraction precision on eval dataset
- <20% false positive rate on contradiction detection

---

### Phase 2A: MCP Project Memory — v0.4

**Goal:** Make Mnemos the default persistent knowledge layer for AI coding agents, providing project-scoped memory that survives across sessions.

**Target Users:** Developers using AI coding agents (Claude Code, Cursor, Copilot)

**Deliverables:**
- [ ] Project-scoped DB — `mnemos mcp` auto-resolves to `.mnemos/mnemos.db` in working directory
- [ ] Auto-ingest on startup — scan for project docs (README, PRD, ADRs, CHANGELOG) and ingest new/changed files
- [ ] File watch MCP tool — `watch_file` to track specific files for changes
- [ ] Browsing MCP tools — `list_claims`, `list_decisions`, `list_contradictions` for agent exploration
- [ ] Git-aware context — ingest recent commit messages and PR descriptions as decision trail
- [x] Ollama auto-detect — zero-config LLM and embeddings when Ollama is running locally

**Success Metrics:**
- AI agent answers "what was decided about X?" correctly on real project docs
- Zero manual `mnemos process` commands needed after MCP setup
- Knowledge persists across agent sessions without re-ingestion

---

### Phase 2B: Knowledge Registry — v0.5

**Goal:** Enable knowledge to flow across projects and team members through a shared registry, completing the "Git for knowledge" analogy.

**Target Users:** Teams of developers working across multiple repositories

**Concept:**
```
Local DB    = local repo     (per-project, local-first)
Registry    = remote origin  (shared team knowledge)
mnemos push = share claims, decisions, contradictions to registry
mnemos pull = query team knowledge alongside local knowledge
```

**Deliverables:**
- [ ] `mnemos serve` — run Mnemos as an HTTP API (the registry server)
- [ ] `mnemos registry connect <url>` — wire a local instance to a registry
- [ ] Automatic sync — claims pushed on process, registry queried on query
- [ ] Cross-project queries — "What did we decide about auth across ALL projects?"
- [ ] REST API for programmatic access
- [ ] Namespace/scope isolation — team-level, org-level, project-level

**Open Questions:**
- Push semantics: all claims or only high-confidence/decisions?
- Conflict resolution: when local and registry claims contradict, who wins?
- Access control: read-only vs read-write registry access per project?

---

### Phase 3: Cognitive Infrastructure — v1.0

**Goal:** Evolve into backend standard for complex decision systems and enterprise AI.

**Target Users:** Enterprise AI systems, decision automation platforms

**Deliverables:**
- [ ] GraphRAG integration for multi-hop queries
- [x] Embeddings for semantic search (shipped in Phase 1)
- [ ] **Epistemic Provenance & Claim Trust Framework**
  - [ ] Claim provenance data model (source doc, authority, liveness)
  - [ ] Citation graph & link density tracking (know what converges)
  - [ ] Liveness detection (e.g. 12-year-old process doc still being executed = live)
  - [ ] Source credibility scoring engine (link density + liveness + recency + authority)
  - [ ] Test provenance model (first-class "test result" as claim with metadata)
  - [ ] Test conflict detection (Test1 passes, Test2 fails for same thing)
  - [ ] Confidence-weighted conflict resolution (which test/source to trust?)
  - [ ] Provenance Query API: "Why trust this claim?" with rationale
  - [ ] Human-readable provenance markdown export
- [ ] Governance and bias detection
- [ ] Enterprise integrations (Slack, Teams, Jira)
- [ ] Compliance and audit trails
- [ ] Web interface for non-technical users (built on Phase 2B API)

## 5. Core Use Cases

### Use Case 1: AI Engineer (Phase 1)

**Scenario:** "Build a RAG system that doesn't hallucinate"

**Inputs:** Technical docs, architecture decisions, meeting notes
**Output:** Evidence-backed claims with contradiction detection
**Tool:** CLI, programmatic API

### Use Case 2: AI Coding Agent (Phase 2A)

**Scenario:** "My Claude Code agent needs to know what decisions our team made about the API design"

**Inputs:** Project docs auto-ingested via MCP (PRD, ADRs, commit history)
**Output:** Persistent project memory that grounds agent responses across sessions
**Tool:** MCP server, zero-config with Ollama

### Use Case 3: Cross-Project Team Knowledge (Phase 2B)

**Scenario:** "What did we decide about authentication across all our services?"

**Inputs:** Multiple project knowledge bases synced to a shared registry
**Output:** Cross-project answers with evidence provenance per source project
**Tool:** CLI + registry API

### Use Case 4: Enterprise AI (Phase 3)

**Scenario:** "Ground AI decisions in verified organizational knowledge"

**Inputs:** All organizational data sources via registry

**Output:** Trusted, auditable AI responses

**Tool:** REST API, SDK, web interface

### Use Case 5: Epistemic Provenance (Phase 3)

**Scenario:** "I have conflicting docs/claims — which one should I trust?"

**User:** Power user with Karpathy-style workflow (Obsidian + transcriptions + docs). Finds two claims that contradict. Needs to know: which is more可信?

**Inputs:** Multiple documents, transcriptions, git commits, test results

**Provenance Signals:**
- Citation density (3 sources agree on Claim A)
- Liveness (12-year-old process doc still being executed)
- Recency (this spec was just updated yesterday)
- Authority (this came from the CTO's decision)
- Test conflict (Test1 passes, Test2 fails — which test is newer/more authoritative?)

**Output:** Trusted claim + rationale ("Trust Claim A: 3 sources agree, source is live (last executed 2 days ago), contradicts Test2 which is 6 months stale")

**Tool:** `mnemos query --explain-trust <claim-id>`, HTTP/gRPC API, MCP provenance tools

## 6. Product Principles

1. **Truth is derived, not stored** — All knowledge originates from raw inputs
2. **Time matters** — History preserved, knowledge evolves
3. **Contradictions are signals** — Conflict is insight, not error
4. **Systems must explain themselves** — Every answer includes evidence
5. **Provenance creates trust** — Not all claims are equal; explain *why* one source wins over others through citation density, liveness, authority, and recency

## 7. Non-Goals

- Workflow automation and agent orchestration
- Collaboration features in Phase 1
- Real-time streaming
- Multi-modal inputs (Phase 3)

## 8. Open Questions

- [ ] How should contradiction detection be tuned per domain?
- [ ] When to introduce governance vs. let knowledge evolve organically?
- [ ] Registry push semantics: all claims or only high-confidence/decisions?
- [ ] Registry conflict resolution: local vs registry when claims contradict?
- [x] ~~What embedding model balances speed vs. accuracy?~~ (Defaults: text-embedding-3-small for OpenAI, text-embedding-004 for Gemini, nomic-embed-text for Ollama)
- [x] ~~What is the threshold for acceptable claim accuracy?~~ (102 eval cases, F1 79.9%)
- [x] ~~Can BM25 alone produce useful query results?~~ (No — semantic search via embeddings required for real documents)

## 9. Definition of Success

Mnemos is successful when:

- Users can answer "What is true and why?"
- Answers are trusted due to evidence backing
- The system feels fundamentally superior to search or RAG
- AI outputs are grounded in verified knowledge
- **Users can resolve conflicting claims** — "Why should I trust Claim A over Claim B?"
- **Provenance signals guide decisions** — citation density, liveness, authority, and recency are first-class outputs
- **Test conflicts are resolved** — when Test1 passes but Test2 fails, the system explains which to trust and why
