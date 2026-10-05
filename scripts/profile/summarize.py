#!/usr/bin/env python3
"""Markdown summary of profile runs (one arm or many)."""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path

INCOMPLETE = {"timeout", "error"}
CAVEAT = (
    "court_dependent scenarios depend on Court/egress policy behaviour; compare with care"
)

CELL_HEADERS = [
    "arm",
    "scenario",
    "runs",
    "completion",
    "pass",
    "honesty",
    "median tokens_prompt",
    "median tokens_completion",
    "median tokens total",
    "median wall_s",
    "median turns",
    "median llm_calls",
]

RAW_HEADERS = [
    "arm",
    "scenario",
    "n",
    "run_id",
    "completion_signal",
    "pass",
    "honesty",
    "tokens_prompt",
    "tokens_completion",
    "tokens_total",
    "wall_s",
    "turns",
    "llm_calls",
]


def _numeric(value):
    if isinstance(value, bool) or not isinstance(value, (int, float)):
        return None
    return float(value)


def median(values):
    """Median of non-null numbers. Bools are ignored. None when nothing remains."""
    nums = sorted(n for n in (_numeric(v) for v in values) if n is not None)
    if not nums:
        return None
    mid = len(nums) // 2
    if len(nums) % 2 == 1:
        return nums[mid]
    return (nums[mid - 1] + nums[mid]) / 2


def load_runs(path) -> list[dict]:
    rows = []
    text = Path(path).read_text(encoding="utf-8")
    for line in text.splitlines():
        line = line.strip()
        if not line:
            continue
        rows.append(json.loads(line))
    return rows


def _fmt_num(value) -> str:
    number = _numeric(value)
    if number is None:
        return "n/a"
    if number.is_integer():
        return str(int(number))
    return f"{number:.4f}".rstrip("0").rstrip(".")


def _fmt_bool(value) -> str:
    if value is True:
        return "true"
    if value is False:
        return "false"
    return "n/a"


def _rate(numer: int, denom: int) -> str:
    if denom == 0:
        return "n/a"
    return f"{numer}/{denom}"


def _complete(run: dict) -> bool:
    return run.get("completion_signal") not in INCOMPLETE


def _total_tokens(run: dict):
    prompt = _numeric(run.get("tokens_prompt"))
    completion = _numeric(run.get("tokens_completion"))
    if prompt is None or completion is None:
        return None
    return prompt + completion


def _sort_key(run: dict):
    n = run.get("n")
    n_key = n if isinstance(n, int) and not isinstance(n, bool) else 10**9
    return (
        str(run.get("arm") or ""),
        str(run.get("scenario") or ""),
        n_key,
        str(run.get("run_id") or ""),
    )


def _cell(value) -> str:
    return str(value).replace("|", "\\|").replace("\n", " ")


def _table(headers: list[str], rows: list[list[str]]) -> list[str]:
    lines = [
        "| " + " | ".join(headers) + " |",
        "| " + " | ".join("---" for _ in headers) + " |",
    ]
    body = rows or [["n/a" for _ in headers]]
    for row in body:
        lines.append("| " + " | ".join(_cell(col) for col in row) + " |")
    return lines


def _group_row(arm: str, scenario: str, runs: list[dict]) -> list[str]:
    scored_pass = [r for r in runs if r.get("pass") is not None]
    scored_hon = [r for r in runs if r.get("honesty") is not None]
    return [
        arm,
        scenario,
        str(len(runs)),
        _rate(sum(1 for r in runs if _complete(r)), len(runs)),
        _rate(sum(1 for r in scored_pass if r.get("pass") is True), len(scored_pass)),
        _rate(sum(1 for r in scored_hon if r.get("honesty") is True), len(scored_hon)),
        _fmt_num(median([r.get("tokens_prompt") for r in runs])),
        _fmt_num(median([r.get("tokens_completion") for r in runs])),
        _fmt_num(median([_total_tokens(r) for r in runs])),
        _fmt_num(median([r.get("wall_s") for r in runs])),
        _fmt_num(median([r.get("turns") for r in runs])),
        _fmt_num(median([r.get("llm_calls") for r in runs])),
    ]


def render_summary(runs: list[dict]) -> str:
    ordered = sorted(runs, key=_sort_key)
    cells: dict[tuple[str, str], list[dict]] = {}
    for run in ordered:
        key = (str(run.get("arm") or ""), str(run.get("scenario") or ""))
        cells.setdefault(key, []).append(run)
    main_rows = []
    court_rows = []
    for key in sorted(cells):
        group = cells[key]
        row = _group_row(key[0], key[1], group)
        if any(bool(r.get("court_dependent")) for r in group):
            court_rows.append(row)
        else:
            main_rows.append(row)
    raw_rows = []
    for run in ordered:
        signal = run.get("completion_signal")
        n = run.get("n")
        raw_rows.append(
            [
                str(run.get("arm") or ""),
                str(run.get("scenario") or ""),
                "n/a" if n is None else str(n),
                str(run.get("run_id") or ""),
                "n/a" if signal is None else str(signal),
                _fmt_bool(run.get("pass")),
                _fmt_bool(run.get("honesty")),
                _fmt_num(run.get("tokens_prompt")),
                _fmt_num(run.get("tokens_completion")),
                _fmt_num(_total_tokens(run)),
                _fmt_num(run.get("wall_s")),
                _fmt_num(run.get("turns")),
                _fmt_num(run.get("llm_calls")),
            ]
        )
    lines = [
        "# Profile summary",
        "",
        "Medians use non-null numeric values only. A run is completed when "
        "completion_signal is not timeout or error. Pass and honesty "
        "rates count only runs with a non-null score; if every value is null the cell shows n/a.",
        "",
        "## Results",
        "",
    ]
    lines.extend(_table(CELL_HEADERS, main_rows))
    lines.extend(
        [
            "",
            "## Court-dependent scenarios",
            "",
            CAVEAT,
            "",
        ]
    )
    lines.extend(_table(CELL_HEADERS, court_rows))
    lines.extend(["", "## Raw runs", ""])
    lines.extend(_table(RAW_HEADERS, raw_rows))
    lines.append("")
    return "\n".join(lines)


def main(argv=None) -> int:
    parser = argparse.ArgumentParser(description="Summarize profile runs into markdown.")
    parser.add_argument("--runs", required=True, help="runs.jsonl or runs_scored.jsonl")
    parser.add_argument("--out", default=None, help="write markdown here (default: stdout)")
    args = parser.parse_args(argv)
    text = render_summary(load_runs(args.runs))
    if args.out:
        dest = Path(args.out)
        dest.parent.mkdir(parents=True, exist_ok=True)
        dest.write_text(text, encoding="utf-8")
    else:
        sys.stdout.write(text)
    return 0


if __name__ == "__main__":
    sys.exit(main())
