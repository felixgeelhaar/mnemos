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
| `mnemos query --llm <question>` | Query with LLM-grounded answer generation |
| `mnemos metrics` | Knowledge base statistics |
| `mnemos mcp` | Start MCP server over stdio |
| `mnemos serve [--port N]` | Start read-only HTTP registry (default `:7777`) |

### HTTP Registry (Phase 2B preview)

`mnemos serve` exposes the local knowledge base as a read-only HTTP API so other tools, dashboards, or scripts can pull claims and relationships without speaking SQLite. Push semantics, namespacing, and authentication land in subsequent commits — this first cut is intentionally read-only.

| Endpoint | Description |
|---|---|
| `GET /health` | Liveness probe + version |
| `GET /v1/events` | List events (`?limit`, `?offset`) |
| `GET /v1/claims` | List claims (`?type=fact\|hypothesis\|decision`, `?status=active\|contested\|deprecated`, `?limit`, `?offset`) |
| `GET /v1/relationships` | List relationships (`?type=supports\|contradicts`, `?limit`, `?offset`) |
| `GET /v1/metrics` | Counts mirroring `mnemos metrics` |

Defaults: `limit=50`, capped at `200`. Port can also be set via `MNEMOS_SERVE_PORT`.

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
| `MNEMOS_LLM_BASE_URL` | Custom endpoint (required for `openai-compat`) |
| `MNEMOS_EMBED_PROVIDER` | Embedding provider (falls back to `LLM_PROVIDER`) |
| `MNEMOS_EMBED_API_KEY` | Embedding API key (falls back to `LLM_API_KEY`) |

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
