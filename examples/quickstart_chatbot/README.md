# Quickstart chatbot — Mnemos memory in 30 lines

A minimal chatbot that remembers facts across turns by posting events
to Mnemos and pulling them back at query time. No SDK, no API key, no
LLM required to demo the memory loop — swap the `reply()` stub for
your model when you're ready.

## What it shows

- **Two HTTP calls** are the whole memory API for the simple case:
  `POST /v1/events` to remember, `GET /v1/events?run_id=…` to recall.
- **One `run_id` per chat session** ties the conversation together.
  Replay it months later from the same store.
- **No SDK to install.** The only dependency is `httpx` (or your
  language's standard HTTP client).

## Run it

```bash
# 1. Start Mnemos somewhere reachable
mnemos serve

# 2. Install the demo's only dependency
cd examples/quickstart_chatbot
pip install -r requirements.txt

# 3. Chat
python chatbot.py
```

Type messages mentioning "vegetarian" or "allergy" — the stub `reply()`
function will surface the memory back. Replace `reply()` with an
Anthropic, OpenAI, or local Llama call to see your own model use the
recalled events as context.

## What you get for those 30 lines

- Run-id-keyed history that survives restarts.
- Replay from a single `GET` weeks later.
- The same audit shape as the production refund-triage demo at
  [`examples/refund_triage_langgraph/`](../refund_triage_langgraph/) —
  add structured claims and contradiction detection on top whenever
  you need them.

## Auth

Set `MNEMOS_JWT` if your Mnemos has auth enabled. Without it, the
script assumes anonymous writes (Mnemos enforces auth on POST by
default; mint a token via `mnemos token issue --user <id>`).
