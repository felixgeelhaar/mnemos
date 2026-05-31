# ADR 0006: Archive Praxis

- **Status:** Accepted
- **Date:** 2026-05-31
- **Deciders:** Felix Geelhaar
- **Scope:** Cognitive stack repository topology. Depends on Obvia inlining
  Praxis's orchestration primitives before this archive proceeds.

## Context

Praxis was the "action" primitive in the cognitive stack: a service that
executed structured actions against vendors (Slack, GitHub, Linear, HTTP,
Calendar, Email), with safety primitives layered around the execution path:

- Stable `Action.ID` + idempotency keeper — re-execution of the same ID
  never double-effects, even across restarts.
- Allow/deny policy engine with first-match rules.
- Outbox-backed outcome emission to Mnemos with retry.
- Audit trail with full lifecycle replay and redaction.
- Out-of-process plugin host with Fulcio keyless signing for sandboxed
  vendor handlers.
- Capability registry with schema validation.
- MCP server exposing actions to agent runtimes.

A repository audit on 2026-05-31 produced three findings:

1. **Vendor handlers are the wrong shape for agent-driven workflows.**
   Agent runtimes (Claude Code, Codex, Hermes, Nomi, OpenClaw, NanoClaw)
   connect to vendors through MCP servers and tool definitions. They do
   not import Go vendor handler libraries. The six Praxis handlers
   (Slack/GitHub/Linear/HTTP/Calendar/Email) are dead weight for the
   agent path.
2. **Obvia is the only programmatic (non-agent) consumer.** Obvia uses
   Praxis's HTTP API for compliance-grade remediation: idempotency,
   audit trail, policy gating, outcome feedback. None of that requires
   a separate service — Obvia can inline what it needs.
3. **The plugin host + Fulcio signing exists because Praxis is a
   multi-tenant service.** With execution moving in-process into Obvia,
   that complexity ceases to earn its keep.

There is no `actionkit` library extraction in scope. Rationale: with
only one programmatic consumer (Obvia) and agent runtimes not needing
the primitives at all, a shared library has insufficient justification.
Obvia inlines what it needs; vendor handlers die.

## Decision

We will archive the Praxis repository **immediately after the Obvia inline
migration PR merges**. The repo stays publicly readable, is set to
read-only on GitHub, and is tagged `v0.3.1-final`. README is replaced with
a redirect explaining the new architecture, and `DEPRECATED.md` documents
the rationale plus the recovery path.

The following Praxis components die with the repo:

- HTTP / gRPC API server and middleware.
- Out-of-process plugin host with Fulcio signing.
- Six vendor handlers (Slack, GitHub, Linear, HTTP, Calendar, Email) —
  Obvia rewrites with direct SDK calls (`slack-go`, `go-github`, ...);
  agents use MCP servers.
- Capability registry as a service. (Capabilities for agents = tool
  definitions in their runtime.)
- Praxis MCP server. (Each agent runtime exposes its own MCP surface.)
- The executor and handler-runner orchestration.

The following primitives are **inlined into Obvia** (not extracted into
a library):

- Idempotency keeper from `praxis/internal/idempotency/` (~25 LOC).
- Outbox-backed outcome emission from `praxis/internal/outcome/`, adapted
  to talk to Mnemos directly. Event name renamed from
  `praxis.action_completed` to `action.completed` to drop the praxis
  namespace.
- Audit trail from `praxis/internal/audit/` (or simplified to bolt
  structured logs if Obvia's needs are lighter).
- Policy engine from `praxis/internal/policy/` **only if Obvia actually
  uses it** (verified by grep before inlining).

This inlining ships as a single PR in the Obvia repo with no feature flag
and no canary — Obvia is in dogfooding with no external users, so a
straight cutover is acceptable.

## Consequences

**Positive:**

- The cognitive stack loses an action service whose primary justification
  was multi-tenancy, sandboxing, and vendor wrappers. None of those serve
  the agent-driven future.
- Obvia owns its own execution path with no remote-service hop, lower
  latency, and a smaller failure surface.
- Vendor SDKs are called directly where they're called — agents via MCP,
  Obvia via Go SDK imports. No abstraction tax.

**Negative / risks:**

- The replay-from-audit invariant ("operators can derive state from audit
  alone") becomes Obvia's responsibility to preserve. Mitigation: copy
  the audit tests with the code; require parity before merging.
- Praxis's out-of-process sandboxing for vendor handlers is lost. In a
  multi-tenant service that mattered; in Obvia's single-tenant programmatic
  loop it does not.
- A future programmatic consumer that needs the same primitives will have
  to either inline them itself or extract a library at that point.
  Acceptable: speculative library extraction has been actively rejected
  (see "Alternatives Considered").

## Pre-archive verification

Before tagging the final release:

```
grep -rln 'felixgeelhaar/praxis\|praxis://\|PRAXIS_' \
    --exclude-dir=praxis --exclude-dir=obvia .
→ <verify zero matches; Obvia is the known consumer and has just inlined>
```

If any sibling outside Obvia references Praxis (host config, env vars,
direct Go import), that consumer must be migrated or its reference removed
before this archive proceeds.

## Migration path

For Obvia: see the Obvia inline migration PR. Replace
`obvia/internal/application/remediation/praxis.go` (HTTP client) with the
inlined primitives plus direct vendor SDK calls. Delete
`obvia/deploy/docker-compose/chaos/cmd/praxis-runtime/` stub.

For any future consumer wanting Praxis's exact shape: resurrect the code
from the `v0.3.1-final` tag. The full source (executor, idempotency,
outbox, audit, policy, plugin host, handlers) is preserved at that tag.

## Alternatives Considered

**1. Extract an `actionkit` library** (idempotency + outbox + audit +
policy + executor + Playbook executor + vendor handlers).
Rejected. The single concrete consumer (Obvia) inlines orchestration
primitives anyway; vendor handlers are the wrong shape for agent
consumers (who use MCP); a library targeting a single consumer is not
worth the maintenance overhead. See the planning conversation transcript
for the full audit of this option.

**2. Fold Praxis into Hermes** as `hermes/action`.
Rejected. Mnemos must work with any agent runtime; making the action
layer Hermes-specific contaminates the runtime-neutral architecture goal.

**3. Keep Praxis alive as a standalone "safe execution sidecar".**
Rejected. With one consumer and an agent path that doesn't need it, the
sidecar is more operational complexity than it earns back.

**4. Migrate Praxis's Playbook executor into Mnemos.**
Not applicable. Mnemos defines `Playbook` as a data structure (synthesised
from Lessons in Phase 6 / 7), but no executor was ever built. The type
stays in Mnemos as an export-only artifact; the executor concern is
shelved until a real consumer asks for it.

## Related Work

- [ADR 0003: Archive Olymp](0003-archive-olymp.md)
- [ADR 0004: Extract decisionkit](0004-extract-decisionkit.md)
- [ADR 0005: Archive Nous](0005-archive-nous.md)
