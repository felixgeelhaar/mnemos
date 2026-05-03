"""mem0 (OSS server) provider adapter.

Talks to mem0's self-hosted server API:
- POST /memories — store
- POST /search — query
- DELETE /memories — wipe (used for reset)

Auth: AUTH_DISABLED=true mode is assumed for benchmark runs.

mem0's contradiction-detection story: it doesn't surface contradictions
as edges. It does have a "memory update" mechanism that overwrites
older memories when a new one supersedes them, but that's a
write-time decision, not a query-time signal. For the contradiction
suite, mem0 will retrieve both contradicting facts side-by-side; the
suite scores that as zero contradictions detected.

That's the wedge: Mnemos's relationships table makes contradictions
queryable; mem0 collapses them silently.
"""

from __future__ import annotations

import os
import uuid
from typing import Any

import httpx

from . import QueryResult


class Mem0Provider:
    name = "mem0"

    def __init__(self, base_url: str | None = None):
        self.base_url = (
            base_url or os.environ.get("MEM0_URL", "http://localhost:8888")
        ).rstrip("/")
        self.user_id = f"bench-{uuid.uuid4().hex[:8]}"

    def reset(self) -> None:
        # Wipe all memories so the next case starts clean.
        # mem0 supports DELETE /memories with a user filter.
        try:
            httpx.delete(
                f"{self.base_url}/memories",
                params={"user_id": self.user_id},
                timeout=30,
            )
        except httpx.HTTPError:
            pass
        self.user_id = f"bench-{uuid.uuid4().hex[:8]}"

    def add(self, content: str, metadata: dict[str, Any] | None = None) -> str:
        body = {
            "messages": [{"role": "user", "content": content}],
            "user_id": self.user_id,
            "metadata": metadata or {},
            # infer=False stops mem0 from running its LLM-based fact
            # extractor — for the contradiction suite we want raw
            # storage, not derived facts.
            "infer": False,
        }
        r = httpx.post(f"{self.base_url}/memories", json=body, timeout=60)
        r.raise_for_status()
        data = r.json()
        # mem0 returns a list of created memory ids; first one wins.
        if isinstance(data, list) and data:
            return data[0].get("id", "")
        if isinstance(data, dict):
            return data.get("id", "")
        return ""

    def query(self, question: str) -> QueryResult:
        body = {
            "query": question,
            "user_id": self.user_id,
            "top_k": 50,
        }
        r = httpx.post(f"{self.base_url}/search", json=body, timeout=60)
        r.raise_for_status()
        results = r.json()
        if isinstance(results, dict):
            results = results.get("results", [])
        memories = [
            {
                "id": m.get("id", ""),
                "content": m.get("memory") or m.get("content", ""),
                "metadata": m.get("metadata", {}),
            }
            for m in results
        ]
        # mem0 surfaces no contradiction edges. Suite will score this
        # as zero contradictions detected — by design, because mem0
        # architecturally lacks the concept.
        return QueryResult(
            answer="",
            memories=memories,
            contradictions=[],
            evidence_ids=[m["id"] for m in memories],
        )
