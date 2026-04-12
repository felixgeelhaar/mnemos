# Mnemos

Mnemos is a local-first knowledge engine that turns inputs into evidence-backed claims and timelines.

## Current status

This repository is in foundation stage. The current implementation provides:

- Core domain model types for inputs, events, claims, relationships, and answers
- A CLI scaffold with ingestion + event normalization for files and raw text
- SQLite-backed append-only event persistence for normalized events
- Claim extraction with event-to-claim evidence mapping
- Relationship detection across extracted claims (supports/contradicts)
- Structured CLI query output (`answer`, `claims`, `contradictions`, `timeline`)
- Workflow jobs with status transitions, timeout/retry handling, and structured JSON logs
- SQLC-based typed data-access layer for core SQLite queries
- Go toolchain setup for format, vet, test, and build

## Prerequisites

- Go 1.22+

## Quickstart

```bash
make check
make build
./bin/mnemos ingest README.md
./bin/mnemos ingest --text "Revenue decreased after launch"
./bin/mnemos extract <event-id> [event-id ...]
./bin/mnemos relate
./bin/mnemos process --text "Revenue decreased after launch. Revenue did not decrease after launch."
./bin/mnemos query --run <run-id> "What happened to our investment?"
```

Ingested events are persisted to `data/mnemos.db`.
Compilation job state is persisted to `compilation_jobs` in the same database.

Typical flow: `ingest -> extract -> relate -> query`.
Shortcut flow: `process -> query`.
Each ingest/process execution emits a `run_id`; pass it to `query --run <run-id>` to avoid cross-run context contamination.

## Commands

- `make fmt` - format Go code
- `make lint` - run static checks (`go vet`)
- `make test` - run unit tests
- `make build` - build CLI binary to `bin/mnemos`
- `make sqlc` - regenerate typed query code from `sqlc.yaml`
- `make check` - `fmt -> lint -> test -> build`

## Layout

- `cmd/mnemos` - CLI entrypoint
- `internal/domain` - core domain entities and validation
- `internal/ports` - repository and engine interfaces (ports)
- `internal/ingest` - multi-format input ingestion service
- `internal/parser` - input-to-event normalization layer
- `internal/extract` - claim extraction engine
- `internal/relate` - relationship detection engine
- `internal/query` - query assembly and ranking
- `internal/store/sqlite` - SQLite event store and repository
- `internal/workflow` - job runner with retries, timeouts, and structured logs
- `sql/sqlite` - SQL schema and sqlc query definitions
