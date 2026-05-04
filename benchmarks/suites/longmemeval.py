"""LongMemEval-style suite over chat-history → question recall.

This is the harness scaffold. The official LongMemEval dataset (Wu
et al., 2024) ships ~500 cases with a specific chat-format JSON; the
loader below reads cases from a small YAML/JSON fixture so the
suite can run today without an internet round-trip. To run against
the published dataset, download it from HuggingFace and write a
shim that adapts each case to TestCase below.

Each test case is a chronological sequence of facts (the chat
history) plus a question and the substring(s) the answer must
contain. We score per-case binary (substring present / not), then
aggregate to recall@1.

This suite is provider-agnostic: any registered Provider with add()
and query() works. Mnemos's adapter feeds facts via the CLI
pipeline; mem0/zep/letta adapters feed via their REST APIs (they
ship in their own tasks).
"""

from __future__ import annotations

import json
from dataclasses import dataclass, field
from pathlib import Path

from ..providers import Provider


@dataclass
class TestCase:
    name: str
    facts: list[str]
    question: str
    expected_substrings: list[str]


@dataclass
class CaseResult:
    name: str
    answered: bool
    matched_substrings: list[str] = field(default_factory=list)
    answer_excerpt: str = ""


@dataclass
class SuiteResult:
    provider: str
    cases: list[CaseResult] = field(default_factory=list)

    def aggregate(self) -> dict:
        if not self.cases:
            return {"provider": self.provider, "n": 0, "recall_at_1": 0.0}
        hits = sum(1 for c in self.cases if c.answered)
        return {
            "provider": self.provider,
            "n": len(self.cases),
            "recall_at_1": round(hits / len(self.cases), 3),
            "f1_avg": round(hits / len(self.cases), 3),  # binary case → recall == F1
            "precision_avg": round(hits / len(self.cases), 3),
            "recall_avg": round(hits / len(self.cases), 3),
        }


SEED_CASES: list[TestCase] = [
    TestCase(
        name="single_session_simple_recall",
        facts=[
            "User said their favorite color is blue.",
            "User said they live in Berlin.",
            "User asked the agent about restaurants.",
        ],
        question="What is the user's favorite color?",
        expected_substrings=["blue"],
    ),
    TestCase(
        name="multi_fact_dietary_constraint",
        facts=[
            "User mentioned they are vegetarian.",
            "User said they are allergic to peanuts.",
            "User asked about restaurants in their neighborhood.",
        ],
        question="What dietary restrictions does the user have?",
        expected_substrings=["vegetarian", "peanut"],
    ),
    TestCase(
        name="contradicting_then_resolved_preference",
        facts=[
            "User said they prefer tea.",
            "User later said they actually prefer coffee.",
        ],
        question="What does the user prefer to drink?",
        expected_substrings=["coffee"],
    ),
]


def load_cases(path: Path | None = None) -> list[TestCase]:
    """Load cases from a JSON fixture, falling back to SEED_CASES.

    JSON shape:
      [{"name": "...", "facts": [...], "question": "...",
        "expected_substrings": [...]}]
    """
    if path is None:
        return SEED_CASES
    try:
        data = json.loads(Path(path).read_text())
    except FileNotFoundError:
        return SEED_CASES
    out: list[TestCase] = []
    for entry in data:
        out.append(
            TestCase(
                name=entry["name"],
                facts=entry["facts"],
                question=entry["question"],
                expected_substrings=entry.get("expected_substrings", []),
            )
        )
    return out


def run(provider: Provider, cases: list[TestCase] | None = None) -> SuiteResult:
    cases = cases or SEED_CASES
    out = SuiteResult(provider=provider.name)
    for case in cases:
        provider.reset()
        for fact in case.facts:
            provider.add(fact)
        result = provider.query(case.question)
        haystack = (result.answer or "")
        if not haystack:
            haystack = " ".join(m.get("content", "") for m in result.memories)
        haystack_lower = haystack.lower()
        matched = [s for s in case.expected_substrings if s.lower() in haystack_lower]
        answered = len(matched) > 0
        out.cases.append(
            CaseResult(
                name=case.name,
                answered=answered,
                matched_substrings=matched,
                answer_excerpt=haystack[:200],
            )
        )
    return out
