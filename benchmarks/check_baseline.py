"""Compare the latest benchmark results against the committed baseline.

Exits non-zero when any suite's F1 / precision / recall drops more than
the threshold below the baseline, or when a baselined suite has no
results at all (regression by omission).

Usage:
    python -m benchmarks.check_baseline                    # default paths
    python -m benchmarks.check_baseline --threshold 0.05   # override
"""

from __future__ import annotations

import argparse
import glob
import json
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


def main() -> int:
    p = argparse.ArgumentParser()
    p.add_argument("--baseline", default="benchmarks/baseline.json")
    p.add_argument("--results-dir", default="benchmarks/results")
    p.add_argument("--threshold", type=float, default=None,
                   help="Override the baseline file's _threshold")
    args = p.parse_args()

    baseline = json.loads(Path(args.baseline).read_text())
    threshold = args.threshold if args.threshold is not None else baseline.get("_threshold", 0.05)
    suites = baseline.get("suites", {})

    results = latest_results(Path(args.results_dir))

    failures: list[str] = []
    for suite_name, expected in suites.items():
        got = results.get(suite_name)
        if got is None:
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

    if failures:
        print("Benchmark regression detected:", file=sys.stderr)
        for f in failures:
            print("  - " + f, file=sys.stderr)
        print("\nIf this drop is intentional, update benchmarks/baseline.json "
              "in the same commit.", file=sys.stderr)
        return 1

    print(f"OK — all baselined suites within {threshold} of recorded F1/P/R.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
