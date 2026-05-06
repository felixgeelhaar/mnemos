"""Provider registry. Each adapter implements the Provider protocol."""

from __future__ import annotations

from typing import Protocol


class QueryResult:
    """What every provider returns for a query."""

    def __init__(
        self,
        answer: str,
        memories: list[dict],
        contradictions: list[dict] | None = None,
        evidence_ids: list[str] | None = None,
        confidence: float = 0.0,
    ):
        self.answer = answer
        self.memories = memories
        self.contradictions = contradictions or []
        self.evidence_ids = evidence_ids or []
        self.confidence = confidence


class Provider(Protocol):
    name: str

    def reset(self) -> None: ...
    def add(self, content: str, metadata: dict | None = None) -> str: ...
    def query(self, question: str) -> QueryResult: ...
