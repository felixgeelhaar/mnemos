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

Claim-to-claim edge. Fields: id, type (supports | contradicts), from\_claim\_id, to\_claim\_id, created\_at

### 3.1.6 CompilationJob

Tracks processing. Fields: id, kind, status, scope, started\_at, updated\_at, error

## 3.2 Data Storage

Database: SQLite (MVP)

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

Use statekit. Extraction states: pending, loading, extracting, saving, relating, completed, failed.

# 5\. Implementation Concerns

## 5.1 Error Handling

Use fortify. Rules: retry LLM calls (max 2), timeout external calls, mark job failed on error, no partial inconsistent writes.

## 5.2 Observability

Use bolt logging. Fields: job\_id, event\_id, claim\_id, duration, outcome, error.

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

