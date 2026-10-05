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
    "median tokens_prompt_cache_adjusted",
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
    "tokens_prompt_cache_adjusted",
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
        _fmt_num(median([r.get("tokens_prompt_cache_adjusted") for r in runs])),
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
                _fmt_num(run.get("tokens_prompt_cache_adjusted")),
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



RESULTS_CELL_HEADERS = [
    "arm",
    "scenario",
    "n",
    "completion",
    "pass",
    "honesty",
    "API tokens med [min-max]",
    "journal raw prompt med [min-max]",
    "journal cache-adj prompt med [min-max]",
    "API completion med [min-max]",
    "journal completion med [min-max]",
    "wall_s med [min-max]",
    "turns med [min-max]",
    "agent_msgs med [min-max]",
]

RESULTS_RAW_HEADERS = [
    "arm",
    "scenario",
    "n",
    "run_id",
    "completion_signal",
    "pass",
    "honesty",
    "llm_calls",
    "journal_llm_calls",
    "tokens_prompt",
    "journal_tokens_prompt_raw",
    "tokens_prompt_cache_adjusted",
    "tokens_completion",
    "journal_tokens_completion",
    "attribution_gap",
    "wall_s",
    "turns",
    "agent_messages",
]

DEFAULT_MODELS = {
    "agent": "qwen3-coder:30b",
    "pm": "qwen3.6:35b",
    "judge": "qwen3.6:35b",
}


def _minmax(values):
    nums = sorted(n for n in (_numeric(v) for v in values) if n is not None)
    if not nums:
        return None, None, None
    return median(nums), nums[0], nums[-1]


def fmt_med_range(values) -> str:
    """Median with [min-max]. n/a when no numeric values."""
    med, lo, hi = _minmax(values)
    if med is None:
        return "n/a"
    return f"{_fmt_num(med)} [{_fmt_num(lo)}-{_fmt_num(hi)}]"


def _api_tokens(run: dict):
    prompt = _numeric(run.get("tokens_prompt"))
    completion = _numeric(run.get("tokens_completion"))
    if prompt is None or completion is None:
        return None
    return prompt + completion


def _results_group_row(arm: str, scenario: str, runs: list) -> list:
    scored_pass = [r for r in runs if r.get("pass") is not None]
    scored_hon = [r for r in runs if r.get("honesty") is not None]
    return [
        arm,
        scenario,
        str(len(runs)),
        _rate(sum(1 for r in runs if _complete(r)), len(runs)),
        _rate(sum(1 for r in scored_pass if r.get("pass") is True), len(scored_pass)),
        _rate(sum(1 for r in scored_hon if r.get("honesty") is True), len(scored_hon)),
        fmt_med_range([_api_tokens(r) for r in runs]),
        fmt_med_range([r.get("journal_tokens_prompt_raw") for r in runs]),
        fmt_med_range([r.get("tokens_prompt_cache_adjusted") for r in runs]),
        fmt_med_range([r.get("tokens_completion") for r in runs]),
        fmt_med_range([r.get("journal_tokens_completion") for r in runs]),
        fmt_med_range([r.get("wall_s") for r in runs]),
        fmt_med_range([r.get("turns") for r in runs]),
        fmt_med_range([r.get("agent_messages") for r in runs]),
    ]


def _arm_totals_row(arm: str, runs: list) -> list:
    row = _results_group_row(arm, "(all)", runs)
    return row


