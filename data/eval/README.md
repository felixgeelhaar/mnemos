# Mnemos Extraction Engine Evaluation Dataset

This directory contains test cases for evaluating the Mnemos claim extraction engine.

## Structure

```
data/eval/
├── README.md              # This file
├── schema.yaml            # Format specification
├── claim_types.yaml       # Claim type classification tests (10 cases)
├── deduplication.yaml     # Deduplication tests (8 cases)
├── contested_detection.yaml  # Contradiction detection tests (10 cases)
├── confidence_scoring.yaml   # Confidence score tests (8 cases)
├── edge_cases.yaml        # Edge case tests (12 cases)
├── boundaries.yaml        # Boundary condition tests (12 cases)
└── real_world.yaml       # Real document tests (8 cases)
```

## Total Test Cases: 68

| Category | Count | Description |
|----------|-------|-------------|
| Claim Types | 10 | Classification of facts, decisions, hypotheses |
| Deduplication | 8 | Near-identical claim detection |
| Contested Detection | 10 | Contradiction identification |
| Confidence Scoring | 8 | Score assignment accuracy |
| Edge Cases | 12 | Boundary conditions and unusual inputs |
| Boundaries | 12 | Minimum length, formatting edge cases |
| Real World | 8 | Full documents (meeting notes, PRDs, etc.) |

## Running the Evaluation

```bash
# Run all tests
go test ./data/eval/...

# Run specific category
go test ./data/eval/... -run TestClaimTypes

# Run with verbose output
go test ./data/eval/... -v

# Generate coverage report
go test ./data/eval/... -coverprofile=eval.out
```

## Test Format

Each test case specifies:
- `id`: Unique identifier
- `description`: What the test validates
- `tags`: Category tags for filtering
- `input`: Document text to extract from
- `expected_claims`: Claims that should be extracted
- `not_expected_claims`: Claims that should NOT be extracted
- `expected_count` / `expected_min_count`: Claim count constraints

## Success Criteria

For a passing evaluation:
1. All expected claims must be extracted with correct type
2. No unexpected claims should be extracted
3. Contested claims must be correctly identified
4. Confidence scores should be within acceptable bounds
5. Claim counts must meet expected thresholds

## Adding New Tests

1. Add test case to appropriate YAML file
2. Follow the schema in `schema.yaml`
3. Include both positive and negative cases
4. Document the reasoning in `description`
