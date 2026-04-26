# Mnemos

**The local-first evidence layer that grounds AI in truth.**

Your RAG system hallucinates because your data layer has no concept of evidence or contradiction. Mnemos fixes the data layer — every claim traces to its source, and conflicting information is surfaced instead of buried.

## The Problem

```
$67.4B — the annual cost of AI hallucination (2026)
51% — of enterprise AI responses contain fabrications on RAG data
52% — of organizations report significant negative consequences from AI inaccuracies
```

RAG alone reduces hallucination by 40-71%. But RAG on ungoverned data? 52% fabrication rate. The problem isn't the model—it's the data layer.

**"Confident wrong answers" are worse than uncertain ones.** Users don't verify cited outputs.

## 5-Minute Quickstart

### 1. Install

```bash
# macOS / Linux (Homebrew)
brew tap felixgeelhaar/tap && brew install mnemos

# Go (any platform with Go 1.25+)
go install github.com/felixgeelhaar/mnemos/cmd/mnemos@latest
go install github.com/felixgeelhaar/mnemos/cmd/mnemos@latest

# Docker
docker run --rm ghcr.io/felixgeelhaar/mnemos --version

# From source
git clone https://github.com/felixgeelhaar/mnemos.git && cd mnemos && make install
```

### 2. Process text — extract claims and detect contradictions

```bash
mnemos process --text "The deployment succeeded in production. The deployment did not succeed in production. Response times averaged 45ms."
```

Mnemos extracts three claims, detects the contradiction between the first two, and flags them as contested.

### 3. Query with evidence

```bash
mnemos query "What happened with the deployment?"
```

The answer comes with the source claims, confidence scores, and surfaced contradictions — so you know what's true and what's contested.

### 4. Try with your own documents

```bash
mnemos process meeting-notes.md
mnemos query "What decisions were made?"
```

No API keys required — rule-based extraction and contradiction detection work out of the box.

### Recommended: Add an LLM provider for best results

For querying real documents, set up an LLM provider. This enables semantic search, better extraction, and grounded answers:

```bash
export MNEMOS_LLM_PROVIDER=openai   # or: anthropic, gemini, ollama, openai-compat
export MNEMOS_LLM_API_KEY=sk-...

# LLM extraction + embeddings + grounded query answers
mnemos process --llm --embed meeting-notes.md
mnemos query --llm "What decisions were made?"
```

Without a provider, extraction and contradiction detection still work via rule-based heuristics. Queries use BM25 keyword matching, which works well for simple questions but may miss nuance on longer documents.

### Optional: MCP server for AI agents

```bash
mnemos mcp   # Exposes query_knowledge, process_text, and knowledge_metrics over stdio
```

## How It Works

```
┌─────────────┐    ┌─────────────┐    ┌─────────────┐    ┌─────────────┐
│   Ingest   │ -> │  Extract    │ -> │   Relate    │ -> │    Query    │
│  (events)  │    │  (claims)   │    │ (evidence)  │    │   (truth)   │
└─────────────┘    └─────────────┘    └─────────────┘    └─────────────┘
```

**Extract** — Turns raw text into structured claims (facts, decisions, hypotheses)
**Relate** — Detects support and contradiction relationships between claims
**Query** — Returns answers with claims, evidence, and surfaced contradictions

## Example Output

```json
{
  "answer": "Tech stack decisions show contradiction: PostgreSQL vs MySQL",
  "claims": [
    {"text": "We decided to use PostgreSQL", "type": "decision", "confidence": 0.88},
    {"text": "The team prefers MySQL", "type": "fact", "confidence": 0.75}
  ],
  "contradictions": [
    {"from": "claim-1", "to": "claim-2", "type": "contradicts"}
  ]
}
```

## Why Mnemos?

