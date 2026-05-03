"""Mnemos provider adapter.

The contradiction-detection wedge sits in the ingest → extract →
relate pipeline, which Mnemos exposes via CLI. The adapter drives
the CLI inside the running mnemos container so each test case
exercises the full pipeline (not just the storage surface).

Reset per case is done by switching the on-disk database file: each
case writes against a fresh sqlite path, so contradictions detected
in one case can't bleed into another.
"""

from __future__ import annotations

import json
import os
import shlex
import subprocess
import uuid
from typing import Any

from . import QueryResult


def _docker_exec(container: str, *args: str, env: dict | None = None) -> tuple[int, str, str]:
    """Run a command inside the named container; return (rc, stdout, stderr)."""
    cmd = ["docker", "exec"]
    for k, v in (env or {}).items():
        cmd += ["-e", f"{k}={v}"]
    cmd.append(container)
    cmd += list(args)
    proc = subprocess.run(cmd, capture_output=True, text=True, timeout=120)
    return proc.returncode, proc.stdout, proc.stderr


class MnemosProvider:
    name = "mnemos"

    def __init__(self, container: str | None = None):
        self.container = container or os.environ.get(
            "MNEMOS_CONTAINER", "benchmarks-mnemos-1"
        )
        self.db_path = ""
        self.reset()

    def reset(self) -> None:
        # Per-case sqlite db inside the container. Fresh file = fresh
        # claims + relationships tables; no cross-case bleed.
        self.db_path = f"/tmp/bench-{uuid.uuid4().hex[:12]}.db"

    def _env(self) -> dict[str, str]:
        return {"MNEMOS_DB_URL": f"sqlite://{self.db_path}"}

    def add(self, content: str, metadata: dict[str, Any] | None = None) -> str:
        # `mnemos process --text` runs the full pipeline: ingest event
        # → extract claims → relate (rule-based contradiction
        # detection happens here, no LLM needed for the suite).
        rc, stdout, stderr = _docker_exec(
            self.container, "mnemos", "process", "--text", content,
            env=self._env(),
        )
        if rc != 0:
            raise RuntimeError(f"mnemos process failed ({rc}): {stderr}")
        # mnemos process emits human-readable text, not JSON. Return
        # an opaque marker; the suite doesn't need event ids for
        # contradiction scoring.
        return f"event-{uuid.uuid4().hex[:8]}"

    def query(self, question: str) -> QueryResult:
        # Pull contested claim pairs via the relationships table.
        rc, stdout, _ = _docker_exec(
            self.container, "mnemos", "audit",
            env=self._env(),
        )
        if rc != 0:
            return QueryResult(answer="", memories=[], contradictions=[])
        try:
            audit = json.loads(stdout)
        except (json.JSONDecodeError, ValueError):
            return QueryResult(answer="", memories=[], contradictions=[])

        claims = audit.get("claims", [])
        rels = [
            r for r in audit.get("relationships", [])
            if r.get("type") == "contradicts"
        ]
        claim_text = {c.get("id"): c.get("text", "") for c in claims}
        contradictions = [
            {
                "between": [r["from_claim_id"], r["to_claim_id"]],
                "text_a": claim_text.get(r["from_claim_id"], ""),
                "text_b": claim_text.get(r["to_claim_id"], ""),
            }
            for r in rels
        ]
        memories = [{"id": c.get("id"), "content": c.get("text", "")} for c in claims]
        return QueryResult(
            answer="",
            memories=memories,
            contradictions=contradictions,
            evidence_ids=[c.get("id") for c in claims if c.get("id")],
        )
