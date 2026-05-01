# Mnemos — Technical Design Document (TDD)

# 1\. Introduction

## 1.1 Purpose

This document defines the architecture, domain model, data structures, and execution flows required to implement Mnemos. Goal: Enable building the system without ambiguity.

## 1.2 System Overview

Mnemos transforms arbitrary inputs into structured, evidence-backed knowledge. Core pipeline: Input → Parser → Event Store → Claim Extraction → Relationship Detection → Query Engine

## 1.3 Architectural Principles

1. append-only event store (source of truth)  
2. claims and relationships are derived  
3. domain model independent of LLM/vendor  
4. separation of domain and infrastructure  
5. workflows explicitly modeled

# 2\. System Architecture

## 2.1 Components

6. Input Service  
7. Parser Layer  
8. Event Store  
9. Extraction Engine  
10. Relationship Engine  
11. Query Engine  
12. CLI Interface

# 3\. Data Model

## 3.1 Domain Model (DDD)

### 3.1.1 Input

Represents raw user-provided data. Fields: id, type (text | json | csv | image | transcript), format (file extension), metadata, created\_at

### 3.1.2 Event (Aggregate Root)

Immutable normalized unit. Fields: id, schema\_version, content, source\_input\_id, timestamp, metadata, ingested\_at. Rules: immutable, append-only

### 3.1.3 Claim

Derived knowledge unit. Fields: id, text, type (fact | hypothesis | decision), confidence (0–1), status (active | contested | deprecated), created\_at

### 3.1.4 ClaimEvidence

Mapping claim → events. Fields: claim\_id, event\_id. Rule: ≥1 event per claim

### 3.1.5 Relationship

Claim-to-claim edge. Fields: id, type (`supports`, `contradicts`, plus the causal/outcome family `causes`, `caused_by`, `action_of`, `outcome_of`, `validates`, `refutes`, `derived_from`), from\_claim\_id, to\_claim\_id, created\_at.

### 3.1.6 CompilationJob

Tracks processing. Fields: id, kind, status, scope, started\_at, updated\_at, error.

### 3.1.7 Action (Phase 2)

Recorded operational change. Fields: id, kind (deploy | rollback | scale | ...), subject, actor, run\_id, at, metadata\_json.

### 3.1.8 Outcome (Phase 2)

Observed result of an Action. Fields: id, action\_id, result (success | failure | partial | unknown), metrics\_json, source (push | pull:prometheus | ...), at.

### 3.1.9 Lesson (Phase 3)

Synthesised operational truth derived from action→outcome chains. Fields: id, statement, scope (Service, Env, Team), evidence (\[]ActionID), confidence, trigger, kind, source (synthesize | human), valid\_from, valid\_to.

### 3.1.10 Decision (Phase 5)

Agent decision audit record. Fields: id, statement, plan, reasoning, risk\_level (low | medium | high | critical), beliefs (\[]ClaimID), alternatives, outcome\_id, scope, created\_at.

### 3.1.11 Playbook (Phase 6)

Praxis-ready response derived from Lesson clusters. Fields: id, trigger, scope, steps (\[]PlaybookStep), derived\_from\_lessons, confidence, valid\_from, valid\_to.

### 3.1.12 Scope (Phase 8)

Multi-tenant filter primitive: {Service, Env, Team}. Attached to Claims, Lessons, Decisions, Playbooks.

## 3.2 Data Storage

Database: pluggable per [ADR 0001](docs/adr/0001-multi-backend-storage.md). Default SQLite (`modernc.org/sqlite`, FTS5); also `memory://`, `postgres://`, `mysql://`, `libsql://`. Backends register from `init()` against the URL-scheme dispatcher in `internal/store`.

### 3.2.1 Tables: events

id (PK), schema\_version, content, source\_input\_id, timestamp, metadata\_json, ingested\_at. Indexes: timestamp, source\_input\_id

### 3.2.2 Tables: claims

id (PK), text, type, confidence, status, created\_at

### 3.2.3 Tables: claim\_evidence

claim\_id, event\_id. PK(claim\_id, event\_id)

### 3.2.4 Tables: relationships

id (PK), type, from\_claim\_id, to\_claim\_id, created\_at

### 3.2.5 Tables: compilation\_jobs

id (PK), kind, status, scope\_json, started\_at, updated\_at, error

### 3.2.6 Tables: actions, outcomes (Phase 2)

`actions(id PK, kind, subject, actor, run_id, at, metadata_json)`. `outcomes(id PK, action_id FK, result, metrics_json, source, at)`. Index on `actions(subject)`, `actions(run_id)`, `outcomes(action_id)`.

### 3.2.7 Tables: lessons, lessons\_versions (Phase 3 + Phase 7)

`lessons(id PK, statement, scope_service, scope_env, scope_team, evidence_json, confidence, trigger, kind, source, valid_from, valid_to)`. Companion `lessons_versions` is a system-versioned snapshot table populated by triggers on every UPDATE/DELETE — supports `mnemos history --kind lesson`.

### 3.2.8 Tables: decisions (Phase 5)

`decisions(id PK, statement, plan, reasoning, risk_level, beliefs_json, alternatives_json, outcome_id, scope_service, scope_env, scope_team, created_at)`. FK `outcome_id → outcomes(id)` nullable.

### 3.2.9 Tables: playbooks, playbook\_steps, playbooks\_versions (Phase 6 + Phase 7)