| | Traditional RAG | Mnemos |
|---|---|---|
| Claims traced to evidence | ❌ | ✅ |
| Contradictions surfaced | ❌ | ✅ |
| Local-first / private | ❌ | ✅ |
| Grounded in governed data | ❌ | ✅ |
| Evolves over time | ❌ | ✅ |

## Key Features

- **Evidence-backed claims** — Every extracted claim maps to source material
- **Contradiction detection** — Automatically surface conflicting information
- **Local-first** — Your data stays on your machine (`~/.local/share/mnemos/`)
- **Multi-provider extraction** — Anthropic, OpenAI, Gemini, Ollama, and OpenAI-compatible endpoints
- **Developer-friendly** — CLI-first, JSON output, MCP-ready, pipeline-friendly

## Commands

| Command | Description |
|---------|-------------|
| `mnemos process <path or --text>` | Ingest + extract + relate in one step |
| `mnemos process --llm --text <text>` | Use LLM-backed extraction |
| `mnemos ingest <file>` | Ingest document as events |
| `mnemos extract --run <run-id>` | Extract claims from a run's events |
| `mnemos relate` | Detect relationships between claims |
| `mnemos query <question>` | Query with evidence |
| `mnemos query --hops <N> <question>` | Expand result claims by N supports/contradicts hops (max 5) |
| `mnemos query --llm <question>` | Query with LLM-grounded answer generation |
| `mnemos metrics` | Knowledge base statistics |
| `mnemos audit [--include-embeddings]` | Export the full knowledge base as JSON for compliance/backup |
| `mnemos resolve <winner> --over <loser> [--reason "..."]` | Resolve a contradiction: winner → resolved, loser → deprecated |
| `mnemos resolve <new> --supersedes <old> [--reason "..."]` | Temporal supersession: close `old.valid_to` at `new.valid_from`. Old claim keeps its status — it remained true while it was true. |
| `mnemos query --at YYYY-MM-DD "..."` | Point-in-time query against the temporal-validity layer |
| `mnemos query --include-history "..."` | Include superseded claims in the answer set (off by default) |
| `mnemos query --entity <name\|id> "..."` | Restrict the answer to claims linked to this entity |
| `mnemos entities list [--type T]` | List canonicalised entities (people/orgs/projects/...) |
| `mnemos entities show <name\|id>` | Show one entity and the claims linked to it |
| `mnemos entities merge <winner> <loser>` | Collapse one entity into another (manual canonicalisation) |
| `mnemos extract-entities [--all]` | Backfill entity links for claims that predate the v0.9 prompt |
| `mnemos reset [--keep-events] [--yes]` | Wipe claims/relationships/embeddings (events optional) |
| `mnemos delete-claim <id>...` | Delete specific claims and their derived state |
| `mnemos delete-event <id>...` | Delete events and cascade to derived claims |
| `mnemos reembed [--force] [--dry-run]` | (Re)generate claim embeddings under the current embed config |
| `mnemos recompute-trust` | Rebuild `trust_score` for every claim (confidence × corroboration × freshness) |
| `mnemos dedup [--threshold T] [--force]` | Merge near-duplicate claims by embedding cosine similarity (dry-run by default) |
| `mnemos query --min-trust X "..."` | Only return claims whose `trust_score` ≥ X |
| `mnemos process --no-relate ...` | Skip the relate stage for fast ingest; relate later in batch |

### Claim lifecycle

Every claim carries a status: `active`, `contested`, `resolved`, or `deprecated`. Status changes are recorded in `claim_status_history` (from, to, when, why) so the lifecycle of every claim is auditable. When a query surfaces a claim whose status changed at some point, the answer text includes an `Evolution:` line summarizing the timeline — e.g. _"Transitioned from contested to resolved on 2026-04-18 (evidence review by jane)."_
| `mnemos mcp` | Start MCP server over stdio |
| `mnemos serve [--port N]` | Start HTTP registry server (default `:7777`) |
| `mnemos registry connect <url>` | Wire this project to a remote registry |
| `mnemos push` | Send local knowledge to the registry |
| `mnemos pull` | Fetch knowledge from the registry into the local DB |

