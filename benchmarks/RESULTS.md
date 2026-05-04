# Benchmark results

Latest run per (provider, suite) pair. Re-run any with
`python -m benchmarks.run --provider <name> --suite <name>`.

## contradiction_detection

| Provider | n | Precision | Recall | F1 | Run |
|---|---|---|---|---|---|
| **mnemos** | 5 | 0.80 | 0.80 | 0.80 | `20260504T052409Z` |

### Per-case detail — mnemos

| Case | Expected | Detected | P | R | F1 |
|---|---|---|---|---|---|
| direct_polarity_conflict | 1 | 1 | 1.00 | 1.00 | 1.00 |
| three_way_partial_conflict | 1 | 1 | 1.00 | 1.00 | 1.00 |
| no_contradictions_clean_facts | 0 | 0 | 1.00 | 1.00 | 1.00 |
| numeric_disagreement | 1 | 1 | 1.00 | 1.00 | 1.00 |
| implicit_temporal_conflict | 1 | 0 | 0.00 | 0.00 | 0.00 |
