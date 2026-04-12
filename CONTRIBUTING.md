# Contributing to Mnemos

Thank you for your interest in contributing to Mnemos!

## Getting Started

1. Fork the repository
2. Clone your fork: `git clone https://github.com/YOUR_USERNAME/Mnemos.git`
3. Install dependencies: `go mod download`
4. Build and test: `make check`

## Development Workflow

### Prerequisites

- Go 1.22+
- Make

### Running Tests

```bash
make test        # Run all tests
make check       # Format, lint, test, build
```

### Code Style

- Run `make fmt` before committing
- Follow standard Go conventions
- Add tests for new functionality

### Commit Messages

We follow [Conventional Commits](https://www.conventionalcommits.org/):

- `feat:` New features
- `fix:` Bug fixes
- `docs:` Documentation changes
- `test:` Test additions
- `refactor:` Code refactoring
- `chore:` Maintenance tasks

Example:
```
feat(extract): add confidence scoring for claims

Add heuristic-based confidence scoring that considers:
- Presence of uncertain words (lowers confidence)
- Numeric data (raises confidence)  
- Evidence keywords (raises confidence)
```

## Project Structure

```
cmd/mnemos/       # CLI entrypoint
internal/
  domain/         # Core types
  ports/          # Interfaces
  ingest/         # Input ingestion
  parser/         # Event normalization
  extract/        # Claim extraction
  relate/         # Relationship detection
  query/          # Query engine
  store/sqlite/    # SQLite persistence
  workflow/       # Job orchestration
```

## Adding Tests

Place tests in the same package as the code being tested:

```
internal/extract/engine.go
internal/extract/engine_test.go
```

Run eval tests:
```bash
cd data/eval && go test -v
```

## Reporting Issues

Bug reports and feature requests welcome! Please include:

- Go version
- Operating system
- Steps to reproduce
- Expected vs actual behavior

## Questions?

Open an issue for discussion before submitting large PRs.

---

By contributing, you agree to abide by our [Code of Conduct](./CODE_OF_CONDUCT.md).