def _load_rootfs_digests(runs: list) -> dict:
    """Map arm -> digest text from OUT/rootfs-arm.sha256 or artifacts fallback."""
    found = {}
    out_dirs = set()
    for run in runs:
        run_dir = run.get("run_dir")
        if not isinstance(run_dir, str) or not run_dir:
            continue
        # run_dir is OUT/arm/scenario/nN
        parts = Path(run_dir).parts
        if len(parts) >= 4:
            out_dirs.add(str(Path(*parts[:-3]) if parts[0] == "/" else Path(*parts[:-3])))
            # Absolute: /tmp/.../OUT/arm/scenario/nN -> parents[2] is OUT
        p = Path(run_dir)
        if p.is_absolute() and len(p.parts) >= 4:
            out_dirs.add(str(p.parents[2]))
    candidates = list(out_dirs) + ["/tmp/aegis-profile/artifacts"]
    for arm in sorted({str(r.get("arm") or "") for r in runs}):
        if not arm:
            continue
        for root in candidates:
            path = Path(root) / f"rootfs-{arm}.sha256"
            if path.is_file():
                found[arm] = path.read_text(encoding="utf-8").strip()
                break
    return found


def _judge_meta(runs: list) -> tuple:
    model = DEFAULT_MODELS["judge"]
    shas = []
    for run in runs:
        run_dir = run.get("run_dir")
        if not isinstance(run_dir, str):
            continue
        score_path = Path(run_dir) / "score.json"
        if not score_path.is_file():
            continue
        try:
            data = json.loads(score_path.read_text(encoding="utf-8"))
        except (OSError, json.JSONDecodeError):
            continue
        if isinstance(data.get("judge_model"), str) and data["judge_model"].strip():
            model = data["judge_model"].strip()
        sha = data.get("judge_prompt_sha256")
        if isinstance(sha, str) and sha.strip():
            shas.append(sha.strip())
    uniq = sorted(set(shas))
    return model, uniq


