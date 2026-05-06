# Comparison

A direct, dignified comparison against the AI-memory category. Where Mnemos doesn't lead, we say so — anything else corrodes the trust the rest of the docs build.

## Approach (what category)

| Approach | Best for | Trade-off |
|---|---|---|
| **Hosted AI memory services** | Fast onboarding, consumer apps, no data residency constraints | Vendor cloud, per-call billing, customer data leaves your infra |
| **Vector DBs** (Pinecone, Chroma, Weaviate) | Pure semantic search across raw chunks | No claim/contradiction structure, no evidence trace, no replay |
| **Notes apps** (Notion, Obsidian, Roam) | Humans organising their thinking | Not built for programmatic AI memory writes at scale |
| **Mnemos** | AI memory in stacks that can't leave your servers — regulated, on-prem, air-gapped | You run a binary |

## Feature matrix

| Capability | mem0 | zep / Graphiti | letta | Mnemos |
|---|---|---|---|---|
| Open source | ⚠️ partial (server is, hosted product isn't) | ✓ Graphiti | ✓ | ✓ MIT |
| Self-hostable as single binary | ✗ | ✗ (needs graph DB) | ✗ (Python service) | ✓ Go binary |
| Multi-backend storage | ⚠️ Postgres + vector store of choice | ✗ Neo4j / FalkorDB / Kuzu | ✗ Postgres + vector | ✓ SQLite / Postgres / MySQL / libSQL / memory |
| Typed claim model | ✗ | ✗ | ✗ | ✓ fact / hypothesis / decision |
| Contradiction detection | ✗ (graph store removed) | ⚠️ implicit via edge invalidation | ✗ | ✓ rule-based polarity / numeric / entity / temporal |
| Evidence-back-to-source | ✗ | ⚠️ episode pointer | ✗ | ✓ first-class |
| Replay-by-run-id | ✗ | ✗ | ✗ | ✓ first-class |
| Bi-temporal model | ✗ | ✓ valid_at + invalid_at + created_at + expired_at | ✗ | ✓ valid_from/to + created_at |
| Hierarchical agent memory | ✗ | ✗ | ✓ core / recall / archival | ⚠️ via status lifecycle |
| MCP server | ⚠️ via OpenMemory project | ✓ | ✓ | ✓ |
| Hybrid retrieval (BM25 + cosine + graph) | ✓ | ✓ | ⚠️ embedding only | ✓ |
| Hosted offering | ✓ | ✓ Zep Cloud only | ✓ Letta Cloud | ✗ self-host only |
| First-party Python SDK | ✓ | ✓ | ✓ | ✓ |
| First-party TypeScript SDK | ✓ | ✓ | ✓ | ✓ |
| First-party Go SDK | ✗ | ✓ | ✗ | ✓ |
| Published cross-product benchmarks | ✓ LoCoMo 91.6, LongMemEval 93.4 | ⚠️ internal claims | ⚠️ MemGPT paper | ⏳ harness shipped, numbers in progress |

## Where each one wins

- **mem0** — easiest pip-install, biggest vector-DB ecosystem, published benchmark leaderboard.
- **zep / Graphiti** — bi-temporal graph + real-time entity resolution + Context Block API.
- **letta** — agent-OS framing, hierarchical memory tiers, in-context "core memory" pinning.
- **Mnemos** — typed claims with contradiction detection, evidence-back-to-event, replay-by-run-id, true single-binary self-host.

## When to pick Mnemos specifically

You're building an AI app for a stack that:

1. **Can't ship customer data to a vendor cloud** — regulated industry, on-prem, air-gapped, or a buyer who won't sign a SOC2 dependency.
2. **Needs defensible memory** — every retrieved fact must trace to a source event, every contradiction must surface, every decision must replay months later.
3. **Wants to own the substrate** — single Go binary, your DB, MIT licence, no per-call meter.

Otherwise, hosted services do "easiest possible onboarding" better than Mnemos can. Pick the tool that matches the constraint.
