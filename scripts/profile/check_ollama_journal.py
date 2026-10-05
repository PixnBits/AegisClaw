#!/usr/bin/env python3
"""Cross-check one profile run against ``journalctl -u ollama``.

Ollama writes prompt-eval and generation timing to the host journal, not
to the Aegis daemon log. The network boundary copies ``prompt_eval_count``
into ``llm.usage.record`` as ``tokens_prompt`` and ``eval_count`` as
``tokens_completion``. The harness sums attributed records into
``tokens_prompt`` / ``tokens_prompt_raw``.

On Ollama 0.33.2 those two numbers are not the same thing:

- API ``prompt_eval_count`` is the **full prompt**. Sending the same
  1469-token prefix twice to ``qwen3-coder:30b`` returned
  ``prompt_eval_count=1469`` both times.
- Journal ``prompt eval time = ... / N tokens`` N is **newly evaluated**
  (cache-adjusted). The second call logged ``cached n_tokens = 1457`` and
  ``prompt eval time = 53.33 ms / 12 tokens``.
- ``qwen3.6:35b`` (hybrid/SWA) logs ``forcing full prompt re-processing``
  and the journal N equals the full prompt.

``tokens_cache_method`` is
``api_prompt_eval_count_full__journal_prompt_eval_new`` when the harness
read those journal lines, or
``api_prompt_eval_count_full__journal_unavailable`` when it could not.
Do not subtract a cache-similarity fraction from ``prompt_eval_count``.
Do not treat a journal prompt sum below API raw as a dropped record.

Until a run that actually called the PM, an agent, and Court has been
checked the same way, do not treat token deltas as experiment results.
Missing agent or Court rows on a PM-only run are a collaboration
scheduling gap when the journal also lacks those ``/api/generate`` calls.

Operator check for one finished run directory (``result.json``):

    python3 scripts/profile/check_ollama_journal.py RUN_DIR

That prints a journalctl command. The window starts a few seconds before
``started_at`` and ends at ``started_at`` plus ``total_s`` when that field
is set (conclusion ``wall_s`` plus drain), otherwise ``wall_s``, plus a
short settle (the harness waits about 3s after the drain before the usage
snapshot). Stop there. A later ``score.py --judge`` call is another
Ollama generate and is not part of this run.

Save that output and compare:

    journalctl -u ollama -o short-iso --since '...' --until '...' --no-pager \\
        > /tmp/ollama-window.log
    python3 scripts/profile/check_ollama_journal.py RUN_DIR --journal /tmp/ollama-window.log

Or let the script run journalctl (it needs permission to read the unit):

    python3 scripts/profile/check_ollama_journal.py RUN_DIR --run-journalctl

Inside the window, count only these lines:

- ``prompt eval time = ... / N tokens`` — N is newly evaluated tokens
  for that call (cache-adjusted). Sum of N is reported as cache-adjusted
  prompt tokens. Number of these lines is the journal call count.
- ``eval time = ... / M tokens`` on a line that is not ``prompt eval time``
  and not ``total time`` — M is ``eval_count``. Sum of M is compared to
  ``tokens_completion + tokens_completion_unattributed``.

Ignore cache similarity (``cached``, ``n_tokens``, or a fraction such as
1457/1469). Those lines are not extra calls. A journal prompt sum below
API ``tokens_prompt_raw + tokens_prompt_unattributed`` is a cache hit, not
a mismatch. Completion sum and prompt-eval line count are what have to
match the result (call count vs ``llm_calls``; extra journal calls are
allowed when unattributed tokens are present, e.g. Court). A wider
excerpt, or another process using Ollama in the same seconds, makes the
journal larger. That is not a dropped usage record.

Prewarm: ``daemon.sh`` POSTs a tiny ``ping`` generate after wait_ready. Those calls have prompt-eval N <= ``PREWARM_MAX_PROMPT_EVAL_TOKENS`` (11) and are excluded from journal totals and attribution_gap. A remaining call-count or completion mismatch (tolerance ``COMPLETION_GAP_TOLERANCE``) is a real gap.\n
The script does not change ``result.json``.

Exit 0 when the command is only printed, or the excerpt matches.
Exit 1 when the excerpt's completion sum or call count disagrees with
the result. A journal prompt sum below raw does not fail.
Exit 2 when the run dir, journalctl, or the excerpt is unusable (no
timing lines).
"""

