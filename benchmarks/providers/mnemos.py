"""Mnemos provider adapter."""

from __future__ import annotations

import os
import uuid
from datetime import datetime, timezone
from typing import Any

import httpx

from . import QueryResult


class MnemosProvider:
    """Talks to a local Mnemos via HTTP."""

    name = "mnemos"

    def __init__(self, base_url: str | None = None, token: str | None = None):
        self.base_url = (
            base_url or os.environ.get("MNEMOS_URL", "http://localhost:7777")
        ).rstrip("/")
        self.token = token or os.environ.get("MNEMOS_JWT")
        self.run_id = str(uuid.uuid4())  # one suite-run = one Mnemos run

    def _headers(self) -> dict[str, str]:
        h = {"Content-Type": "application/json"}
        if self.token:
            h["Authorization"] = f"Bearer {self.token}"
        return h

    def reset(self) -> None:
        # Switch to a fresh run id; old data stays in store but is
        # invisible to subsequent queries scoped to the new run.
        self.run_id = str(uuid.uuid4())

    def add(self, content: str, metadata: dict[str, Any] | None = None) -> str:
        event_id = str(uuid.uuid4())
        body = {
            "events": [{
                "id": event_id,
                "run_id": self.run_id,
                "source_input_id": f"bench::{self.run_id}",
                "content": content,
                "timestamp": datetime.now(timezone.utc).isoformat(),
                "metadata": _stringify(metadata or {}),
            }]
        }
        r = httpx.post(
            f"{self.base_url}/v1/events",
            headers=self._headers(),
            json=body,
            timeout=30,
        )
        r.raise_for_status()
        return event_id

    def query(self, question: str) -> QueryResult:
        # For the contradiction-detection suite we just need every
        # event for this run + Mnemos's contradiction view. The
        # "answer" is left blank because contradiction-detection
        # doesn't test grounded-answer quality.
        events_resp = httpx.get(
            f"{self.base_url}/v1/events",
            headers=self._headers(),
            params={"run_id": self.run_id, "limit": 200},
            timeout=30,
        )
        events_resp.raise_for_status()
        memories = events_resp.json().get("events", [])

        # Pull every active claim/relationship; the contradiction
        # eval looks at the relationships table directly.
        claims_resp = httpx.get(
            f"{self.base_url}/v1/claims",
            headers=self._headers(),
            params={"limit": 200},
            timeout=30,
        )
        claims = claims_resp.json().get("claims", []) if claims_resp.status_code == 200 else []

        rels_resp = httpx.get(
            f"{self.base_url}/v1/relationships",
            headers=self._headers(),
            params={"type": "contradicts", "limit": 200},
            timeout=30,
        )
        rels = rels_resp.json().get("relationships", []) if rels_resp.status_code == 200 else []

        # Project contradictions onto the memories we just listed.
        own_event_ids = {e["id"] for e in memories}
        contradictions = [
            r for r in rels
            if {r.get("from_claim_id"), r.get("to_claim_id")} <= own_event_ids
            or _claim_in_run(r, claims, own_event_ids)
        ]

        return QueryResult(
            answer="",
            memories=memories,
            contradictions=contradictions,
            evidence_ids=[e["id"] for e in memories],
        )


def _stringify(meta: dict[str, Any]) -> dict[str, str]:
    import json
    return {k: v if isinstance(v, str) else json.dumps(v, default=str) for k, v in meta.items()}


def _claim_in_run(rel: dict, claims: list[dict], own_event_ids: set[str]) -> bool:
    """A relationship counts as in-run when both claims point at events from this run."""
    by_id = {c.get("id"): c for c in claims}
    for cid in (rel.get("from_claim_id"), rel.get("to_claim_id")):
        c = by_id.get(cid)
        if not c:
            return False
        ev_ids = {e.get("event_id") for e in (c.get("evidence") or [])}
        if not (ev_ids & own_event_ids):
            return False
    return True
