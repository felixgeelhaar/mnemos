"""Generate a markdown summary table from benchmark results JSON."""

from __future__ import annotations

import argparse
import glob
import json
from collections import defaultdict
from pathlib import Path


def load_results(directory: str) -> dict[tuple[str, str], dict]:
    """Latest result wins for each (provider, suite) pair."""
    out: dict[tuple[str, str], dict] = {}
    for path in sorted(glob.glob(f"{directory}/*.json")):
        with open(path) as f:
            data = json.load(f)
        key = (data["provider"], data["suite"])
        prev = out.get(key)
        if prev is None or data["timestamp"] > prev["timestamp"]:
            out[key] = data
    return out


def write_summary(results: dict[tuple[str, str], dict], outfile: Path) -> None:
    by_suite: dict[str, list[dict]] = defaultdict(list)
    for (_, suite), data in results.items():
        by_suite[suite].append(data)

    lines: list[str] = ["# Benchmark results", ""]
    lines.append("Latest run per (provider, suite) pair. Re-run any with")
    lines.append("`python -m benchmarks.run --provider <name> --suite <name>`.")
    lines.append("")

    for suite, runs in sorted(by_suite.items()):
        lines.append(f"## {suite}")
        lines.append("")
        lines.append("| Provider | n | Precision | Recall | F1 | Run |")
        lines.append("|---|---|---|---|---|---|")
        for r in sorted(runs, key=lambda x: x["summary"].get("f1_avg", 0), reverse=True):
            s = r["summary"]
            lines.append(
                f"| **{r['provider']}** | {s['n']} | "
                f"{s.get('precision_avg', 0):.2f} | "
                f"{s.get('recall_avg', 0):.2f} | "
                f"{s.get('f1_avg', 0):.2f} | "
                f"`{r['timestamp']}` |"
            )
        lines.append("")

        # Per-case detail for the leader.
        leader = max(runs, key=lambda x: x["summary"].get("f1_avg", 0))
        lines.append(f"### Per-case detail — {leader['provider']}")
        lines.append("")

        cases = leader.get("cases", [])
        # Detect schema: contradiction_detection cases have 'expected'/'detected'/'f1';
        # QA suites (locomo, longmemeval) have 'answered'/'matched_substrings'.
        if cases and "expected" in cases[0]:
            lines.append("| Case | Expected | Detected | P | R | F1 |")
            lines.append("|---|---|---|---|---|---|")
            for c in cases:
                lines.append(
                    f"| {c['name']} | {c['expected']} | {c['detected']} | "
                    f"{c['precision']:.2f} | {c['recall']:.2f} | {c['f1']:.2f} |"
                )
        elif cases and "passed" in cases[0]:
            lines.append("| Case | Split | Critical | Passed | Missing substrings |")
            lines.append("|---|---|---|---|---|")
            for c in cases:
                missing = ", ".join(c.get("missing_substrings", []))
                critical = "yes" if c.get("critical") else "no"
                passed = "✓" if c.get("passed") else "✗"
                lines.append(f"| {c['id']} | {c.get('split', '')} | {critical} | {passed} | {missing} |")
        else:
            lines.append("| Case | Answered | Matched substrings | Answer excerpt |")
            lines.append("|---|---|---|---|")
            for c in cases:
                matched = ", ".join(c.get("matched_substrings", []))
                excerpt = c.get("answer_excerpt", "")[:80].replace("|", "\\|")
                answered = "✓" if c.get("answered") else "✗"
                lines.append(f"| {c['name']} | {answered} | {matched} | {excerpt} |")
        lines.append("")

    outfile.write_text("\n".join(lines))


def main() -> int:
    p = argparse.ArgumentParser()
    p.add_argument("--results-dir", default="benchmarks/results")
    p.add_argument("--out", default="benchmarks/RESULTS.md")
    args = p.parse_args()
    results = load_results(args.results_dir)
    write_summary(results, Path(args.out))
    print(f"wrote {args.out}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
