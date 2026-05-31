# ADR 0004: Extract decisionkit from Nous

- **Status:** Accepted
- **Date:** 2026-05-31
- **Deciders:** Felix Geelhaar
- **Scope:** New standalone repository `github.com/felixgeelhaar/decisionkit`.
  Source code lifted from `nous/internal/risk/` and `nous/internal/intervention/`.
  Prerequisite to archiving Nous (ADR 0005).

## Context

We are archiving Nous (ADR 0005) as part of the cognitive-stack
simplification. Most of Nous's value (LLM extraction, gRPC/HTTP transport,
multi-backend storage, service shell) is replaceable by agent runtimes plus
Mnemos and dies with the repo.

Two packages inside Nous have unique, non-replaceable value:

1. **`internal/risk/`** — a deterministic, weighted, explainable risk
   scorer. Inputs: a set of factors (overdue, due-soon, confidence,
   external signals). Output: a score with a per-factor breakdown that an
   operator can read. Pure functions, no I/O, no LLM.
2. **`internal/intervention/`** — a threshold-based policy engine mapping
   risk scores to intervention levels (nudge / escalate / automation),
   with confidence-aware routing to avoid auto-remediation on low-confidence
   inputs.

These are not LLM wrappers. They are domain logic with a 90%+ test suite.
Obvia (our remediation system, the only programmatic consumer in scope)
needs deterministic scoring for three load-bearing reasons:

- **Compliance.** The risk score's per-factor breakdown is an auditable
  rationale. An LLM judgement is opaque.
- **Cost.** Scoring runs on every observation. LLM calls per observation
  are not viable at scale.
- **Latency.** Deterministic scoring is sub-millisecond; LLM calls are
  hundreds of milliseconds.

A frontier model can reason about risk qualitatively. It cannot replace a
deterministic, audited, low-latency scoring path.

## Decision

We will extract `nous/internal/risk/` and `nous/internal/intervention/`
into a new standalone repository: **`github.com/felixgeelhaar/decisionkit`**.

The new repo is a small, single-purpose Go module:

- `decisionkit/risk/` — copied verbatim from `nous/internal/risk/`, including
  the existing test suite.
- `decisionkit/intervention/` — copied verbatim from `nous/internal/intervention/`,
  including tests.
- `decisionkit/mnemos/` — new thin adapter mapping `mnemos.Event` into the
  factor inputs the risk scorer expects, so consumers can call
  `decisionkit.Score(events)` directly against a Mnemos query result.

We do **not** carry over from Nous:

- The LLM extractor (`internal/llm`) — replaced by agent tool calls.
- Storage adapters (`internal/store`) — Mnemos owns event storage.
- gRPC / HTTP transports — decisionkit is a library, not a service.
- The commitment domain model — stays in the archived Nous repo for
  reference; consumers using decisionkit construct their own input shapes.

Initial release: `v0.1.0`. Pre-1.0 versioning until at least 90 days of
consumer feedback or a second adopter beyond Obvia.

## Consequences

**Positive:**

- Obvia gets a small, focused dependency with deterministic, audited
  scoring. No coupling to Nous's service shell or any LLM provider.
- Future programmatic systems (if they emerge) have a library to embed
  without standing up a service.
- The 90%+ test coverage from Nous comes along — no rewrite, no fidelity
  loss.

**Negative / risks:**

- We introduce a new repo to maintain. Mitigation: small surface area (two
  packages of pure functions); no roadmap beyond bug fixes.
- decisionkit's API shape may need iteration based on Obvia's actual usage.
  Mitigation: ship `v0.x` until proven; sealed types where the API
  could evolve.

## Migration path

Nous consumers (currently only Olymp, which is also being archived):

1. Replace import `github.com/felixgeelhaar/nous/internal/risk` with
   `github.com/felixgeelhaar/decisionkit/risk`.
2. Replace import `github.com/felixgeelhaar/nous/internal/intervention` with
   `github.com/felixgeelhaar/decisionkit/intervention`.
3. If the consumer was using Nous's storage or gRPC layer for risk inputs,
   call the appropriate Mnemos query directly and feed results into
   `decisionkit/mnemos.ToFactors(events)` before scoring.

Since Olymp is archived, there are no live migrations required at the time
of decisionkit creation. The migration path documented here is for any
future consumer who picks Nous up from its archived state.

## Alternatives Considered

**1. Inline risk + intervention into Obvia directly.**
Rejected. Obvia is the only programmatic consumer *today*, but a small
shared library is cheap to publish and worth keeping reusable. The cost
delta between "inline" and "small lib" is negligible; the optionality is
real.

**2. Migrate risk + intervention into Mnemos as `internal/decisions/`.**
Rejected. Risk scoring is policy, not memory. Mnemos has been
disciplined about not owning intent or decision logic. Folding decisions
into Mnemos overloads the substrate and contaminates the "memory layer"
positioning.

**3. Use the Anthropic SDK / agent tool calls for risk scoring instead.**
Rejected. Quantitative, deterministic, auditable, low-latency scoring is
not a strength of LLMs. The risk scorer's factor breakdown is the audit
trail; an LLM judgement is opaque to compliance review.

## Related Work

- [ADR 0003: Archive Olymp](0003-archive-olymp.md)
- [ADR 0005: Archive Nous](0005-archive-nous.md)
- [ADR 0006: Archive Praxis](0006-archive-praxis.md)
