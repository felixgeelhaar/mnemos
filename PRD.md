# Mnemos — Product Requirements Document (PRD)

## 1. Overview

Mnemos transforms arbitrary inputs into structured, evolving, evidence-backed knowledge. It enables users to understand what is true and why—eliminating AI hallucination through traceable claims.

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

## 4. Phased Roadmap

### Phase 1: Developer Primitive (CURRENT) — v0.1

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
- [x] 68 eval test cases for extraction accuracy
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

### Phase 2: Team Knowledge Engine — v0.2

**Goal:** Expand to auditable, traceable institutional memory for knowledge workers and product teams.

**Target Users:** Knowledge workers, product teams with decision debt

**Deliverables:**
- [ ] Web interface for non-technical users
- [ ] Human-readable query output mode
- [ ] Team collaboration features (shared knowledge bases)
- [ ] Document ingestion from cloud sources (Google Drive, Notion, Confluence)
- [ ] Decision tracking with status workflows
- [ ] Evidence review and approval UI
- [ ] API for programmatic access

**Success Metrics:**
- Non-technical users can query without JSON knowledge
- Team knowledge retention across project lifecycles
- Reduction in "when did we decide X?" questions

---

### Phase 3: Cognitive Infrastructure — v1.0

**Goal:** Evolve into backend standard for complex decision systems and enterprise AI.

**Target Users:** Enterprise AI systems, decision automation platforms

**Deliverables:**
- [ ] GraphRAG integration for multi-hop queries
- [x] Embeddings for semantic search (shipped in Phase 1)
- [ ] Multi-agent pipeline orchestration
- [ ] Governance and bias detection
- [ ] Enterprise integrations (Slack, Teams, Jira)
- [ ] Compliance and audit trails

## 5. Core Use Cases

### Use Case 1: AI Engineer (Phase 1)

**Scenario:** "Build a RAG system that doesn't hallucinate"

**Inputs:** Technical docs, architecture decisions, meeting notes
**Output:** Evidence-backed claims with contradiction detection
**Tool:** CLI, programmatic API

### Use Case 2: Product Team (Phase 2)

**Scenario:** "What decisions were made about feature X and why?"

**Inputs:** PRDs, meeting notes, Slack threads, metrics
**Output:** Timeline of decisions with evidence and contradictions
**Tool:** Web interface, natural language queries

### Use Case 3: Enterprise AI (Phase 3)

**Scenario:** "Ground AI decisions in verified organizational knowledge"

**Inputs:** All organizational data sources
**Output:** Trusted, auditable AI responses
**Tool:** Backend API, SDK

## 6. Product Principles

1. **Truth is derived, not stored** — All knowledge originates from raw inputs
2. **Time matters** — History preserved, knowledge evolves
3. **Contradictions are signals** — Conflict is insight, not error
4. **Systems must explain themselves** — Every answer includes evidence

## 7. Non-Goals

- Workflow automation and agent orchestration
- Collaboration features in Phase 1
- Real-time streaming
- Multi-modal inputs (Phase 3)

## 8. Open Questions

- [ ] How should contradiction detection be tuned per domain?
- [ ] When to introduce governance vs. let knowledge evolve organically?
- [x] ~~What embedding model balances speed vs. accuracy?~~ (Defaults: text-embedding-3-small for OpenAI, text-embedding-004 for Gemini, nomic-embed-text for Ollama)
- [x] ~~What is the threshold for acceptable claim accuracy?~~ (78 eval test cases, rule-based + LLM extraction)

## 9. Definition of Success

Mnemos is successful when:
- Users can answer "What is true and why?"
- Answers are trusted due to evidence backing
- The system feels fundamentally superior to search or RAG
- AI outputs are grounded in verified knowledge
