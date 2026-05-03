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

    def __init__(self, container: str | None = None, llm: bool | None = None):
        self.container = container or os.environ.get(
            "MNEMOS_CONTAINER", "benchmarks-mnemos-1"
        )
        # Default to --llm when MNEMOS_LLM_PROVIDER is set inside the
        # container (compose.yml sets this when OPENAI_API_KEY is
        # present). Override with MNEMOS_BENCH_LLM=0 to force rule-only.
        if llm is None:
            llm_flag = os.environ.get("MNEMOS_BENCH_LLM", "1")
            self.llm = llm_flag != "0"
        else:
            self.llm = llm
        self.db_path = ""
        self.reset()

    def reset(self) -> None:
        # Per-case sqlite db inside the container. Fresh file = fresh
        # claims + relationships tables; no cross-case bleed.
        self.db_path = f"/tmp/bench-{uuid.uuid4().hex[:12]}.db"
        self._pending: list[str] = []

    def _env(self) -> dict[str, str]:
        return {"MNEMOS_DB_URL": f"sqlite://{self.db_path}"}

    def add(self, content: str, metadata: dict[str, Any] | None = None) -> str:
        # Defer ingest to query time. Mnemos's relate stage detects
        # contradictions when a batch of claims sits in one extract
        # pass; per-fact invocations don't always cross-relate. Batch
        # makes the benchmark fair.
        self._pending.append(content)
        return f"event-{uuid.uuid4().hex[:8]}"

    def _flush(self) -> None:
        if not self._pending:
            return
        # Newline-joined text so all facts are one event; mnemos
        # process extracts claims from the whole block then relates
        # them in one pass.
        joined = "\n".join(self._pending)
        cmd = ["mnemos", "process", "--text", joined]
        if self.llm:
            cmd.append("--llm")
        rc, _, stderr = _docker_exec(self.container, *cmd, env=self._env())
        if rc != 0:
            raise RuntimeError(f"mnemos process failed ({rc}): {stderr}")
        self._pending.clear()

    def query(self, question: str) -> QueryResult:
        self._flush()
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
