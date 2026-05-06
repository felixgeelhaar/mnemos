"""Real-trace recall suite for reliability-first benchmarking.

The suite evaluates fact recall on anonymized conversation-style traces.
Each case declares one or more required substrings that must appear in the
answer corpus (answer text or memory evidence text).
"""

from __future__ import annotations

import json
from dataclasses import dataclass, field
from pathlib import Path

from ..providers import Provider

DATA_FILE = Path(__file__).resolve().parent.parent / "data" / "real_trace_recall_seed.json"
VALID_SPLITS = {"train", "validation", "holdout", "all"}


@dataclass
class TestCase:
    id: str
    split: str
    critical: bool
    facts: list[str]
    question: str
    required_substrings: list[str]


@dataclass
class CaseResult:
    id: str
    split: str
    critical: bool
    passed: bool
    matched_substrings: list[str] = field(default_factory=list)
    missing_substrings: list[str] = field(default_factory=list)
    answer_excerpt: str = ""
    confidence: float = 0.0


@dataclass
class SuiteResult:
    provider: str
    split: str
    cases: list[CaseResult] = field(default_factory=list)

    def aggregate(self) -> dict:
        n = len(self.cases)
        if n == 0:
            return {
                "provider": self.provider,
                "split": self.split,
                "n": 0,
                "recall_pass_rate": 0.0,
                "omission_rate": 1.0,
                "critical_pass_rate": 0.0,
                "confidence_avg": 0.0,
            }

        passed = sum(1 for c in self.cases if c.passed)
        critical_cases = [c for c in self.cases if c.critical]
        critical_passed = sum(1 for c in critical_cases if c.passed)
        critical_rate = critical_passed / len(critical_cases) if critical_cases else 1.0
        confidence_sum = sum(c.confidence for c in self.cases)
        confidence_avg = confidence_sum / n

        return {
            "provider": self.provider,
            "split": self.split,
            "n": n,
            "recall_pass_rate": round(passed / n, 3),
            "omission_rate": round((n - passed) / n, 3),
            "critical_pass_rate": round(critical_rate, 3),
            "f1_avg": round(passed / n, 3),
            "precision_avg": round(passed / n, 3),
            "recall_avg": round(passed / n, 3),
            "confidence_avg": round(confidence_avg, 3),
        }


def load_cases(path: Path = DATA_FILE) -> list[TestCase]:
    data = json.loads(path.read_text())
    cases: list[TestCase] = []
    for entry in data:
        split = entry.get("split", "holdout")
        if split not in VALID_SPLITS - {"all"}:
            raise ValueError(f"unsupported split: {split}")
        cases.append(
            TestCase(
                id=entry["id"],
                split=split,
                critical=bool(entry.get("critical", False)),
                facts=entry["facts"],
                question=entry["question"],
                required_substrings=entry.get("required_substrings", []),
            )
        )
    return cases


def run(provider: Provider, split: str = "holdout", cases: list[TestCase] | None = None) -> SuiteResult:
    if split not in VALID_SPLITS:
        raise ValueError(f"invalid split '{split}', expected one of {sorted(VALID_SPLITS)}")

    all_cases = cases or load_cases()
    selected = all_cases if split == "all" else [c for c in all_cases if c.split == split]
    out = SuiteResult(provider=provider.name, split=split)

    for case in selected:
        provider.reset()
        for fact in case.facts:
            provider.add(fact)

        result = provider.query(case.question)
        haystack = (result.answer or "").strip()
        if not haystack:
            haystack = " ".join(m.get("content", "") for m in result.memories)
        haystack_lower = haystack.lower()

        matched = [s for s in case.required_substrings if s.lower() in haystack_lower]
        missing = [s for s in case.required_substrings if s.lower() not in haystack_lower]
        passed = len(missing) == 0 and len(case.required_substrings) > 0

        out.cases.append(
            CaseResult(
                id=case.id,
                split=case.split,
                critical=case.critical,
                passed=passed,
                matched_substrings=matched,
                missing_substrings=missing,
                answer_excerpt=haystack[:200],
                confidence=getattr(result, 'confidence', 0.0),
            )
        )

    return out
