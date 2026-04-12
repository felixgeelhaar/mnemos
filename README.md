# Mnemos

**The local-first knowledge engine that eliminates AI hallucination.**

AI systems are becoming decision-makers. But they forget context, invent facts, and contradict themselves. Without a system of truth, AI cannot be reliable.

Mnemos is the system that makes knowledge reliable—for humans and AI.

## The Problem

```
$67.4B — the annual cost of AI hallucination (2026)
51% — of enterprise AI responses contain fabrications on RAG data
52% — of organizations report significant negative consequences from AI inaccuracies
```

RAG alone reduces hallucination by 40-71%. But RAG on ungoverned data? 52% fabrication rate. The problem isn't the model—it's the data layer.

**"Confident wrong answers" are worse than uncertain ones.** Users don't verify cited outputs.

## The Solution

Mnemos introduces an evidence-backed knowledge layer:

```
inputs → claims → evidence → contradictions → truth
```

Every claim is traceable to its source. Contradictions are surfaced, not buried. Knowledge evolves instead of decays.

## Key Features

- **Evidence-backed claims** — Every extracted claim maps to source material
- **Contradiction detection** — Automatically surface conflicting information  
- **Local-first** — Your data stays on your machine
- **Multi-provider extraction** — Anthropic, OpenAI, Gemini, Ollama, and OpenAI-compatible endpoints
- **Developer-friendly** — CLI-first, JSON output, MCP-ready, pipeline-friendly

## Quickstart

```bash
# Install from source
make install

# Process any text and immediately query
mnemos process --text "We decided to use PostgreSQL. The team prefers MySQL."
mnemos query --human "What database should we use?"

# Ingest documents
mnemos ingest PRD.md
mnemos ingest --text "Revenue grew 25% after the launch."

# Query with evidence
mnemos query "What decisions were made about our tech stack?"

# Start the MCP server over stdio
mnemos-mcp
```

If you do not want to install globally, run `make build` and use `./bin/mnemos` plus `./bin/mnemos-mcp`.

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

## Usage Examples

```bash
# Rule-based extraction
mnemos process --text "Q2 revenue grew 18%. We will expand to Germany next quarter."

# LLM extraction with a cloud provider
export MNEMOS_LLM_PROVIDER=openai
export MNEMOS_LLM_API_KEY=...
mnemos process --llm --text "Customers may prefer annual billing. We decided to test it in Q3."

# Local extraction with Ollama
export MNEMOS_LLM_PROVIDER=ollama
export MNEMOS_LLM_MODEL=llama3.2
mnemos process --llm --text "The roadmap now prioritizes API reliability over new UI work."

# Semantic query with embeddings
export MNEMOS_EMBED_PROVIDER=openai
export MNEMOS_EMBED_API_KEY=...
mnemos process --llm --embed --text "Support tickets dropped after the onboarding rewrite."
mnemos query --embed --human "What improved after onboarding changed?"
```

## Why Mnemos?

| | Traditional RAG | Mnemos |
|---|---|---|
| Claims traced to evidence | ❌ | ✅ |
| Contradictions surfaced | ❌ | ✅ |
| Local-first / private | ❌ | ✅ |
| No hallucination | ❌ | ✅ |
| Evolves over time | ❌ | ✅ |

## Status

Phase 1: Developer Primitive — Available now.

Shipped in Phase 1:

- Rule-based extraction with eval coverage
- LLM-powered extraction with multi-provider support
- Embeddings for semantic search
- CLI + MCP server entrypoints

Current focus: hardening release, packaging, and team-ready workflows.

## Social Proof

- Built against 78 extraction eval cases
- Supports local-first and hosted model providers with the same CLI
- Uses the same `mcp-go` framework as Roady-style MCP tooling for typed server integration

## Architecture

```
cmd/mnemos           # CLI entrypoint
internal/
  domain/            # Core types: Event, Claim, Relationship
  ports/             # Interfaces for engines and repositories
  ingest/            # Multi-format input ingestion
  parser/            # Input-to-event normalization
  extract/           # Claim extraction with evidence mapping
  relate/            # Relationship detection (supports/contradicts)
  query/             # Query assembly and ranking
  store/sqlite/      # SQLite event store
  workflow/          # Job runner with retries and structured logs
```

## Prerequisites

- Go 1.22+

## Commands

| Command | Description |
|---------|-------------|
| `make check` | Format, lint, test, build |
| `make build` | Build CLI binary |
| `make install` | Install `mnemos` and `mnemos-mcp` |
| `make test` | Run tests (includes 78 eval cases) |
| `mnemos ingest <file>` | Ingest document |
| `mnemos ingest --text <text>` | Ingest raw text |
| `mnemos extract <event-id>...` | Extract claims from events |
| `mnemos relate` | Detect relationships |
| `mnemos process --text <text>` | Ingest + extract + relate in one step |
| `mnemos process --llm --text <text>` | Use LLM-backed extraction |
| `mnemos query <question>` | Query with evidence |
| `mnemos metrics` | Report local success metrics and knowledge counts |
| `mnemos-mcp` | Start MCP server over stdio |

## Contributing

Contributions welcome. See [PRD.md](./PRD.md) for product direction and [TDD.md](./TDD.md) for technical design.

## License

MIT
