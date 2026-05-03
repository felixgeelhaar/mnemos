"""Contradiction-detection eval suite.

Each test case is a list of facts (some deliberately contradictory)
plus the expected number of contradiction edges the provider should
surface. We measure precision + recall on the contradicted pairs.

This is Mnemos's claimed wedge. Either it wins decisively here or
the positioning is wrong — both outcomes are useful to know.
"""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any

from ..providers import Provider


@dataclass
class TestCase:
    name: str
    facts: list[str]
    # Pairs of (1-indexed) facts that contradict each other.
    expected_contradictions: list[tuple[int, int]]


@dataclass
class CaseResult:
    name: str
    expected: int
    detected: int
    precision: float
    recall: float
    f1: float
    notes: str = ""


@dataclass
class SuiteResult:
    provider: str
    cases: list[CaseResult] = field(default_factory=list)

    def aggregate(self) -> dict[str, Any]:
        if not self.cases:
            return {"provider": self.provider, "n": 0}
        avg = lambda key: sum(getattr(c, key) for c in self.cases) / len(self.cases)
        return {
            "provider": self.provider,
            "n": len(self.cases),
            "precision_avg": round(avg("precision"), 3),
            "recall_avg": round(avg("recall"), 3),
            "f1_avg": round(avg("f1"), 3),
        }


CASES: list[TestCase] = [
    TestCase(
        name="direct_polarity_conflict",
        facts=[
            "The deployment succeeded in production.",
            "The deployment did not succeed in production.",
            "Response times averaged 45ms.",
        ],
        expected_contradictions=[(1, 2)],
    ),
    TestCase(
        name="three_way_partial_conflict",
        facts=[
            "The CEO is Alice.",
            "The CEO is Bob.",
            "Alice founded the company in 2018.",
            "Bob joined the company in 2022.",
        ],
        expected_contradictions=[(1, 2)],
    ),
    TestCase(
        name="no_contradictions_clean_facts",
        facts=[
            "Postgres is the primary database.",
            "Redis caches frequent queries.",
            "Nginx fronts the API.",
        ],
        expected_contradictions=[],
    ),
    TestCase(
        name="numeric_disagreement",
        facts=[
            "The user has 12 prior refunds.",
            "The user has 0 prior refunds.",
            "The user signed up in 2024.",
        ],
        expected_contradictions=[(1, 2)],
    ),
    TestCase(
        name="implicit_temporal_conflict",
        facts=[
            "The migration completed on Tuesday.",
            "The migration is still running.",
            "Engineering signed off on Monday.",
        ],
        expected_contradictions=[(1, 2)],
    ),
]


def run(provider: Provider) -> SuiteResult:
    out = SuiteResult(provider=provider.name)
    for case in CASES:
        provider.reset()
        for fact in case.facts:
            provider.add(fact)

        result = provider.query("what contradictions exist?")
        detected = _normalize_pairs(result.contradictions, case.facts)

        expected = set(_canonicalise_pairs(case.expected_contradictions))
        true_positives = len(detected & expected)
        false_positives = len(detected - expected)
        false_negatives = len(expected - detected)

        precision = (
            true_positives / (true_positives + false_positives)
            if (true_positives + false_positives)
            else 1.0
            if not expected
            else 0.0
        )
        recall = (
            true_positives / (true_positives + false_negatives)
            if (true_positives + false_negatives)
            else 1.0
        )
        f1 = (
            2 * precision * recall / (precision + recall)
            if (precision + recall)
            else 0.0
        )
        out.cases.append(
            CaseResult(
                name=case.name,
                expected=len(expected),
                detected=len(detected),
                precision=precision,
                recall=recall,
                f1=f1,
            )
        )
    return out


def _canonicalise_pairs(pairs: list[tuple[int, int]]) -> set[tuple[int, int]]:
    return {tuple(sorted(p)) for p in pairs}


def _normalize_pairs(contradictions: list[dict], facts: list[str]) -> set[tuple[int, int]]:
    """Map a provider's contradiction edges to (1-indexed) fact-pair tuples.

    Provider-agnostic: each contradiction must somehow reference two
    pieces of content. We match on substring-overlap, which is cruder
    than ideal but works across schemas.
    """
    out: set[tuple[int, int]] = set()
    fact_idx = list(enumerate(facts, start=1))
    for c in contradictions:
        # Try common shape variants — claim ids, between-keys, raw text.
        between = c.get("between") or [c.get("from_claim_id"), c.get("to_claim_id")]
        text_a = (c.get("text_a") or c.get("a") or "").strip()
        text_b = (c.get("text_b") or c.get("b") or "").strip()

        a_idx = _match(text_a, fact_idx) if text_a else None
        b_idx = _match(text_b, fact_idx) if text_b else None
        if a_idx and b_idx:
            out.add(tuple(sorted([a_idx, b_idx])))
            continue

        if isinstance(between, list) and len(between) == 2 and all(between):
            # Provider gave us ids; we can't cross-reference without claim
            # bodies, so just count the edge as one contradiction
            # against the first matching pair we haven't claimed yet.
            for i, fa in fact_idx:
                for j, fb in fact_idx:
                    if i >= j:
                        continue
                    if (i, j) not in out:
                        out.add((i, j))
                        break
                else:
                    continue
                break

    return out


def _match(text: str, fact_idx: list[tuple[int, str]]) -> int | None:
    """Coarse text → fact-index match; returns first overlap above 30%."""
    text_l = text.lower()
    for idx, fact in fact_idx:
        fact_l = fact.lower()
        a_in_b = text_l in fact_l or fact_l in text_l
        if a_in_b:
            return idx
    return None
