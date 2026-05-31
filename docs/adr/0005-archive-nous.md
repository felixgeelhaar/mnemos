# ADR 0005: Archive Nous

- **Status:** Accepted
- **Date:** 2026-05-31
- **Deciders:** Felix Geelhaar
- **Scope:** Cognitive stack repository topology. Depends on ADR 0004
  (decisionkit) being in place first.

## Context

Nous was the "reasoning" primitive in the cognitive stack: a service that
ingested events, extracted commitments via LLM, scored them for risk,
decided interventions, and emitted decisions through gRPC/HTTP.

A repository audit on 2026-05-31 produced three findings:

1. **The only consumer of Nous is Olymp**, which is itself being archived
   (ADR 0003). After Olymp goes away, Nous has zero callers.
2. **The LLM extraction layer is replaceable by agent runtimes.** Frontier
   models plus tool use handle commitment extraction with comparable or
   better accuracy and no service to maintain.
3. **The deterministic value (risk scoring + intervention policy) is
   extractable into a small library**, which we are doing in ADR 0004
   (decisionkit).

Nous's gRPC/HTTP transport, multi-backend storage, LLM client abstraction,
configuration, and adapter orchestration with circuit breakers all exist
to support a service deployment. With no service deployment in our future,
all of that ceases to earn its keep.

## Decision

We will archive the Nous repository after decisionkit (ADR 0004) ships.
The repo stays publicly readable but is set to read-only on GitHub and
tagged with its final release version. Its README is replaced with a
redirect to decisionkit + mnemos, and a `DEPRECATED.md` is added at the
repo root with the rationale and the recovery path.

The following Nous components die with the repo (no extraction):

- **LLM extraction pipeline** (`internal/llm/`, `internal/pipeline/`) —
  replaced by agent tool calls. A modern agent can extract structured
  commitments from text via a single `extract_commitments` tool definition.
- **gRPC + HTTP transport** (`internal/transport/`) — services aren't the
  right shape for what's left.
- **Multi-backend storage** (`internal/store/`) — Mnemos owns event
  storage; Nous's parallel store has no consumer.
- **Adapter orchestration with circuit breakers** (`internal/adapters/`) —
  pattern is useful; consumers can use `fortify` or write their own. Not
  worth a library extraction.
- **Commitment / Decision / Intervention domain types** (`internal/domain/`) —
  consumers using decisionkit construct input shapes appropriate to their
  context. The Nous-specific shapes assumed a service deployment.

The risk and intervention engines are extracted in [ADR 0004](0004-extract-decisionkit.md)
before this archive proceeds.

## Consequences

**Positive:**

- The cognitive stack loses an LLM-heavy service that mostly wrapped
  capabilities now available in agent runtimes.
- Maintenance burden drops: no more Nous releases, no more provider
  rotations (Anthropic / OpenAI / Gemini / Bedrock / Ollama).
- The valuable parts (risk + intervention) survive in a much smaller,
  more reusable shape (decisionkit).

**Negative / risks:**

- The MVP work that shipped Nous to "production ready" (SLOs, graceful
  shutdown, observability, tracing) is sunk. Acceptable: the value of
  that work was always the production-grade pattern, not the specific
  binary, and the pattern carries forward to other services.
- A future consumer wanting Nous's exact LLM extraction shape will need
  to resurrect from the tag. Acceptable: the LLM layer is the easiest
  part to rebuild against modern providers anyway.

## Pre-archive verification

Performed at the time of decisionkit creation:

```
grep -rln '"github.com/felixgeelhaar/nous' --include='*.go' \
    --exclude-dir=nous --exclude-dir=olymp .
→ <verify zero matches before archiving; Olymp is the known consumer
   and is also archived>
```

If any sibling outside Olymp imports Nous, that consumer must be migrated
or its dependency removed before this archive proceeds.

## Migration path

For the Nous gRPC/HTTP API consumers (Olymp): no migration needed; Olymp
is also archived.

For risk + intervention scoring: see [ADR 0004](0004-extract-decisionkit.md).
Switch imports from `github.com/felixgeelhaar/nous/internal/{risk,intervention}`
to `github.com/felixgeelhaar/decisionkit/{risk,intervention}`. The factor
input shape is preserved; storage and transport are not part of the new
library.

For LLM-based commitment extraction: implement as an agent tool. A typical
shape:

```go
// Tool definition for an agent
type ExtractCommitmentsInput struct {
    Text string `json:"text"`
}
type ExtractCommitmentsOutput struct {
    Commitments []Commitment `json:"commitments"`
}
```

The agent runtime (Claude Code, Codex, Hermes, ...) handles the LLM call;
Mnemos stores the extracted commitments as claims.

## Alternatives Considered

**1. Keep Nous alive for the risk + intervention engines.**
Rejected. The 95% of Nous that's transport / storage / LLM glue isn't
worth maintaining to host two small packages. Extraction is cleaner.

**2. Fold decisionkit into Mnemos as `internal/decisions/` instead of a
separate repo.**
Rejected. See ADR 0004; risk scoring is policy, not memory.

**3. Migrate Nous's LLM extraction into a Mnemos pipeline.**
Rejected. Mnemos already has a rule-based extractor with an LLM fallback;
adding a second LLM-extraction layer with different prompts and providers
would duplicate functionality. Agent runtimes are the right home.

## Related Work

- [ADR 0003: Archive Olymp](0003-archive-olymp.md)
- [ADR 0004: Extract decisionkit](0004-extract-decisionkit.md)
- [ADR 0006: Archive Praxis](0006-archive-praxis.md)