from __future__ import annotations

import argparse
import json
import re
import subprocess
import sys
from datetime import datetime, timedelta, timezone
from pathlib import Path

# Seconds before started_at, and after started_at + total_s (or wall_s when
# the result has no total_s). The tail covers the harness usage settle (~3s)
# and clock skew. It is not long enough to include a later judge call on
# purpose; do not widen it into score time.
LEAD_S = 5.0
SETTLE_S = 15.0
# daemon.sh prewarm POSTs prompt "ping" (~7-11 prompt-eval tokens). Exclude those.
PREWARM_MAX_PROMPT_EVAL_TOKENS = 11
# attribution_gap when |journal_completion - API completion (incl. unattributed)| exceeds this.
COMPLETION_GAP_TOLERANCE = 5
TOKEN_CACHE_METHOD = "api_prompt_eval_count_full__journal_prompt_eval_new"
TOKEN_CACHE_METHOD_UNAVAILABLE = "api_prompt_eval_count_full__journal_unavailable"

_PROMPT_EVAL = re.compile(r"prompt eval time\s*=.*?/\s*(\d+)\s+tokens", re.IGNORECASE)
_EVAL = re.compile(r"eval time\s*=.*?/\s*(\d+)\s+tokens", re.IGNORECASE)
_NEW_PROMPT = re.compile(
    r"new prompt,.*?task\.n_tokens\s*=\s*(\d+)", re.IGNORECASE
)
_ISO = re.compile(
    r"(\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:?\d{2})?)"
)


def parse_time(value: str) -> datetime:
    text = str(value).strip().replace(" ", "T")
    if text.endswith("Z"):
        text = text[:-1] + "+00:00"
    match = re.search(r"([+-]\d{2})(\d{2})$", text)
    if match and text[match.start() :].count(":") == 0:
        text = text[: match.start()] + f"{match.group(1)}:{match.group(2)}"
    parsed = datetime.fromisoformat(text)
    if parsed.tzinfo is None:
        parsed = parsed.replace(tzinfo=timezone.utc)
    return parsed


def format_journal_time(moment: datetime) -> str:
    return moment.astimezone(timezone.utc).strftime("%Y-%m-%d %H:%M:%S UTC")


def _window_span_s(result: dict) -> float:
    """Journal span. total_s includes drain; older results only have wall_s."""
    raw = result.get("total_s")
    if raw is None:
        raw = result.get("wall_s")
    try:
        span = float(0 if raw is None else raw)
    except (TypeError, ValueError):
        span = 0.0
    if span < 0 or span != span or span == float("inf"):
        span = 0.0
    return span


def run_window(result: dict, lead_s: float = LEAD_S, settle_s: float = SETTLE_S):
    raw = result.get("started_at")
    if not raw:
        raise ValueError("result.json has no started_at")
    started = parse_time(str(raw))
    span = _window_span_s(result)
    start = started - timedelta(seconds=float(lead_s))
    end = started + timedelta(seconds=span + float(settle_s))
    return start, end


def journalctl_argv(result: dict, lead_s: float = LEAD_S, settle_s: float = SETTLE_S) -> list[str]:
    start, end = run_window(result, lead_s=lead_s, settle_s=settle_s)
    return [
        "journalctl",
        "-u",
        "ollama",
        "-o",
        "short-iso",
        "--since",
        format_journal_time(start),
        "--until",
        format_journal_time(end),
        "--no-pager",
    ]


def _line_in_window(line: str, start: datetime, end: datetime) -> bool:
    match = _ISO.search(line)
    if not match:
        return True
    try:
        stamped = parse_time(match.group(1))
    except ValueError:
        return True
    return start <= stamped <= end