`playbooks(id PK, trigger, scope_*, derived_from_lessons_json, confidence, valid_from, valid_to)`. `playbook_steps(playbook_id FK, ordinal, kind, args_json)`. `playbooks_versions` mirrors `lessons_versions`.

### 3.2.10 Tables: entities, claim\_entities (Phase 9)

`entities(id PK, type, normalized_name UNIQUE-with-type, display_name)`. `claim_entities(claim_id FK, entity_id FK)`. Materialised by the pipeline from LLM-tagged claims.

### 3.2.11 Tables: claim\_status\_history (Phase 4 lifecycle)

`claim_status_history(id PK, claim_id FK, from_status, to_status, changed_at, changed_by, reason)`. Append-only audit trail consumed by the query engine to render `Evolution:` lines.

### 3.2.12 Tables: events\_fts, claims\_fts (Phase 10 hybrid retrieval)

FTS5 virtual tables maintained by INSERT/UPDATE/DELETE triggers on `events` and `claims`. Backfilled on the v0.9 → v0.10 migration. Reads do not need to think about staleness.

## 3.3 Interfaces (Ports)

```go
type EventRepository interface { Append(Event) error; GetByID(string) (Event, error); ListByIDs([]string) ([]Event, error) }
```

```go
type ClaimRepository interface { Upsert([]Claim) error; ListByEventIDs([]string) ([]Claim, error) }
```

```go
type RelationshipRepository interface { Upsert([]Relationship) error; ListByClaim(string) ([]Relationship, error) }
```

```go
type ExtractionEngine interface { ExtractClaims([]Event) ([]Claim, error) }
```

```go
type QueryEngine interface { Answer(query string) (Answer, error) }
```

# 4\. Execution Flows

## 4.1 Ingestion Flow

13. user adds input  
14. parser processes input  
15. events created  
16. events stored  
17. embeddings indexed  
18. compilation job created

## 4.2 Claim Extraction Flow

19. load events  
20. call extraction engine  
21. validate claims  
22. persist claims  
23. link evidence

## 4.3 Relationship Detection

24. load claims  
25. compare within scope  
26. detect: supports, contradicts  
27. persist relationships

## 4.4 Query Flow

28. receive query  
29. semantic search → events  
30. load claims  
31. load relationships  
32. assemble timeline  
33. compose answer

## 4.5 Output

```json
{ "answer": "...", "claims": [...], "contradictions": [...], "timeline": [...] }
```

## 4.6 Extraction Pipeline

Responsibilities: extract claims, assign type, assign confidence, map evidence. Validation: non-empty text, valid confidence, evidence exists.

## 4.7 Workflow Orchestration

Use `github.com/felixgeelhaar/statekit` to model workflow states and transitions. The current runner lifecycle is `pending -> running -> loading -> extracting -> saving -> relating -> embedding -> completed|failed`, and should be expressed as a state machine rather than ad hoc string transitions.

# 5\. Implementation Concerns

## 5.1 Error Handling

Use `github.com/felixgeelhaar/fortify` for retry, timeout, and resilience around external calls. CLI-facing failures still map into `MnemosError` exit codes, but networked LLM and embedding operations should be guarded by Fortify policies. Rules: retry LLM calls (max 2), timeout external calls, mark job failed on error, no partial inconsistent writes.

## 5.2 Observability

Use `github.com/felixgeelhaar/bolt` for structured logging. Fields: job\_id, kind, stage, attempt, duration\_ms, error, and provider/model metadata for LLM and embedding operations.

## 5.3 Concurrency

MVP: single process, sequential jobs. Future: worker pool, job queue.

## 5.4 Extensibility

Replaceable components: LLM provider, vector DB, storage backend.

## 5.5 Tradeoffs

SQLite: simple, local-first. Relational DB: easier MVP. Batch processing: simpler than streaming.

## 5.6 Non-functional Requirements (NFRs)

1. **Performance:**  
   1. Ingestion Latency: 95% of Input-to-Event processing must complete within 5 seconds.  
      2. Query Latency: 99% of Answer queries must return results within 2 seconds.  
   2. **Scalability:**  
      1. The system must handle 100 concurrent input ingestion flows.  
      2. Database choice must allow for future scaling to a distributed relational/NoSQL solution beyond the SQLite MVP.  
   3. **Security:**  
      1. All external service calls (e.g., LLM provider) must use authenticated, encrypted connections (TLS).  
      2. Raw user inputs must be isolated from derived claims/events via ACLs if stored separately.  
   4. **Availability:**  
      1. The core Query Engine must maintain 99.9% uptime.

# 6\. Project Context

## 6.1 Testing Strategy

5. **Unit Tests:** Every component interface will have unit tests with mock dependencies, aiming for \>90% code coverage.  
   6. **Integration Tests:** End-to-end flow tests will be implemented for Ingestion, Claim Extraction, and Querying, using an in-memory test database.  
   7. **Validation Tests (LLM):** A fixed dataset of inputs and expected claims/relationships will be maintained to validate the Extraction and Relationship Engines after any changes to the LLM interaction logic or provider.  
   8. **Chaos Testing:** (Future) Introduce faults into the event store to validate the Error Handling and retry mechanisms.

## 6.2 Definition of Done

34. inputs ingested  
35. events stored  
36. claims extracted  
37. relationships created  
38. query returns structured answer  
39. contradictions visible

## 6.3 Open Questions

40. claim quality thresholds  
41. contradiction detection accuracy  
42. when to add governance
