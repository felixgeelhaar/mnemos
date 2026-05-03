"""Quickstart chatbot — Mnemos memory in 30 lines, no SDK.

A minimal chatbot that remembers facts about the user across turns by
posting events to Mnemos and pulling them back at query time. The point
is to show that "memory for an AI app" is just two HTTP calls; the
fancier capabilities (claims, contradictions, replay) are a layer on
top, not a prerequisite.

Run:
    pip install -r requirements.txt
    python chatbot.py
"""

from __future__ import annotations

import os
import sys
import uuid
from datetime import datetime, timezone

import httpx

MNEMOS = os.environ.get("MNEMOS_URL", "http://localhost:7777")
TOKEN = os.environ.get("MNEMOS_JWT")  # required for writes if auth on
SESSION = str(uuid.uuid4())            # one run per chat session


def _headers() -> dict[str, str]:
    h = {"Content-Type": "application/json"}
    if TOKEN:
        h["Authorization"] = f"Bearer {TOKEN}"
    return h


def remember(text: str, role: str) -> None:
    """Append one event to Mnemos for this session."""
    httpx.post(
        f"{MNEMOS}/v1/events",
        headers=_headers(),
        json={"events": [{
            "id": str(uuid.uuid4()),
            "run_id": SESSION,
            "source_input_id": f"chatbot::{SESSION}",
            "content": text,
            "timestamp": datetime.now(timezone.utc).isoformat(),
            "metadata": {"role": role},
        }]},
    ).raise_for_status()


def recall() -> list[dict]:
    """Get every event for this session, oldest first."""
    r = httpx.get(
        f"{MNEMOS}/v1/events",
        headers=_headers(),
        params={"run_id": SESSION, "limit": 200},
    )
    r.raise_for_status()
    return list(reversed(r.json().get("events", [])))


def reply(user: str, history: list[dict]) -> str:
    """Stand-in for an LLM. Replace with your model of choice."""
    text = " ".join(e["content"].lower() for e in history)
    if "vegetarian" in text:
        return "I remember you're vegetarian. Want me to skip meat in suggestions?"
    if "allergy" in text or "allergic" in text:
        return "Got it — I'll keep your allergy in mind."
    return f"You said: {user}. Tell me more."


def main() -> None:
    print(f"Mnemos quickstart chatbot — session {SESSION}")
    print(f"Mnemos at {MNEMOS}. Ctrl-D to exit.\n")
    while True:
        try:
            user = input("> ").strip()
        except EOFError:
            print()
            break
        if not user:
            continue
        remember(user, role="user")
        history = recall()
        bot = reply(user, history)
        remember(bot, role="assistant")
        print(bot)
    print(f"\nReplay this session: GET {MNEMOS}/v1/events?run_id={SESSION}")


if __name__ == "__main__":
    sys.exit(main())
