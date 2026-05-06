# Claims & evidence

Every memory in Mnemos is a **claim** — a typed assertion with confidence, evidence, and a lifecycle status.

## Why typed?

Most memory layers store flat text. That's fine for "what did the user say" but not for "what did the agent decide" or "what did the operator believe at the time." Mnemos tracks the difference:

| Type | Meaning | Example |
|---|---|---|
| `fact` | Asserted true given current evidence | "The user has 12 prior refunds" |
| `decision` | An agent or operator chose this path | "Escalate to human review" |
| `hypothesis` | Plausible but not confirmed | "Customer may be repeat fraudster" |

## Evidence

Every claim links back to one or more events:

```
event ev_42 ────► claim cl_a ("succeeded")
event ev_43 ────► claim cl_b ("did not succeed")
                  cl_a ⊥ cl_b   (contradicts)
```

This back-link is what makes "trace any answer to its source" tractable. Hallucination bisection becomes "follow the link," not "re-prompt and pray."

## Lifecycle

| Status | When |
|---|---|
| `active` | Currently in force |
| `contested` | Pairs with another claim that disagrees on the same subject |
| `resolved` | One side of a contested pair was picked as winner |
| `deprecated` | Superseded by newer evidence; soft-removed but still queryable |

The lifecycle has its own audit trail (`mnemos history --kind=claim cl_a`).

## Trust score

Computed from `confidence × corroboration × freshness`:

- **Confidence**: from extraction (rule-based or LLM-assigned)
- **Corroboration**: how many independent events support the claim
- **Freshness**: per-claim half-life decay; `mnemos verify` ticks it back up

Trust feeds into search ranking and the [Context Block](../api.md#context-block).
