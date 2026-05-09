"""Compare the latest benchmark results against the committed baseline.

Exits non-zero when any suite's F1 / precision / recall drops more than
the threshold below the baseline, when suite-level policy metrics fall
below explicit minimums (for example recall_pass_rate_min), or when a
baselined suite has no results at all (regression by omission).

LLM-mode awareness: confidence_avg is meaningful only when the LLM-
augmented extraction path produced the run. CI exercises the rule-
based path (no LLM provider configured); set ``MNEMOS_BENCH_LLM=0``
and the harness will skip the ``confidence_avg_min`` floor for that
invocation. F1 / precision / recall / pass-rate floors still apply.

The harness also skips suites that the run didn't produce results for
(``--allow-missing``) so a CI job that only runs ``real_trace_recall``
isn't penalised for not running ``contradiction_detection`` in the
same workflow.

Usage:
    python -m benchmarks.check_baseline                    # default paths
    python -m benchmarks.check_baseline --threshold 0.05   # override
    MNEMOS_BENCH_LLM=0 python -m benchmarks.check_baseline # skip LLM floors
    python -m benchmarks.check_baseline --allow-missing real_trace_recall
"""

from __future__ import annotations

import argparse
import glob
import json
import os
import sys
from pathlib import Path


def latest_results(directory: Path) -> dict[str, dict]:
    """Return the latest result dict for each (provider=mnemos, suite)."""
    out: dict[str, dict] = {}
    for path in sorted(glob.glob(str(directory / "*.json"))):
        data = json.loads(Path(path).read_text())
        if data.get("provider") != "mnemos":
            continue
        suite = data["suite"]
        prev = out.get(suite)
        if prev is None or data["timestamp"] > prev["timestamp"]:
            out[suite] = data
    return out


def _truthy(value: str) -> bool:
    return value.strip().lower() in {"1", "true", "yes", "on"}


def main() -> int:
    p = argparse.ArgumentParser()
    p.add_argument("--baseline", default="benchmarks/baseline.json")
    p.add_argument("--results-dir", default="benchmarks/results")
    p.add_argument("--threshold", type=float, default=None,
                   help="Override the baseline file's _threshold")
    p.add_argument("--allow-missing", action="append", default=[],
                   help="Suite name to allow missing results for (repeatable). "
                        "Use when the workflow only runs a subset of baselined suites.")
    args = p.parse_args()

    baseline = json.loads(Path(args.baseline).read_text())
    threshold = args.threshold if args.threshold is not None else baseline.get("_threshold", 0.05)
    suites = baseline.get("suites", {})

    # Honour MNEMOS_BENCH_LLM=0 by skipping the confidence_avg_min floor:
    # rule-based extraction does not populate per-claim confidence scores
    # the way LLM extraction does, so the floor is structurally
    # incompatible with no-LLM mode.
    llm_mode = _truthy(os.environ.get("MNEMOS_BENCH_LLM", "1"))
    allow_missing = set(args.allow_missing or [])

    results = latest_results(Path(args.results_dir))

    failures: list[str] = []
    skipped: list[str] = []
    for suite_name, expected in suites.items():
        got = results.get(suite_name)
        if got is None:
            if suite_name in allow_missing:
                skipped.append(f"[{suite_name}] no results — allowed missing")
                continue
            failures.append(f"[{suite_name}] no results found")
            continue
        summary = got["summary"]
        for metric in ("f1", "precision", "recall"):
            min_key = f"{metric}_min"
            if min_key not in expected:
                continue
            avg_key = f"{metric}_avg"
            actual = summary.get(avg_key, 0.0)
            min_value = expected[min_key]
            allowed_drop = max(0.0, min_value - threshold)
            if actual < allowed_drop:
                failures.append(
                    f"[{suite_name}] {metric} = {actual:.3f}, "
                    f"baseline >= {min_value:.3f} (allowed >= {allowed_drop:.3f}, "
                    f"threshold {threshold})"
                )

        # Optional suite-level minimums that should not use threshold drift
        # semantics (these are hard floors). confidence_avg_min is skipped
        # in no-LLM mode because rule-based extraction doesn't populate
        # the per-claim confidence the floor was designed against.
        for min_key, summary_key in (
            ("recall_pass_rate_min", "recall_pass_rate"),
            ("critical_pass_rate_min", "critical_pass_rate"),
            ("confidence_avg_min", "confidence_avg"),
        ):
            if min_key not in expected:
                continue
            if min_key == "confidence_avg_min" and not llm_mode:
                skipped.append(f"[{suite_name}] {summary_key} skipped (MNEMOS_BENCH_LLM=0)")
                continue
            actual = summary.get(summary_key, 0.0)
            min_value = expected[min_key]
            if actual < min_value:
                failures.append(
                    f"[{suite_name}] {summary_key} = {actual:.3f}, "
                    f"required >= {min_value:.3f}"
                )

    if skipped:
        print("Skipped (informational):", file=sys.stderr)
        for s in skipped:
            print("  - " + s, file=sys.stderr)

    if failures:
        print("Benchmark regression detected:", file=sys.stderr)
        for f in failures:
            print("  - " + f, file=sys.stderr)
        print("\nIf this drop is intentional, update benchmarks/baseline.json "
              "in the same commit.", file=sys.stderr)
        return 1

    print(f"OK — all baselined suites satisfy F1/P/R drift and policy floors.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
