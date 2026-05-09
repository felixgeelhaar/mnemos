# Mutation testing

Mnemos ships an in-tree mutation harness at `tools/mutate`. It exists
to close the gap between line coverage (which is high — ~90%+ across
critical packages) and *meaningful* coverage. A mutation surfaces every
boundary that no test exercises in a way that would catch a flipped
operator.

## What it does

For every binary operator (`>`, `<`, `>=`, `<=`, `==`, `!=`, `&&`, `||`)
in the target package's non-test source, the harness:

1. Parses the file and locates the operator's byte position.
2. Generates a temp copy with the operator flipped to a sibling
   (`> ↔ <`, `>= ↔ <=`, `== ↔ !=`, `&& ↔ ||`).
3. Runs `go test -overlay=<map>` against the package, where the overlay
   substitutes the original file with the mutated copy. Source files
   on disk are never modified.
4. Records the test exit code: non-zero → mutant **killed**, zero →
   mutant **survived**.

A surviving mutant is a coverage hole: the test suite cannot tell the
mutated code from the original.

## Why in-tree, not go-mutesting / gremlins

`go-mutesting` panics on Go 1.24+ inside its vendored `golang.org/x/tools`
(stale `go/types` walk). Forks (avito, gremlins) require external
binary install, which the build environment does not auto-permit.
A ~250-line in-tree harness using the standard `go/parser` package and
`go test -overlay` is simpler, deterministic, and version-pinned with
the rest of the codebase.

The harness only mutates comparison and logical operators on purpose.
Arithmetic mutations (`+ ↔ -`) tend to produce uncompilable code in a
typed language and bias the kill rate upward without adding signal.

## Thresholds

| Package          | Threshold | Status    |
| ---------------- | --------- | --------- |
| `internal/trust` | 0.70      | Gating    |
| `internal/relate`| —         | Advisory  |
| `internal/query` | —         | Advisory  |

`internal/trust` is gated because its operators are all decision
boundaries on a continuous score: a flipped `>=` directly produces
silently-wrong rankings. The other two packages currently sit below
0.70 (relate ~0.62, query ~0.55) — they will move to gating once tests
are tightened. Do not lower the trust threshold to ratchet them up.

## Running locally

```sh
# gating run on internal/trust
make mutation

# advisory runs on relate / query (non-zero exit only on baseline failure)
make mutation-relate
make mutation-query

# arbitrary package
go run ./tools/mutate -pkg ./internal/<pkg> -threshold 0.70 -v
```

The `-v` flag prints per-mutant progress. The `-json <path>` flag emits
a structured report for CI.

## Killing a survivor

When the harness reports a survivor:

```
SURVIVED internal/trust/credibility.go:194:34 >-><  (gt-to-lt)
```

Open that line, ask: *what value would change if the operator flipped?*
Write a test that asserts the original answer at that boundary. Pin
the comparison both `<` and `>=` of the threshold; an `if x > 5`
needs a test at `x = 5` (false branch) and `x = 6` (true branch).
The pattern in `internal/trust/mutations_test.go` is intentional —
each test pins one mutant.

## Ratchet plan

- Q3 2026: gate `internal/relate` at 0.70 once entity-overlap tests
  exercise the citation-count and same-polarity boundaries.
- Q4 2026: gate `internal/query` at 0.70 once retrieval-rank tests
  cover the BM25/cosine fallback boundary in `engine.go`.

The gate threshold for `internal/trust` does not move. Mutations
introduced by new code in trust must be killed before merge.

## CI integration

`.github/workflows/mutation.yml` runs the harness on:
- every PR that touches `internal/trust/**`, `internal/relate/**`,
  `internal/query/**`, or `tools/mutate/**` — gating on the trust
  threshold, advisory on the others.
- nightly schedule against `main`, posting the kill-rate trend to the
  workflow summary.

The job is a separate workflow from `ci.yml` so a slow mutation run
cannot block normal commits.