def extract_timing(text: str, start: datetime | None = None, end: datetime | None = None) -> list[dict]:
    """Sum only prompt-eval and eval timing lines.

    Prompt-eval N is newly evaluated (cache-adjusted), not API
    ``prompt_eval_count``. Cache similarity and ``n_tokens`` notes are
    ignored. ``total time`` lines are ignored so they are not counted as
    completion tokens.
    """
    rows = []
    for line in text.splitlines():
        if start is not None and end is not None and not _line_in_window(line, start, end):
            continue
        if re.search(r"prompt eval time", line, re.IGNORECASE):
            match = _PROMPT_EVAL.search(line)
            if match:
                rows.append({"kind": "prompt", "tokens": int(match.group(1))})
            continue
        if re.search(r"total time", line, re.IGNORECASE):
            continue
        match = _EVAL.search(line)
        if match:
            rows.append({"kind": "completion", "tokens": int(match.group(1))})
    return rows



def extract_calls(text: str, start: datetime | None = None, end: datetime | None = None) -> list[dict]:
    """One row per generate call: prompt_raw, prompt_eval, completion, prewarm.

    prompt_eval is newly evaluated (cache-adjusted). prompt_raw is task.n_tokens
    from the preceding "new prompt" line when present, else prompt_eval.
    prewarm is True when prompt_eval <= PREWARM_MAX_PROMPT_EVAL_TOKENS (daemon
    keep_alive ping). Completion is paired to the preceding prompt-eval.
    """
    last_raw = None
    pending_prompt = None
    calls = []
    for line in text.splitlines():
        if start is not None and end is not None and not _line_in_window(line, start, end):
            continue
        m_new = _NEW_PROMPT.search(line)
        if m_new:
            last_raw = int(m_new.group(1))
        if re.search(r"prompt eval time", line, re.IGNORECASE):
            match = _PROMPT_EVAL.search(line)
            if match:
                prompt_eval = int(match.group(1))
                prompt_raw = last_raw if last_raw is not None else prompt_eval
                pending_prompt = {
                    "prompt_raw": prompt_raw,
                    "prompt_eval": prompt_eval,
                    "completion": 0,
                    "prewarm": prompt_eval <= PREWARM_MAX_PROMPT_EVAL_TOKENS,
                }
                calls.append(pending_prompt)
                last_raw = None
            continue
        if re.search(r"total time", line, re.IGNORECASE):
            continue
        match = _EVAL.search(line)
        if match and pending_prompt is not None:
            pending_prompt["completion"] = int(match.group(1))
            pending_prompt = None
    return calls


def without_prewarm(calls: list[dict]) -> list[dict]:
    return [c for c in calls if not c.get("prewarm")]


def journal_totals(calls: list[dict]) -> dict:
    """Sums over non-prewarm calls. Empty input yields zeros."""
    kept = without_prewarm(calls)
    return {
        "calls": len(kept),
        "prompt_raw": sum(int(c["prompt_raw"]) for c in kept),
        "prompt_eval": sum(int(c["prompt_eval"]) for c in kept),
        "completion": sum(int(c["completion"]) for c in kept),
        "prewarm_excluded": sum(1 for c in calls if c.get("prewarm")),
    }


def attribution_gap(result: dict, totals: dict | None) -> tuple[bool, str | None]:
    """Compare API usage to journal totals (prewarm already excluded).

    Gap when call counts differ (tolerance 0) or completion absolute delta
    exceeds COMPLETION_GAP_TOLERANCE. Prompt is not used for the gap flag.
    """
    if totals is None:
        return False, None
    expected = expected_totals(result)
    api_calls = expected["attributed_calls"]
    api_completion = expected["completion"]
    j_calls = int(totals["calls"])
    j_completion = int(totals["completion"])
    reasons = []
    if api_calls is not None and j_calls != api_calls:
        reasons.append(f"calls api={api_calls} journal={j_calls}")
    if abs(j_completion - api_completion) > COMPLETION_GAP_TOLERANCE:
        reasons.append(f"completion api={api_completion} journal={j_completion}")
    if not reasons:
        return False, None
    return True, "; ".join(reasons)


