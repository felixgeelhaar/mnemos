# ADR 0003: Archive Olymp

- **Status:** Accepted
- **Date:** 2026-05-31
- **Deciders:** Felix Geelhaar
- **Scope:** Cognitive stack repository topology. No code changes inside Mnemos.

## Context

The cognitive stack was historically organised around five primitives:

- Mnemos — memory
- Chronos — time / events
- Nous — reasoning
- Praxis — action
- Olymp — orchestration

Olymp implemented a closed-loop finite state machine
(`observe → understand → decide → act → learn`) wired across the other four
tools, with runtime steering (pause/approve/cancel), federation via a
`plugin.Peer` interface, a typed intent registry, provenance auditing, and
an MCP surface (`submit_intent`, `inspect_run`, `steer_run`, `halt`, ...).

A repository audit on 2026-05-31 produced two findings that change the
calculus for keeping Olymp alive:

1. **Reasoning has moved into the agent layer.** Frontier models plus modern
   agent SDKs (Claude Code, Codex, Hermes, Nomi, OpenClaw, NanoClaw, ...)
   subsume the orchestration patterns Olymp was built to provide. Single-
   agent tool use, agent subagents, and external frameworks like CrewAI /
   AutoGen / LangGraph cover the 80% case without a separate runtime.
2. **Olymp has zero Go importers anywhere in the org.** Grep across all
   `business-felix-geelhaar/*/go.mod` and `*.go` files outside Olymp itself
   returns zero matches. Olymp is referenced narratively in some sibling
   READMEs but no code depends on it.

Olymp also remains coupled to Nous (its decision port) and Praxis (its
action port), both of which we are independently archiving (ADRs 0005 and
0006). Keeping Olymp alive would require finding it new ports, with no
existing consumer asking for them.

## Decision

We will archive the Olymp repository. The repo stays publicly readable but
is set to read-only on GitHub and tagged `v0.1.5-final`. Its README is
replaced with a redirect explaining the new architecture (Mnemos + Chronos
+ agent runtimes), and a `DEPRECATED.md` is added at the repo root with the
rationale and the recovery path.

We do **not** extract any Olymp code into a new library:

- The closed-loop FSM is a reusable pattern but has no current consumer.
  If a future consumer materialises, the pattern can be re-derived into
  `statekit/loop` from the archived source.
- Federation `plugin.Peer` is interesting for multi-runtime delegation but
  is solving a problem (cross-agent coordination) that the agent layer is
  expected to own going forward.
- The MCP tool surface (`submit_intent`, etc.) is design-specific to
  Olymp's intent registry. Each agent runtime will expose whatever MCP
  surface makes sense for its own loop, backed by mnemos / chronos calls.
- The provenance audit trail is partially duplicated by mnemos action /
  outcome events (Phase 2) and partially replaceable by structured agent
  transcripts.

## Consequences

**Positive:**

- The cognitive-stack story collapses from five primitives to three
  (mnemos / chronos / agent runtime). New developers can grasp it in under
  60 seconds without learning Olymp's loop model.
- No upstream consumer breaks (zero importers).
- Releases the v0.1.5 line as a reference if someone wants to fork the
  loop FSM later.

**Negative / risks:**

- We lose the prebuilt MCP surface for runtime steering. Each agent runtime
  reimplements pause/resume/approve on its own. Mitigation: the patterns
  are well documented in Olymp's README and source; we accept the cost.
- A future user wanting Olymp's exact orchestration shape will need to
  resurrect from the tag. Acceptable: the cost of revival is bounded;
  the cost of keeping a maintained-but-unused service is unbounded.

## Pre-archive verification

Pre-archive grep across `/Users/felixgeelhaar/Developer/projects/business-felix-geelhaar/*`:

```
grep -rln '"github.com/felixgeelhaar/olymp' --include='*.go' --exclude-dir=olymp .
→ 0 matches

grep -rln 'felixgeelhaar/olymp' --include='go.mod' --include='go.sum' --exclude-dir=olymp .
→ 0 matches
```

No code depends on Olymp.

## Migration path

Nothing to migrate. Olymp's adapters under `olymp/internal/adapters/{nous,praxis,mnemos,chronos}`
die with the repo (Nous and Praxis are also being archived under ADRs 0005
and 0006; Mnemos and Chronos remain).

## Alternatives Considered

**1. Keep Olymp alive as a reference implementation.**
Rejected. Maintained code rots; reference code is a tag.

**2. Extract `statekit/loop` and `statekit/federation` from Olymp now.**
Rejected. No current consumer. Extracting on speculation grows the surface
to maintain without any caller exercising the API. If a real consumer
emerges, the patterns can be lifted from the archived tag.

**3. Fold Olymp into Hermes (or another agent runtime).**
Rejected. Mnemos must work with any agent runtime, not be Hermes-specific.
Folding Olymp into Hermes contaminates the runtime-neutral architecture
goal that drove this simplification.

## Related Work

- [ADR 0004: Extract decisionkit](0004-extract-decisionkit.md)
- [ADR 0005: Archive Nous](0005-archive-nous.md)
- [ADR 0006: Archive Praxis](0006-archive-praxis.md)