### HTTP Registry (Phase 2B)

`mnemos serve` exposes the local knowledge base as a small HTTP API so other tools, dashboards, or scripts can read and write without speaking SQLite. Cross-project federation and namespace scoping land in subsequent commits.

| Endpoint | Method | Description |
|---|---|---|
| `/health` | GET | Liveness probe + version |
| `/v1/events` | GET | List events (`?limit`, `?offset`) |
| `/v1/events` | POST | Append a batch of events |
| `/v1/claims` | GET | List claims (`?type=fact\|hypothesis\|decision`, `?status=active\|contested\|resolved\|deprecated`, `?limit`, `?offset`) |
| `/v1/claims` | POST | Upsert a batch of claims (with optional `evidence` links) |
| `/v1/relationships` | GET | List relationships (`?type=supports\|contradicts`, `?limit`, `?offset`) |
| `/v1/relationships` | POST | Upsert a batch of relationships |
| `/v1/embeddings` | GET | List embeddings (`?entity_type=event\|claim`, `?limit`, `?offset`) |
| `/v1/embeddings` | POST | Upsert a batch of embeddings (vector as JSON float array) |
| `/v1/metrics` | GET | Counts mirroring `mnemos metrics` |

Defaults: `limit=50`, capped at `200`. Port also accepts `MNEMOS_SERVE_PORT`. Request bodies cap at 5 MB.

**Web UI.** `mnemos serve` also serves a minimal single-page UI at `GET /`. It renders the metrics, paginated claims (with type/status filters), and the contradiction list by hitting the same `/v1/*` endpoints above. The HTML is embedded via `//go:embed` so there's no separate deploy step — one binary, one port.

**Authentication.** Set `MNEMOS_REGISTRY_TOKEN=<your-secret>` to require `Authorization: Bearer <your-secret>` on all write methods (POST/PUT/DELETE). Reads stay open by default — useful for browse-only dashboards. When the env var is unset, the registry is fully open (suitable for local dev and trusted networks).

### Integrating Mnemos in your app

The HTTP API at `mnemos serve` is the integration surface. Three flavors:

**Go (typed client)** — `import "github.com/felixgeelhaar/mnemos/client"`:

```go
c := client.New("http://localhost:7777",
    client.WithToken("optional-secret"),
    client.WithLogger(logger),               // *bolt.Logger
    client.WithRetry(retry.Config{           // fortify retry; 5xx + 429 retry, 4xx fail fast
        MaxAttempts:   3,
        InitialDelay:  200 * time.Millisecond,
        MaxDelay:      time.Second,
        BackoffPolicy: retry.BackoffExponential,
        Jitter:        true,
    }),
)

// Write
c.Events().Append(ctx, []client.Event{{
    ID: "ev_1", RunID: "session-A", SchemaVersion: "v1",
    Content: "We chose Postgres for the new service",
    SourceInputID: "src_1", Timestamp: client.FormatTime(time.Now()),
}})

// Read with chained filters
list, _ := c.Claims().Type("decision").Status("active").Limit(25).List(ctx)
for _, claim := range list.Claims {
    fmt.Printf("[%s] %s\n", claim.Type, claim.Text)
}
```

Resource accessors (`Events()`, `Claims()`, `Relationships()`, `Embeddings()`) return fluent builders. Filter methods chain; terminal `List(ctx)` reads, `Append(ctx, ...)` writes. Non-2xx responses return `*client.APIError` with the server's status and message; works with `errors.As`. Built-in `bolt` request logging and `fortify` retry-with-backoff. Safe for concurrent use.

**Any other language (curl)**:

```bash
# Append an event
curl -X POST http://localhost:7777/v1/events \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer <optional-token>' \
  -d '{"events":[{"id":"ev_1","content":"...","timestamp":"2026-04-19T10:00:00Z"}]}'

# Browse claims, filtered
curl 'http://localhost:7777/v1/claims?type=decision&limit=25'
```