def expected_totals(result: dict) -> dict:
    raw = result.get("tokens_prompt_raw", result.get("tokens_prompt"))
    completion = result.get("tokens_completion")
    if raw is None or completion is None:
        raise ValueError("result.json is missing tokens_prompt_raw or tokens_completion")
    prompt_u = result.get("tokens_prompt_unattributed")
    completion_u = result.get("tokens_completion_unattributed")
    split = prompt_u is not None or completion_u is not None
    prompt_extra = int(prompt_u or 0)
    completion_extra = int(completion_u or 0)
    calls = result.get("llm_calls")
    try:
        attributed_calls = None if calls is None else int(calls)
    except (TypeError, ValueError):
        attributed_calls = None
    unattr = 0
    try:
        unattr = int(result.get("tokens_unattributed") or 0)
    except (TypeError, ValueError):
        unattr = 0
    return {
        "prompt": int(raw) + prompt_extra,
        "completion": int(completion) + completion_extra,
        "includes_unattributed": split,
        "attributed_calls": attributed_calls,
        "unattributed_tokens": unattr,
    }


def _calls_match(journal_calls: int, expected: dict) -> bool:
    attributed = expected["attributed_calls"]
    if attributed is None:
        return True
    if expected["unattributed_tokens"] > 0:
        return journal_calls >= attributed
    return journal_calls == attributed


def compare_totals(result: dict, rows: list[dict]) -> dict:
    expected = expected_totals(result)
    prompt = sum(row["tokens"] for row in rows if row["kind"] == "prompt")
    completion = sum(row["tokens"] for row in rows if row["kind"] == "completion")
    prompt_calls = sum(1 for row in rows if row["kind"] == "prompt")
    return {
        "journal_prompt_tokens": prompt,
        "journal_completion_tokens": completion,
        "journal_prompt_calls": prompt_calls,
        "journal_completion_calls": sum(1 for row in rows if row["kind"] == "completion"),
        "expected_prompt": expected["prompt"],
        "expected_completion": expected["completion"],
        "expected_calls": expected["attributed_calls"],
        "includes_unattributed": expected["includes_unattributed"],
        "prompt_match": prompt == expected["prompt"],
        "completion_match": completion == expected["completion"],
        "calls_match": _calls_match(prompt_calls, expected),
        "prompt_lt_raw": prompt < expected["prompt"],
    }


def _load_result(run_dir: Path) -> dict:
    path = run_dir / "result.json"
    if not path.is_file():
        raise ValueError(f"no result.json in {run_dir}")
    data = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(data, dict):
        raise ValueError("result.json is not an object")
    return data


def _report(result: dict, rows: list[dict]) -> tuple[int, str]:
    if not rows:
        return 2, "no prompt eval / eval timing lines in the journal excerpt"
    compared = compare_totals(result, rows)
    scope = (
        "tokens_prompt_raw + tokens_prompt_unattributed"
        if compared["includes_unattributed"]
        else "tokens_prompt_raw only (result has no unattributed split)"
    )
    if compared["prompt_match"]:
        prompt_note = "match"
    elif compared["prompt_lt_raw"]:
        prompt_note = "cache-adjusted below raw (not a failure)"
    else:
        prompt_note = "above raw (other traffic? not a failure by itself)"
    expected_calls = compared["expected_calls"]
    calls_note = "match" if compared["calls_match"] else "differ"
    lines = [
        f"prompt eval lines: {compared['journal_prompt_calls']} "
        f"cache-adjusted tokens: {compared['journal_prompt_tokens']}",
        f"eval lines: {compared['journal_completion_calls']} tokens: {compared['journal_completion_tokens']}",
        f"result prompt ({scope}): {compared['expected_prompt']}",
        f"result completion: {compared['expected_completion']}",
        f"result llm_calls (attributed): {expected_calls if expected_calls is not None else 'n/a'}",
        f"prompt (cache-adjusted vs API raw): {prompt_note}",
        f"completion: {'match' if compared['completion_match'] else 'differ'}",
        f"call count: {calls_note}",
    ]
    code = 0 if compared["completion_match"] and compared["calls_match"] else 1
    return code, "\n".join(lines)


