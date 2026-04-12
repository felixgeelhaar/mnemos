# AGENTS

## Repo reality (as of current tree)
- This repository is currently planning-first: no application source code, no build system, and no test/lint/typecheck config are present.
- Do not guess stack-specific commands (`npm`, `pytest`, `go test`, etc.) unless relevant files are added in the same change.

## Verified sources of truth
- Product/architecture intent lives in `PRD.md` and `TDD.md`.
- Roadmapping context is in `Vision.md`, `Roadmap.md`, and `Product Brief.md`.
- Execution state is tracked in `.roady/spec.yaml`, `.roady/plan.json`, and `.roady/state.json`.

## Required planning workflow
- This repo has an active Roady workspace (`.roady/`) and an approved plan; check Roady state before implementing.
- Prefer Roady state over prose when deciding what is pending vs done.
- Current unlocked pending task in state: `task-core-foundation` (see `.roady/state.json`).

## Structure and boundaries
- `docs/` currently has no usable project docs/content (only `.DS_Store`).
- `.relicta/` exists for release memory/state; treat as tool metadata, not product source.
- `.gitignore` reserves `data/ingestion/` for local ingestion artifacts; keep `.gitkeep` if creating that tree.

## Known gotcha
- `.roady/state.json` contains historical evidence referencing files like `scripts/ingest_docs.py` and `docs/source/README.md` that are not in the current tree; verify file existence before relying on those notes.