**Python (stdlib)**:

```python
import json, urllib.request

req = urllib.request.Request(
    "http://localhost:7777/v1/events",
    data=json.dumps({"events": [{"id": "ev_1", "content": "...", "timestamp": "2026-04-19T10:00:00Z"}]}).encode(),
    headers={"Content-Type": "application/json", "Authorization": "Bearer <token>"},
)
urllib.request.urlopen(req)
```

For an AI agent: skip the HTTP API entirely and use the MCP transport (`mnemos mcp`). Agents that speak MCP get the same surface plus `query_knowledge`, `process_text`, browsing, file watching, and git ingestion already wired up.

### Push / Pull

Once a project is connected to a registry, knowledge flows like git:

```bash
mnemos registry connect https://registry.example.com --token <secret>
mnemos push                       # send local events/claims/relationships
mnemos pull                       # fetch remote knowledge into the local DB
```

`registry connect` writes `.mnemos/config.json`. Resolution precedence for `push`/`pull` is **CLI flags (`--url`, `--token`) > env vars (`MNEMOS_REGISTRY_URL`, `MNEMOS_REGISTRY_TOKEN`) > config file**, so CI can override per-job without editing the file.

Sync is idempotent — IDs are the dedup key, so running `push`/`pull` twice is safe. Vectors transfer too: embeddings ride on the same wire as JSON float arrays and round-trip bit-exact, so semantic ranking on pulled content works without re-embedding. Claim-evidence links travel with the claims so the local query engine can resolve pulled claims back to their source events.

**Federation provenance.** Pulled events get stamped with `pulled_from_registry: <url>` in their metadata. The query engine surfaces this in the answer text — claims sourced from a registry appear as `… (from https://reg.example.com)` and the summary line counts them: `Context used 5 event(s) and 8 claim(s) (3 from a connected registry).` Local claims are unmarked (the no-registry case stays uncluttered). The `claim_provenance` field on the MCP `query_knowledge` response carries the same map programmatically.

## Architecture

```
cmd/mnemos           # CLI entrypoint
internal/
  domain/            # Core types: Event, Claim, Relationship, EmbeddingRecord
  ports/             # Interfaces for engines and repositories
  pipeline/          # Shared orchestration (extraction, persistence, embeddings)
  ingest/            # Multi-format input ingestion
  parser/            # Input-to-event normalization
  extract/           # Claim extraction with evidence mapping
  relate/            # Relationship detection (supports/contradicts)
  query/             # Query assembly and ranking
  embedding/         # Vector embedding client abstraction
  llm/               # LLM client abstraction (multi-provider)
  store/sqlite/      # SQLite event store (sqlc-generated queries)
  workflow/          # Job runner with retries and structured logs
```

## Environment Variables