def main(argv=None) -> int:
    parser = argparse.ArgumentParser(
        description="Cross-check one profile run against journalctl -u ollama",
    )
    parser.add_argument("run_dir", help="directory that contains result.json")
    parser.add_argument("--journal", help="saved journalctl text; do not call journalctl")
    parser.add_argument(
        "--run-journalctl",
        action="store_true",
        help="run journalctl -u ollama for the run window",
    )
    parser.add_argument("--lead-s", type=float, default=LEAD_S)
    parser.add_argument("--settle-s", type=float, default=SETTLE_S)
    args = parser.parse_args(argv)
    if args.journal and args.run_journalctl:
        print("pass only one of --journal and --run-journalctl", file=sys.stderr)
        return 2
    try:
        result = _load_result(Path(args.run_dir))
        argv_cmd = journalctl_argv(result, lead_s=args.lead_s, settle_s=args.settle_s)
        start, end = run_window(result, lead_s=args.lead_s, settle_s=args.settle_s)
    except (OSError, ValueError, json.JSONDecodeError) as exc:
        print(f"check_ollama_journal: {exc}", file=sys.stderr)
        return 2
    print(" ".join(argv_cmd))
    print(
        "window "
        f"{format_journal_time(start)} .. {format_journal_time(end)} "
        f"(lead {args.lead_s:g}s, settle {args.settle_s:g}s). "
        "Do not extend this into score.py --judge."
    )
    text = None
    if args.journal:
        try:
            text = Path(args.journal).read_text(encoding="utf-8", errors="replace")
        except OSError as exc:
            print(f"check_ollama_journal: {exc}", file=sys.stderr)
            return 2
    elif args.run_journalctl:
        try:
            proc = subprocess.run(
                argv_cmd,
                check=False,
                capture_output=True,
                text=True,
                timeout=60,
            )
        except (OSError, subprocess.TimeoutExpired) as exc:
            print(f"check_ollama_journal: {exc}", file=sys.stderr)
            return 2
        if proc.returncode != 0:
            detail = (proc.stderr or proc.stdout or "").strip()
            print(f"journalctl exited {proc.returncode}: {detail}", file=sys.stderr)
            return 2
        text = proc.stdout or ""
    if text is None:
        print(
            "no journal excerpt supplied. Re-run with --journal FILE or --run-journalctl "
            "to sum prompt eval / eval timing lines."
        )
        return 0
    try:
        calls = extract_calls(text, start, end)
        kept = without_prewarm(calls)
        # Build timing rows from non-prewarm calls so operator check matches harness.
        rows = []
        for call in kept:
            rows.append({"kind": "prompt", "tokens": int(call["prompt_eval"])})
            rows.append({"kind": "completion", "tokens": int(call["completion"])})
        code, report = _report(result, rows)
        gap, detail = attribution_gap(result, journal_totals(calls))
        if detail:
            report = report + f"\nattribution_gap: {detail}"
        elif calls and not gap:
            report = report + "\nattribution_gap: false (prewarm excluded)"
    except ValueError as exc:
        print(f"check_ollama_journal: {exc}", file=sys.stderr)
        return 2
    print(report)
    if code == 1:
        print(
            "differ: journal completion sum or prompt-eval call count disagrees "
            "with the result. A journal prompt sum below API raw is a cache hit, "
            "not a failure. "
            f"tokens_cache_method on new runs is {TOKEN_CACHE_METHOD} or "
            f"{TOKEN_CACHE_METHOD_UNAVAILABLE}.",
            file=sys.stderr,
        )
    return code


if __name__ == "__main__":
    try:
        sys.exit(main())
    except BrokenPipeError:
        sys.exit(0)