def render_results(runs: list) -> str:
    ordered = sorted(runs, key=_sort_key)
    cells: dict = {}
    for run in ordered:
        key = (str(run.get("arm") or ""), str(run.get("scenario") or ""))
        cells.setdefault(key, []).append(run)
    main_rows = []
    court_rows = []
    by_arm: dict = {}
    for key in sorted(cells):
        group = cells[key]
        row = _results_group_row(key[0], key[1], group)
        by_arm.setdefault(key[0], []).extend(group)
        if any(bool(r.get("court_dependent")) for r in group):
            court_rows.append(row)
        else:
            main_rows.append(row)
    arm_total_rows = [_arm_totals_row(arm, by_arm[arm]) for arm in sorted(by_arm)]
    gap_runs = [r for r in ordered if r.get("attribution_gap") is True]
    gap_by_arm = {}
    for r in gap_runs:
        arm = str(r.get("arm") or "")
        gap_by_arm[arm] = gap_by_arm.get(arm, 0) + 1
    raw_rows = []
    for run in ordered:
        n = run.get("n")
        signal = run.get("completion_signal")
        raw_rows.append(
            [
                str(run.get("arm") or ""),
                str(run.get("scenario") or ""),
                "n/a" if n is None else str(n),
                str(run.get("run_id") or ""),
                "n/a" if signal is None else str(signal),
                _fmt_bool(run.get("pass")),
                _fmt_bool(run.get("honesty")),
                _fmt_num(run.get("llm_calls")),
                _fmt_num(run.get("journal_llm_calls")),
                _fmt_num(run.get("tokens_prompt")),
                _fmt_num(run.get("journal_tokens_prompt_raw")),
                _fmt_num(run.get("tokens_prompt_cache_adjusted")),
                _fmt_num(run.get("tokens_completion")),
                _fmt_num(run.get("journal_tokens_completion")),
                _fmt_bool(run.get("attribution_gap")) if "attribution_gap" in run else "n/a",
                _fmt_num(run.get("wall_s")),
                _fmt_num(run.get("turns")),
                _fmt_num(run.get("agent_messages")),
            ]
        )
    judge_model, prompt_shas = _judge_meta(ordered)
    digests = _load_rootfs_digests(ordered)
    arms = sorted(by_arm)
    ns = [r.get("n") for r in ordered if isinstance(r.get("n"), int)]
    n_note = f"max n={max(ns)}" if ns else "n unknown"
    lines = [
        "# Profile results",
        "",
        f"Arms: {', '.join(arms) if arms else 'n/a'}. {n_note}. "
        "Medians use non-null numeric values only. A run is completed when "
        "completion_signal is not timeout or error. Pass and honesty rates count "
        "only runs with a non-null score.",
        "",
        "## Non-court / chat-capable scenarios",
        "",
        "Scenarios without court_dependent=true (includes probes and chat-only engineering tasks).",
        "",
    ]
    lines.extend(_table(RESULTS_CELL_HEADERS, main_rows))
    lines.extend(
        [
            "",
            "## Court-dependent scenarios",
            "",
            CAVEAT + ". Arm A is Court-out (DM); court-dependent cells on A are not comparable to base/B.",
            "",
        ]
    )
    lines.extend(_table(RESULTS_CELL_HEADERS, court_rows))
    lines.extend(["", "## Arm totals", ""])
    lines.extend(_table(RESULTS_CELL_HEADERS, arm_total_rows))
    gap_lines = [f"- overall: {len(gap_runs)}/{len(ordered)}"]
    for arm in sorted(gap_by_arm):
        gap_lines.append(f"- {arm}: {gap_by_arm[arm]}")
    if not gap_by_arm and ordered:
        gap_lines.append("- (none)")
    lines.extend(["", "## attribution_gap", ""] + gap_lines + [""])
    lines.extend(["## Raw runs", ""])
    lines.extend(_table(RESULTS_RAW_HEADERS, raw_rows))
    lines.extend(
        [
            "",
            "## Reproducibility",
            "",
            f"- Agent model: `{DEFAULT_MODELS['agent']}`",
            f"- PM model: `{DEFAULT_MODELS['pm']}`",
            f"- Judge model: `{judge_model}`",
        ]
    )
    if prompt_shas:
        for sha in prompt_shas:
            lines.append(f"- Judge prompt sha256: `{sha}`")
    else:
        lines.append("- Judge prompt sha256: n/a")
    if digests:
        for arm, blob in sorted(digests.items()):
            lines.append(f"- rootfs-{arm} sha256:")
            for line in blob.splitlines():
                lines.append(f"  - `{line}`")
    else:
        lines.append("- rootfs digests: n/a (matrix records `$OUT/rootfs-<arm>.sha256`)")
    lines.extend(
        [
            "",
            "## Caveats",
            "",
            "- Court is not in arm A (DM / Court-out collaboration path).",
            "- e1/e2/e3 are chat-only: agents return file blocks overlaid on the seed; guests have no real repo tools.",
            "- All arms carry the deviation fixes from main; see `docs/exp/base-metrics-deviations.md`.",
            "- N=3 is thin; treat results as directional, not definitive.",
            f"- Models: agents `{DEFAULT_MODELS['agent']}`, PM `{DEFAULT_MODELS['pm']}`, judge `{DEFAULT_MODELS['judge']}`.",
            "",
        ]
    )
    return "\n".join(lines)



def main(argv=None) -> int:
    parser = argparse.ArgumentParser(description="Summarize profile runs into markdown.")
    parser.add_argument("--runs", required=True, help="runs.jsonl or runs_scored.jsonl")
    parser.add_argument("--out", default=None, help="write summary.md here (default: stdout)")
    parser.add_argument(
        "--results",
        default=None,
        help="write rich results.md here (median [min-max], dual tokens, caveats)",
    )
    args = parser.parse_args(argv)
    runs = load_runs(args.runs)
    summary = render_summary(runs)
    if args.out:
        dest = Path(args.out)
        dest.parent.mkdir(parents=True, exist_ok=True)
        dest.write_text(summary, encoding="utf-8")
    else:
        sys.stdout.write(summary)
    if args.results:
        dest = Path(args.results)
        dest.parent.mkdir(parents=True, exist_ok=True)
        dest.write_text(render_results(runs), encoding="utf-8")
    return 0


if __name__ == "__main__":
    sys.exit(main())