| Variable | Description |
|----------|-------------|
| `MNEMOS_DB_PATH` | Database path (default: `~/.local/share/mnemos/mnemos.db`) |
| `MNEMOS_LLM_PROVIDER` | `anthropic`, `openai`, `gemini`, `ollama`, `openai-compat` |
| `MNEMOS_LLM_API_KEY` | API key (required for cloud providers) |
| `MNEMOS_LLM_MODEL` | Model override (optional) |
| `MNEMOS_LLM_BASE_URL` | Custom endpoint. Required for `openai-compat`. **Required for `ollama` when Mnemos is not on the same host as the Ollama daemon** — most commonly when Mnemos runs in a container and Ollama runs on the host (`http://host.docker.internal:11434` on Docker Desktop, `http://172.17.0.1:11434` on Linux). Defaults to `http://localhost:11434` for `ollama`. |
| `MNEMOS_LLM_TIMEOUT` | Per-request LLM HTTP timeout (default `120s`). Bump for slow local models or large completions: `MNEMOS_LLM_TIMEOUT=5m`. |
| `MNEMOS_EXTRACT_MODEL` | Override `MNEMOS_LLM_MODEL` just for the extract stage. Lets you pair a strong model for extraction with a smaller model elsewhere. |
| `MNEMOS_JOB_TIMEOUT` | Overall workflow-job deadline (default `10m`). Raise this if your provider is slow enough that an entire `process` run exceeds 10 minutes. |
| `MNEMOS_EMBED_PROVIDER` | Embedding provider (falls back to `LLM_PROVIDER`) |
| `MNEMOS_EMBED_API_KEY` | Embedding API key (falls back to `LLM_API_KEY`) |
| `MNEMOS_EMBED_MODEL` | Embedding model override (optional) |
| `MNEMOS_EMBED_BASE_URL` | Embedding endpoint (same container/host caveat as `MNEMOS_LLM_BASE_URL`) |
| `MNEMOS_EMBED_TIMEOUT` | Per-request embedding HTTP timeout (default `60s`) |
| `MNEMOS_AUTH_DIR` | Directory for the JWT signing secret (default: project `.mnemos/` or `$HOME/.mnemos/`). Override when running on a read-only rootfs (Docker `read_only: true`, k8s `readOnlyRootFilesystem: true`) by pointing at a writable volume. |
| `MNEMOS_JWT_SECRET` | Hex-encoded JWT signing secret (≥32 bytes). When set, takes precedence over the file path; useful in CI/Kubernetes where you'd rather inject the secret as an env var than mount a file. |

### Trust scoring (v0.7+)

Every claim carries a `trust_score ∈ [0, 1]` derived from three
signals the LLM cannot fake:

```
trust = confidence × corroboration × freshness

corroboration = 1 + ln(evidence_count) × 0.2     # 1 source: 1.0; 5: 1.32; 20: 1.60
freshness     = max(0.3, exp(-days_since_latest / 90))   # 90-day half-life, floor 0.3
```

The score is recomputed automatically after every `process` run; you
can rebuild it manually with `mnemos recompute-trust` (e.g., after
upgrading or tuning the constants in `internal/trust`).

`mnemos query --min-trust 0.5 "..."` filters out low-confidence
results before ranking. `mnemos metrics` reports `avg_trust` and
`low_trust_count` for at-a-glance corpus quality. The
trust-scoring policy lives in `internal/trust/trust.go` — change
the constants there to retune for your corpus, then run
`mnemos recompute-trust` to backfill.

### Entity layer (v0.9+)

Mnemos canonicalises noun-phrases ("Felix Geelhaar", "Acme",
"PostgreSQL", "Berlin", ...) into first-class entity nodes that
exist independently of any one claim. Once entities exist you can
ask entity-scoped questions:

```bash
mnemos entities list --type person
mnemos entities show "Felix Geelhaar"
mnemos query --entity "Felix Geelhaar" "what does he need this week?"
```

How they get created. The v1.4 LLM extraction prompt tags every
claim with the named entities it mentions. After `mnemos process`
persists claims, the pipeline materialises those tags into the
`entities` and `claim_entities` tables, deduping by
(normalized_name, type) so "Felix", "felixgeelhaar", and
"Felix Geelhaar" land on different ids only if the LLM gives them
different names — manual canonicalisation closes the gap:

```bash
mnemos entities merge en_abc123 en_def456   # winner absorbs loser
```

For databases that pre-date v0.9 (claims extracted under the v1.3
prompt or earlier), `mnemos extract-entities --all` re-runs the
LLM over stored claim text to backfill entity links. It batches
through the LLM cache, so a re-run on the same content is free.

### Temporal validity (v0.8+)

Every claim carries a validity interval — `valid_from` (when the
fact became true) and `valid_to` (when it stopped being true; NULL
means "still in force"). The pipeline derives `valid_from` from the
earliest evidence event's timestamp at insert time, so backfilled
ingest gets correct timelines without operator effort.

