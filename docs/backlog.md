
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
