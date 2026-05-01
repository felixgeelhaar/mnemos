# ADR 0002: Cross-entity edges and pull-based outcome adapters

## Status

Accepted (2026-04). Implemented in `internal/autoedge` and `internal/adapters/outcomes`.

## Context

Mnemos's original `relationships` table carries `supports` / `contradicts` edges between Claims only. Phase 1-6 added domain types that don't fit into a claim-only graph: Actions, Outcomes, Lessons, Playbooks, and Decisions. The audit layer needs to answer questions like:

- "Which outcome refuted decision X?"
- "Which lessons derive from this action+outcome cluster?"
- "Which playbook step was validated by this run?"

Encoding those answers as separate per-pair tables would multiply the schema and force every reader to know the right join. A polymorphic edge table — `entity_relationships(from_id, from_kind, to_id, to_kind, edge_kind, ...)` — gives one place to read and write the cross-entity graph.

Separately, Outcomes have to enter Mnemos somehow. Manual `mnemos outcome record` works for human operators, but production pipelines emit outcomes as a side-effect of metric movements: a deploy at T0 expects p95 latency to stay below 300 ms over the next 10 minutes; if it does, that's a `success` Outcome. Pulling from Prometheus and similar metric stores closes that loop without forcing every operator to script their own integration.

## Decision

### 1. `entity_relationships` table + `internal/autoedge` package

A polymorphic table holds all non-claim-to-claim edges. Edge kinds covered today:

- `action_of` / `outcome_of` — connect an Action to its Outcomes (auto-fired when `OutcomeRepository.Append` runs).
- `validates` / `refutes` — connect an Outcome to the Decision it observed (auto-fired when `decision attach-outcome` runs).
- `derived_from` — connects synthesised Lessons / Playbooks to their evidence.

`internal/autoedge` is the seam between domain operations and edge bookkeeping. Its helpers are idempotent on the unique edge tuple so retrying is safe.

The classic claim-only `relationships` table stays as-is for `supports` / `contradicts` and the causal family added in Phase 1 (`causes`, `caused_by`, ...). Both tables are queried by the answer engine; surfacing decisions / lessons / playbooks alongside claims is a join over both.

### 2. `internal/adapters/outcomes` pull adapters

A new adapter port:

```go
type Adapter interface {
    Pull(ctx context.Context, actionID string) (domain.Outcome, error)
    Name() string
}
```

The first implementation is `PrometheusAdapter` — it issues PromQL instant queries, classifies the result against operator-defined `SuccessThreshold` per metric, and returns a domain `Outcome` keyed to the supplied `actionID`. The adapter knows nothing about persistence; the caller handles ID generation and `OutcomeRepository.Append`.

The pull-vs-push choice is deliberate: outcomes need an `actionID` to anchor to, and the action is known at the moment we pull. Running adapters as background pollers without a known action would emit orphans.

## Consequences

### Positive
- One place to read the cross-entity graph (`entity_relationships`); no schema sprawl per pair.
- Auto-fire keeps callers honest — recording an Outcome cannot accidentally skip the `action_of` edge.
- Pull adapters let production systems emit Outcomes against their existing metrics stack without writing custom export code.
- The adapter port is tiny (`Pull`, `Name`), making future adapters (Datadog, CloudWatch, OpenTelemetry, log scrapers) straightforward.

### Negative
- Two relationship tables now live in the schema (`relationships` for claim edges, `entity_relationships` for everything else). Readers must remember to query both.
- Polymorphic FKs are not enforced at the database level — `internal/autoedge` is the integrity layer. Direct INSERTs that bypass autoedge can write garbage edges.
- The Prometheus adapter assumes single-shot instant queries. Range queries (e.g. "p95 over the last 30 m") need a follow-up adapter or a wrapper that aggregates client-side.

## Alternatives considered

- **One relationship table**: Reuse `relationships` with a polymorphic discriminator. Rejected because the historical column shape (`from_claim_id` / `to_claim_id`) bakes in the assumption that both endpoints are claims; widening it would break every existing query.
- **Push-based outcomes via webhook**: Have Prometheus / Alertmanager push outcomes to Mnemos. Rejected as the v1 default — it requires every operator to wire alerting rules and an inbound endpoint, when most teams already have Prometheus running and can tolerate a polling delay.
- **Skip auto-fire**: Make every caller responsible for emitting edges. Rejected because forgetting to emit produces a silently incomplete audit graph.

## Forward-looking

- ADR-0003 will record the runner that schedules pull adapters across multiple actions.
- A Datadog adapter is the obvious next addition; the interface is intentionally small to keep that easy.