Two new behaviors fall out:

- **Default queries hide superseded claims.** `mnemos query "..."`
  filters out claims whose `valid_to` is in the past, so "Felix is
  a junior engineer" stops surfacing once "senior engineer" closes
  its interval. Pass `--include-history` to see both.
- **Point-in-time queries.** `mnemos query --at 2026-03-01 "..."`
  returns the answer as it would have been on that date — handy for
  audit trails and "what did we believe at the time?" questions.

To close one claim's interval when a new one takes its place:

```bash
mnemos resolve cl_new --supersedes cl_old --reason "promoted 2026-04"
```

`--supersedes` is distinct from `--over` (the contradiction
resolver). `--over` says "one of these was always wrong and the
other right"; `--supersedes` says "this fact changed". The latter
preserves the old claim's status — it remained true while it was
true — and only sets `valid_to`. Auto-supersession (heuristic
detection without operator action) is on the v0.9 roadmap.

### Upgrading

Mnemos v0.6.1+ auto-migrates older databases on `sqlite.Open` — no
`mnemos reset` or DB delete needed. The migration is idempotent and
adds the `created_by` / `changed_by` columns the auth feature
introduced in v0.6.0. Schema generation is tracked via
`PRAGMA user_version`, so re-running on an already-migrated DB is a
no-op.

### Local Models (Ollama)

Tested combinations and known quirks. The extract pipeline is tolerant to
common reasoning-model output (`<think>` blocks, prose preambles, ` ```json `
fences), so most models work; the table calls out exceptions.

| Model | LLM | Embed | Notes |
|---|---|---|---|
| `llama3.2:latest` | ✅ | — | Fast, reliable, clean JSON |
| `mistral:latest` | ✅ | — | Fast; occasional prose preamble (handled) |
| `qwen3:*` | ✅ | — | Emits `<think>...</think>` blocks (stripped automatically); pair with `MNEMOS_LLM_TIMEOUT=2m+` if reasoning is long |
| `deepseek-r1:*` | ✅ | — | Same reasoning-block handling as qwen3 |
| `gpt-oss:20b` | ⚠️ | — | Strong structured output but slow on consumer hardware → set `MNEMOS_LLM_TIMEOUT=5m` and `MNEMOS_JOB_TIMEOUT=15m` |
| `gemma3:*` | ❌ | — | No tool/structured-output support in current Ollama builds |
| `nomic-embed-text` | — | ✅ | 768-dim embeddings, fast; the recommended local embed model |

Quick start for fully-local Mnemos:

```bash
ollama pull llama3.2 nomic-embed-text
export MNEMOS_LLM_PROVIDER=ollama
export MNEMOS_LLM_MODEL=llama3.2
export MNEMOS_EMBED_MODEL=nomic-embed-text
mnemos process --llm --embed --text "Your knowledge here"
```

**Container note.** When Mnemos runs in Docker/Podman and Ollama runs on
the host, set `MNEMOS_LLM_BASE_URL=http://host.docker.internal:11434`
(Docker Desktop) or `http://172.17.0.1:11434` (Linux Docker). The default
`http://localhost:11434` resolves to the container itself and will fail
with connection refused.

## Development

```bash
make check          # Format, lint, test, build (CI equivalent)
make build          # Build bin/mnemos
make test           # Run tests (includes 102 eval cases)
make sqlc           # Regenerate sqlc query code
make release-check  # Validate GoReleaser config
```

## Status

Phase 1: Developer Primitive — Available now.

- Rule-based and LLM-powered extraction with eval coverage
- Embeddings for semantic search
- CLI + MCP server entrypoints
- 78 extraction eval cases

## Contributing

Contributions welcome. See [PRD.md](./PRD.md) for product direction and [TDD.md](./TDD.md) for technical design.

## Releases

Tagged releases are published with GoReleaser via `.github/workflows/release.yml`, including Homebrew formula updates and Docker images.

## License

MIT
