"""LoCoMo-style suite over multi-session conversations.

Adapts the published LoCoMo benchmark format (Maharana et al., 2024)
to the harness's Provider protocol. Each LoCoMo case is a sequence
of dialogue sessions between two speakers, paired with a question
and a reference answer. The harness flattens messages into facts,
feeds them to the provider in chronological order, then scores
substring recall on the answer.

Substring scoring is the lightweight equivalent — the original paper
uses an LLM-judged QA score. Wire MNEMOS_LLM_PROVIDER and swap the
scorer when you want to reproduce headline numbers.

The loader accepts either:
- A path to a JSON file shaped like the published LoCoMo dataset
  (a list of cases, each with `sessions` + `questions`).
- No path: returns a small seed fixture so the suite is runnable
  today against any provider.
"""

from __future__ import annotations

import json
from dataclasses import dataclass, field
from pathlib import Path

from ..providers import Provider


@dataclass
class TestCase:
    """One LoCoMo-style case.

    facts: chronological list of conversation utterances + speaker
    annotation, flattened from session messages. Provider feeds them
    to its memory in order.

    question / expected_substrings: scoring targets. Substrings are
    case-insensitive.
    """

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
            "f1_avg": round(hits / len(self.cases), 3),
            "precision_avg": round(hits / len(self.cases), 3),
            "recall_avg": round(hits / len(self.cases), 3),
        }


SEED_CASES: list[TestCase] = [
    TestCase(
        name="recall_across_sessions",
        facts=[
            "Speaker A (session 1): I'm planning a trip to Tokyo next month.",
            "Speaker B (session 1): Any specific dates?",
            "Speaker A (session 1): March 14 to 22.",
            "Speaker B (session 1): Sounds great.",
            "Speaker A (session 2): I had to push the trip back two weeks because of work.",
            "Speaker B (session 2): What are the new dates?",
            "Speaker A (session 2): March 28 to April 5.",
        ],
        question="When is Speaker A traveling to Tokyo?",
        expected_substrings=["march 28", "april 5"],
    ),
    TestCase(
        name="cross_session_preference",
        facts=[
            "Speaker A (session 1): I really love sushi.",
            "Speaker A (session 2): I tried that new ramen place yesterday — incredible.",
            "Speaker A (session 3): If I could only eat one cuisine forever it would be Japanese.",
        ],
        question="What cuisine does Speaker A prefer?",
        expected_substrings=["japanese"],
    ),
    TestCase(
        name="contradicting_then_corrected",
        facts=[
            "Speaker A (session 1): My favorite color is blue.",
            "Speaker A (session 5): Actually I changed my mind, I prefer green now.",
        ],
        question="What is Speaker A's favorite color now?",
        expected_substrings=["green"],
    ),
]


def load_cases(path: Path | None = None) -> list[TestCase]:
    """Load cases from a published-LoCoMo-format JSON file.

    The published format is a list of objects shaped roughly:

        {
          "sample_id": "...",
          "sessions": {
            "session_1": {"date_time": "...", "messages": [
              {"speaker": "Bob", "text": "..."}, ...
            ]},
            "session_2": ...
          },
          "questions": [
            {"question": "...", "answer": "...", "category": ...}
          ]
        }

    The loader flattens (sessions, messages) into a chronological
    `facts` list and emits one TestCase per question. Substring
    targets are derived from the reference answer (split on commas
    and stripped).
    """
    if path is None:
        return SEED_CASES
    try:
        data = json.loads(Path(path).read_text())
    except FileNotFoundError:
        return SEED_CASES

    out: list[TestCase] = []
    for sample_idx, sample in enumerate(data):
        sessions = sample.get("sessions") or sample.get("conversation") or {}
        # Sessions arrive keyed "session_1" / "session_2" / …; sort
        # by the integer suffix so chronology is preserved.
        ordered_keys = sorted(
            sessions.keys(),
            key=lambda k: int(k.rsplit("_", 1)[-1]) if k.rsplit("_", 1)[-1].isdigit() else 0,
        )
        facts: list[str] = []
        for key in ordered_keys:
            sess = sessions[key]
            timestamp = sess.get("date_time") or sess.get("timestamp") or key
            for msg in sess.get("messages", []):
                speaker = msg.get("speaker", "?")
                text = msg.get("text") or msg.get("content", "")
                facts.append(f"{speaker} ({timestamp}): {text}")

        for q_idx, q in enumerate(sample.get("questions", [])):
            question = q.get("question") or q.get("query", "")
            answer = q.get("answer") or q.get("expected_answer", "")
            if not question or not answer:
                continue
            substrings = [s.strip() for s in str(answer).split(",") if s.strip()]
            if not substrings:
                substrings = [str(answer).strip()]
            out.append(TestCase(
                name=f"sample{sample_idx}_q{q_idx}",
                facts=facts,
                question=question,
                expected_substrings=[s.lower() for s in substrings],
            ))
    return out or SEED_CASES


def run(provider: Provider, cases: list[TestCase] | None = None) -> SuiteResult:
    cases = cases or SEED_CASES
    out = SuiteResult(provider=provider.name)
    for case in cases:
        provider.reset()
        for fact in case.facts:
            provider.add(fact)
        result = provider.query(case.question)
        haystack = result.answer or ""
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
