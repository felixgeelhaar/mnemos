# Benchmark results

Latest run per (provider, suite) pair. Re-run any with
`python -m benchmarks.run --provider <name> --suite <name>`.

## contradiction_detection

| Provider | n | Precision | Recall | F1 | Run |
|---|---|---|---|---|---|
| **mnemos** | 5 | 1.00 | 1.00 | 1.00 | `20260505T131540Z` |

### Per-case detail — mnemos

| Case | Expected | Detected | P | R | F1 |
|---|---|---|---|---|---|
| direct_polarity_conflict | 1 | 1 | 1.00 | 1.00 | 1.00 |
| three_way_partial_conflict | 1 | 1 | 1.00 | 1.00 | 1.00 |
| no_contradictions_clean_facts | 0 | 0 | 1.00 | 1.00 | 1.00 |
| numeric_disagreement | 1 | 1 | 1.00 | 1.00 | 1.00 |
| implicit_temporal_conflict | 1 | 1 | 1.00 | 1.00 | 1.00 |

## locomo

| Provider | n | Precision | Recall | F1 | Run |
|---|---|---|---|---|---|
| **mnemos** | 3 | 1.00 | 1.00 | 1.00 | `20260505T131513Z` |

### Per-case detail — mnemos

| Case | Answered | Matched substrings | Answer excerpt |
|---|---|---|---|
| recall_across_sessions | ✓ | march 28, april 5 | Speaker A (session 1): I'm planning a trip to Tokyo next month Speaker B (sessio |
| cross_session_preference | ✓ | japanese | Speaker A (session 1): I really love sushi Speaker A (session 2): I tried that n |
| contradicting_then_corrected | ✓ | green | Speaker A (session 1): My favorite color is blue Speaker A (session 5): Actually |

## longmemeval

| Provider | n | Precision | Recall | F1 | Run |
|---|---|---|---|---|---|
| **mnemos** | 3 | 1.00 | 1.00 | 1.00 | `20260505T131505Z` |

### Per-case detail — mnemos

| Case | Answered | Matched substrings | Answer excerpt |
|---|---|---|---|
| single_session_simple_recall | ✓ | blue | User said their favorite color is blue User asked the agent about restaurants Us |
| multi_fact_dietary_constraint | ✓ | vegetarian, peanut | User mentioned they are vegetarian User asked about restaurants in their neighbo |
| contradicting_then_resolved_preference | ✓ | coffee | User said they prefer tea User later said they actually prefer coffee |

## real_trace_recall

| Provider | n | Precision | Recall | F1 | Run |
|---|---|---|---|---|---|
| **mnemos** | 2 | 0.50 | 0.50 | 0.50 | `20260505T194348Z` |

### Per-case detail — mnemos

| Case | Split | Critical | Passed | Missing substrings |
|---|---|---|---|---|
| rt-005 | holdout | yes | ✓ |  |
| rt-006 | holdout | yes | ✗ | 2026-09-30 |
