# Mnemos benchmarks

Honest, reproducible comparisons of Mnemos against other AI memory
products on tasks that matter for production agents.

## Why this exists

The AI memory market lacks shared benchmarks. Each vendor publishes
their own demo on their own data. This harness runs the **same eval
suite** against each provider through a common interface so the
numbers are comparable.

If the numbers look bad for Mnemos on something, that's a bug we
fix or a gap we document — not a result we hide. The harness is
public, the methodology is documented, and anyone can re-run it.

## Eval suites

| Suite | What it tests | Why |
|---|---|---|
| `contradiction_detection` | Feed deliberately-contradicting facts. Query. Measure whether the system surfaces the conflict or quotes one side confidently. | This is Mnemos's claimed wedge. Either it wins here or the wedge is wrong. |
| `replay_completeness` (planned) | Multi-step agent run. Ask each provider for the full chain at the end. Measure ordered-recall completeness. | "Replay months later" is the second wedge. |
| `evidence_traceability` (planned) | Each retrieved memory must point at its source event. Measure trace coverage. | Hallucination bisection. |
| `recall_at_k` (planned) | Standard semantic-recall benchmark on a shared corpus. | Sanity check. Mnemos isn't a vector DB; expect it to lose here, but by how much matters. |

## Providers tested

| Provider | Status | Notes |
|---|---|---|
| Mnemos | ✓ implemented | Local Mnemos via HTTP. |
| mem0 | planned | OSS, easy local install. |
| zep | planned | OSS version (Graphiti) used. |
| letta (MemGPT) | planned | OSS. |
| gbrain | planned | Closed; depends on API access. |
| Vector DB baseline | planned | Pinecone/Chroma + naive retrieve. |

## Run it

```bash
cd benchmarks
pip install -r requirements.txt

# All suites against Mnemos
python run.py --provider mnemos --suite all

# One suite against one provider
python run.py --provider mnemos --suite contradiction_detection

# Compare across providers (when more land)
python run.py --provider all --suite contradiction_detection
```

## Methodology

Each suite is a list of test cases. Each provider implements the
adapter contract in `providers/<name>.py`:

```python
class Provider(Protocol):
    def reset(self) -> None: ...
    def add(self, content: str, metadata: dict) -> str: ...
    def query(self, question: str) -> QueryResult: ...
```

`QueryResult` carries the answer plus structural details
(retrieved memories, contradictions surfaced, evidence ids) the
suite needs to score.

Suites score per-test, aggregate to per-suite, write JSON to
`results/<provider>-<suite>-<timestamp>.json`. A summary script
generates the markdown table that ships in launch posts.

## CI regression gate

`.github/workflows/benchmarks.yml` runs the rule-based suite on every
PR that touches `internal/relate/`, `internal/extract/`,
`internal/pipeline/`, `cmd/mnemos/`, or `benchmarks/`. It boots the
docker-compose stack, runs the harness, and compares results against
`benchmarks/baseline.json`. The workflow fails when any baselined
metric drops more than the threshold (default 0.05).

### Updating the baseline

When a change intentionally raises (or lowers) a baselined metric:

1. Run the suite locally and confirm the new number.
2. Edit `benchmarks/baseline.json`:
   - bump `f1_min` / `precision_min` / `recall_min` for the suite
   - update `evidence_commit` to the commit hash that introduces it
3. Commit the new baseline together with the underlying change.

The threshold can be tuned per-suite later if needed.

## What we will NOT do

- Cherry-pick suites where Mnemos wins.
- Bury suites where Mnemos loses.
- Compare against products on different deployment models without
  flagging the asymmetry (managed vs OSS, online vs offline, etc.).
- Pretend latency or cost are the same across providers when they
  aren't.

The point is to know the truth — including when the truth is "we're
not yet best at X."
