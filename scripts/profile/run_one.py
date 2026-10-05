#!/usr/bin/env python3
"""Run one profiling scenario on one arm.

Conclusion checks run only after min_wait_s since the goal was accepted
(Sent/Posted goal acceptance in the pm-goal output), in this order: final_marker, quiet,
quiet_no_reply, then the timeout_s hard cap measured from t0.

final_marker requires a non-PM agent reply after the first PM message and a
marker on a message at or after that reply. The PM plan alone does not conclude.

A turn is one PM or agent message in the collected transcript, whether that
message was a channel post or a DM chat.message.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import re
import sys
import time
from pathlib import Path

import check_ollama_journal as ollama_journal
from common import (
    aegis,
    arm_messaging,
    as_int,
    canonical_arm,
    collect_dm_messages,
    extract_json,
    load_scenario,
    now_iso,
    parse_transcript,
    run_bounded,
    usage_delta,
    usage_snapshot,
)

PROFILE_DIR = Path(__file__).resolve().parent

RESULT_KEYS = [
    "arm",
    "scenario",
    "kind",
    "court_dependent",
    "n",
    "run_id",
    "channel",
    "completion_signal",
    "timed_out",
    "pass",
    "honesty",
    "tokens_prompt",
    "tokens_prompt_raw",
    "tokens_prompt_cache_adjusted",
    "tokens_cache_method",
    "tokens_completion",
    "llm_calls",
    "journal_llm_calls",
    "wall_s",
    "turns",
    "turns_source",
    "messages",
    "agent_messages",
    "pm_messages",
    "retries",
    "stalls",
    "tokens_unattributed",
    "tokens_prompt_unattributed",
    "tokens_completion_unattributed",
    "started_at",
    "run_dir",
    "error",
]

# API prompt_eval_count is the full prompt (Ollama 0.33.2). Journal
# "prompt eval time ... / N tokens" N is newly evaluated (cache-adjusted).
TOKEN_CACHE_METHOD = "api_prompt_eval_count_full__journal_prompt_eval_new"
TOKEN_CACHE_METHOD_UNAVAILABLE = "api_prompt_eval_count_full__journal_unavailable"
CACHE_METHODS = {TOKEN_CACHE_METHOD, TOKEN_CACHE_METHOD_UNAVAILABLE}
SIGNALS = {"final_marker", "quiet", "quiet_no_reply", "timeout", "error", "dry_run"}
RETRY_RE = re.compile(r"retry|RETRY|retrying")
STALL_RE = re.compile(r"stall|STALL|timed out|deadline exceeded")
TURN_MARK = "channel.turn.recv"
PM_GOAL_TIMEOUT_S = 240
CLI_TIMEOUT_S = 20
SEED_TIMEOUT_S = 120
SHORT_POLL_S = 60
USAGE_SETTLE_S = 3
LOG_SLICE_MAX = 64 * 1024 * 1024


class HarnessError(Exception):
    """The harness itself failed. The scenario outcome is not usable data."""


def _as_bool(value, default=False) -> bool:
    if isinstance(value, bool):
        return value
    if value is None:
        return default
    if isinstance(value, (int, float)) and not isinstance(value, bool):
        return value != 0
    if isinstance(value, str):
        text = value.strip().lower()
        if text in {"true", "yes", "1"}:
            return True
        if text in {"false", "no", "0", ""}:
            return False
    return default


def _clip(text, limit=4000):
    if text is None:
        return None
    value = str(text)
    if len(value) <= limit:
        return value
    return value[: limit - 3] + "..."


def _check_segment(label: str, value: str) -> str:
    if not isinstance(value, str) or not re.fullmatch(r"[A-Za-z0-9._-]+", value):
        raise HarnessError(f"{label} must be a single path segment, got {value!r}")
    return value


def _scenario_id(scenario: dict, path) -> str:
    raw = scenario.get("id")
    if isinstance(raw, str) and raw.strip():
        return raw.strip()
    return Path(path).stem


def make_channel(scenario_id: str, arm: str, n: int, deterministic: bool) -> str:
    sid = re.sub(r"[^a-z0-9-]+", "-", scenario_id.lower()).strip("-") or "scenario"
    aid = re.sub(r"[^a-z0-9-]+", "-", arm.lower()).strip("-") or "arm"
    if deterministic:
        hx = hashlib.sha256(f"{arm}\n{scenario_id}\n{n}".encode()).hexdigest()[:6]
    else:
        import secrets

        hx = secrets.token_hex(3)
    channel = f"prof-{sid}-{aid}-n{int(n)}-{hx}"
    channel = re.sub(r"-{2,}", "-", channel).strip("-")
    return channel


def files_block(root: Path) -> str:
    if not root.is_dir():
        return ""
    parts = []
    for dirpath, dirnames, filenames in os.walk(root, followlinks=False):
        dirnames.sort()
        filenames.sort()
        for name in filenames:
            path = Path(dirpath) / name
            if not path.is_file():
                continue
            rel = path.relative_to(root).as_posix()
            text = path.read_bytes().decode("utf-8", errors="replace")
            parts.append(f"=== {rel} ===\n{text}")
    return "\n".join(parts)


def apply_goal(goal: str, block: str) -> str:
    if "{{FILES}}" not in goal:
        return goal
    return goal.replace("{{FILES}}", block)


def _write_text(path: Path, text: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(text, encoding="utf-8")


def _write_json(path: Path, obj) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(obj, indent=2, ensure_ascii=False, default=str) + "\n", encoding="utf-8")


def append_jsonl(path: Path, obj: dict) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    line = json.dumps(obj, ensure_ascii=False, default=str) + "\n"
    with path.open("a", encoding="utf-8") as fh:
        fh.write(line)
        fh.flush()
        os.fsync(fh.fileno())


def write_result_last(path: Path, obj: dict) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    tmp = path.with_suffix(".json.tmp")
    tmp.write_text(json.dumps(obj, indent=2, ensure_ascii=False, default=str) + "\n", encoding="utf-8")
    os.replace(tmp, path)


def transcript_text(messages) -> str:
    lines = []
    for message in messages:
        content = (message.get("content") or "").replace("\r\n", "\n").replace("\n", " ")
        lines.append(f"[{message['seq']}] {message['from']}: {content}")
    if not lines:
        return ""
    return "\n".join(lines) + "\n"


def messaging_mode(arm: str, scenario) -> str:
    """Scenario messaging overrides the arm. Default is the arm spec, else channel."""
    if isinstance(scenario, dict):
        raw = scenario.get("messaging")
        if isinstance(raw, str) and raw.strip():
            return raw.strip().lower()
    try:
        return arm_messaging(arm)
    except KeyError:
        return "channel"


def _message_identity(message: dict):
    return (
        (message.get("role") or "").strip(),
        (message.get("from") or "").strip(),
        (message.get("content") or "").strip(),
    )


def _renumber(messages) -> list:
    out = []
    for index, message in enumerate(messages, start=1):
        item = dict(message)
        item["seq"] = index
        out.append(item)
    return out


def _dm_insert_at(channel_messages, extras, patterns) -> int:
    """Where undated DM rows go so a channel synthesis stays after them.

    A lone channel PM is treated as the closing synthesis when the DM side
    already has a PM (the plan) or the lone PM matches a marker and an agent
    reply was collected. Otherwise DMs follow the first PM (the channel plan).
    """
    pm_indexes = [index for index, message in enumerate(channel_messages) if message.get("role") == "pm"]
    if not pm_indexes:
        return len(channel_messages)
    first_pm = pm_indexes[0]
    last_pm = pm_indexes[-1]
    if last_pm != first_pm:
        return last_pm
    extras_have_pm = any(message.get("role") == "pm" for message in extras)
    extras_have_agent = any(message.get("role") == "agent" for message in extras)
    lone = channel_messages[first_pm]
    lone_matches = False
    text = lone.get("content") or ""
    for pattern in _compile_markers(patterns):
        if pattern.search(text):
            lone_matches = True
            break
    if extras_have_pm or (lone_matches and extras_have_agent):
        return first_pm
    return first_pm + 1


def merge_transcripts(channel_messages, extra_messages, patterns=None) -> list:
    """Channel posts plus DM messages. Same role, sender, and content count once.

    When every kept message has a timestamp, order is that timestamp.
    Otherwise undated DM messages are inserted with `_dm_insert_at`.
    """
    channel_messages = [dict(message) for message in (channel_messages or [])]
    seen = set()
    for message in channel_messages:
        ident = _message_identity(message)
        if ident != ("", "", ""):
            seen.add(ident)
    extras = []
    for message in extra_messages or []:
        if not isinstance(message, dict):
            continue
        item = dict(message)
        ident = _message_identity(item)
        if ident != ("", "", "") and ident in seen:
            continue
        if ident != ("", "", ""):
            seen.add(ident)
        extras.append(item)
    if not extras:
        return _renumber(channel_messages)
    combined = channel_messages + extras
    if combined and all(isinstance(message.get("ts"), str) and message.get("ts").strip() for message in combined):
        decorated = list(enumerate(combined))
        decorated.sort(key=lambda pair: (pair[1]["ts"], pair[0]))
        return _renumber([message for _index, message in decorated])
    insert_at = _dm_insert_at(channel_messages, extras, patterns or [])
    return _renumber(channel_messages[:insert_at] + extras + channel_messages[insert_at:])


def with_transcript_turns(metrics: dict, messages, collected: bool) -> dict:
    """Count turns from PM and agent transcript messages when a transcript exists."""
    out = dict(metrics)
    if collected:
        out["turns"] = sum(1 for message in messages or [] if message.get("role") in {"pm", "agent"})
        out["turns_source"] = "transcript"
    return out


def message_counts(messages):
    agent = sum(1 for message in messages if message.get("role") == "agent")
    pm = sum(1 for message in messages if message.get("role") == "pm")
    non_user = sum(
        1 for message in messages if message.get("role") not in {"user", "system"}
    )
    senders = len({message.get("from") or "" for message in messages if message.get("from")})
    return len(messages), agent, pm, non_user, senders


def _compile_markers(patterns):
    compiled = []
    for pattern in patterns or []:
        if not isinstance(pattern, str) or pattern == "":
            continue
        try:
            compiled.append(re.compile(pattern, re.IGNORECASE))
        except re.error:
            continue
    return compiled


def _first_agent_reply_index(messages):
    """Index of the first non-PM agent message after the first PM message.

    None when there is no PM message, or no agent reply after that plan.
    Court senders and facilitator system status posts are not agents.
    The PM's own first message is not a reply.
    """
    first_pm = None
    for index, message in enumerate(messages):
        if message.get("role") == "pm":
            first_pm = index
            break
    if first_pm is None:
        return None
    for index, message in enumerate(messages):
        if index <= first_pm:
            continue
        if message.get("role") == "agent":
            return index
    return None


def marker_hit(messages, patterns) -> bool:
    """True only after an agent has replied to the PM plan and a marker matches.

    final_marker may fire only when there is at least one non-PM agent reply
    after the first PM message, and a final_markers pattern matches some PM or
    agent message at or after that first agent reply (the reply itself or a
    later PM synthesis). The PM's first plan or clarifying ask alone does not
    match, even when its text contains a marker.
    """
    compiled = _compile_markers(patterns)
    if not compiled:
        return False
    start = _first_agent_reply_index(messages)
    if start is None:
        return False
    for message in messages[start:]:
        if message.get("role") not in ("pm", "agent"):
            continue
        text = message.get("content") or ""
        for pattern in compiled:
            if pattern.search(text):
                return True
    return False


def _is_true(value) -> bool:
    if isinstance(value, bool):
        return value
    if isinstance(value, (int, float)):
        return value != 0
    if isinstance(value, str):
        return value.strip().lower() in {"1", "true", "yes"}
    return False


def inspect_turn_state(obj):
    """Return (available, any_pending, summary). None means the command failed."""
    if obj is None:
        return False, False, "unavailable"
    pending = 0
    members = 0

    def walk(node, in_members: bool) -> None:
        nonlocal pending, members
        if isinstance(node, dict):
            is_member = in_members or "pending" in node or "role" in node
            if is_member and ("pending" in node or "role" in node or "last_seen_seq" in node):
                members += 1
                if _is_true(node.get("pending")):
                    pending += 1
                return
            for key, value in node.items():
                walk(value, in_members or key in ("members", "Members"))
            return
        if isinstance(node, list):
            for value in node:
                walk(value, in_members)

    walk(obj, False)
    return True, pending > 0, f"members={members} pending={pending}"


def silence_seconds(last_change_mono, now_mono) -> float:
    """Host monotonic seconds since the message set last changed.

    Message timestamps are ignored. No observation yet (None) is 0.0.
    """
    if last_change_mono is None:
        return 0.0
    return now_mono - last_change_mono


def judge_signal(
    messages,
    elapsed_since_accept,
    silence_s,
    turn_available,
    any_pending,
    min_wait_s,
    quiet_s,
    patterns,
):
    """Return final_marker, quiet, quiet_no_reply, or None. Timeout is the caller's hard cap."""
    if elapsed_since_accept is None or elapsed_since_accept < min_wait_s:
        return None
    if marker_hit(messages, patterns):
        return "final_marker"
    _total, _agent, _pm, non_user, _senders = message_counts(messages)
    pending_blocks = turn_available and any_pending
    if non_user >= 1 and silence_s >= quiet_s:
        if not pending_blocks:
            return "quiet"
        # Stale-pending escape: product may leave pending=true after a post
        # (last_outcome=delivered). Do not wait until timeout_s.
        if silence_s >= max(quiet_s * 2, 150):
            return "quiet"
    # quiet_no_reply requires a successful turn-state that shows nothing pending.
    # quiet treats a missing turn-state as best-effort and does not block on it.
    nothing_pending = turn_available and not any_pending
    quiet_no_reply_s = max(quiet_s * 2, 150)
    if non_user == 0 and elapsed_since_accept >= quiet_no_reply_s and nothing_pending:
        return "quiet_no_reply"
    return None


def _count_lines(text: str, pattern: re.Pattern) -> int:
    return sum(1 for line in text.splitlines() if pattern.search(line))


def _unique_lines(text: str):
    seen = set()
    lines = []
    for line in text.splitlines():
        if line in seen:
            continue
        seen.add(line)
        lines.append(line)
    return lines


def _opt_int(value):
    if value is None:
        return None
    return int(value)


def _usage_delta_doc(attributed, unattributed, metrics) -> dict:
    """Shape of usage_delta.json. Cache-adjusted prompt tokens may be null."""
    return {
        "attributed": attributed,
        "unattributed": unattributed,
        "tokens_prompt": metrics["tokens_prompt"],
        "tokens_prompt_raw": metrics["tokens_prompt_raw"],
        "tokens_prompt_cache_adjusted": metrics["tokens_prompt_cache_adjusted"],
        "tokens_cache_method": metrics["tokens_cache_method"],
        "tokens_completion": metrics["tokens_completion"],
        "llm_calls": metrics["llm_calls"],
        "journal_llm_calls": metrics.get("journal_llm_calls"),
        "tokens_unattributed": metrics["tokens_unattributed"],
        "tokens_prompt_unattributed": metrics["tokens_prompt_unattributed"],
        "tokens_completion_unattributed": metrics["tokens_completion_unattributed"],
    }


def account(trace_text: str, window_text: str, attributed, unattributed) -> dict:
    trace_lines = [line for line in _unique_lines(trace_text) if "[collab-trace]" in line or TURN_MARK in line]
    # turns come from collab-trace lines; window_text is the deduped log window.
    turn_lines = [line for line in trace_lines if TURN_MARK in line]
    if not turn_lines:
        turn_lines = [line for line in _unique_lines(trace_text) if TURN_MARK in line]
    if turn_lines:
        turns = len(turn_lines)
        turns_source = "trace"
    else:
        turns = len(attributed)
        turns_source = "llm_calls"
    window_lines = "\n".join(_unique_lines(window_text))
    prompt = sum(as_int(record.get("tokens_prompt")) for record in attributed)
    completion = sum(as_int(record.get("tokens_completion")) for record in attributed)
    un_prompt = sum(as_int(record.get("tokens_prompt")) for record in unattributed)
    un_completion = sum(as_int(record.get("tokens_completion")) for record in unattributed)
    return {
        "tokens_prompt": prompt,
        "tokens_prompt_raw": prompt,
        "tokens_prompt_cache_adjusted": None,
        "tokens_cache_method": TOKEN_CACHE_METHOD_UNAVAILABLE,
        "tokens_completion": completion,
        "llm_calls": len(attributed),
        "journal_llm_calls": None,
        "turns": int(turns),
        "turns_source": turns_source,
        "retries": _count_lines(window_lines, RETRY_RE),
        "stalls": _count_lines(window_lines, STALL_RE),
        "tokens_unattributed": un_prompt + un_completion,
        "tokens_prompt_unattributed": un_prompt,
        "tokens_completion_unattributed": un_completion,
    }


def apply_journal_cache(metrics: dict, started_at, wall_s, *, run_cmd=None) -> dict:
    """Fill cache-adjusted prompt tokens from journalctl. Never raises.

    Live runs only. Journal prompt-eval N is newly evaluated tokens, not the
    API full-prompt count. Missing journalctl or no timing lines leave
    tokens_prompt_cache_adjusted null.
    """
    out = dict(metrics)
    out["tokens_prompt_cache_adjusted"] = None
    out["journal_llm_calls"] = None
    out["tokens_cache_method"] = TOKEN_CACHE_METHOD_UNAVAILABLE
    try:
        stub = {"started_at": started_at, "wall_s": wall_s}
        argv = ollama_journal.journalctl_argv(stub)
        if run_cmd is None:
            proc = run_bounded(argv, cwd=None, timeout=60)
        else:
            proc = run_cmd(argv)
        if proc is None or getattr(proc, "timed_out", False) or getattr(proc, "returncode", 1) != 0:
            return out
        text = getattr(proc, "stdout", "") or ""
        start, end = ollama_journal.run_window(stub)
        rows = ollama_journal.extract_timing(text, start, end)
        prompt_rows = [row for row in rows if row.get("kind") == "prompt"]
        if not prompt_rows:
            return out
        out["tokens_prompt_cache_adjusted"] = sum(int(row["tokens"]) for row in prompt_rows)
        out["journal_llm_calls"] = len(prompt_rows)
        out["tokens_cache_method"] = TOKEN_CACHE_METHOD
        return out
    except Exception:
        return out


def build_result(
    *,
    arm,
    scenario,
    kind,
    court_dependent,
    n,
    run_id,
    channel,
    completion_signal,
    timed_out,
    messages,
    metrics,
    wall_s,
    started_at,
    run_dir,
    error,
) -> dict:
    total, agent, pm, _non_user, _senders = message_counts(messages)
    if completion_signal not in SIGNALS:
        raise HarnessError(f"bad completion_signal {completion_signal!r}")
    obj = {
        "arm": arm,
        "scenario": scenario,
        "kind": kind,
        "court_dependent": bool(court_dependent),
        "n": int(n),
        "run_id": run_id,
        "channel": channel,
        "completion_signal": completion_signal,
        "timed_out": bool(timed_out),
        "pass": None,
        "honesty": None,
        "tokens_prompt": int(metrics["tokens_prompt"]),
        "tokens_prompt_raw": int(metrics["tokens_prompt_raw"]),
        "tokens_prompt_cache_adjusted": _opt_int(metrics.get("tokens_prompt_cache_adjusted")),
        "tokens_cache_method": metrics.get("tokens_cache_method"),
        "tokens_completion": int(metrics["tokens_completion"]),
        "llm_calls": int(metrics["llm_calls"]),
        "journal_llm_calls": _opt_int(metrics.get("journal_llm_calls")),
        "wall_s": float(wall_s),
        "turns": int(metrics["turns"]),
        "turns_source": metrics["turns_source"],
        "messages": int(total),
        "agent_messages": int(agent),
        "pm_messages": int(pm),
        "retries": int(metrics["retries"]),
        "stalls": int(metrics["stalls"]),
        "tokens_unattributed": int(metrics["tokens_unattributed"]),
        "tokens_prompt_unattributed": int(metrics["tokens_prompt_unattributed"]),
        "tokens_completion_unattributed": int(metrics["tokens_completion_unattributed"]),
        "started_at": started_at,
        "run_dir": str(run_dir),
        "error": _clip(error),
    }
    if list(obj) != RESULT_KEYS:
        raise HarnessError("result keys drifted from the contract")
    if obj["turns_source"] not in {"transcript", "trace", "llm_calls"}:
        raise HarnessError("turns_source must be transcript, trace, or llm_calls")
    if obj["tokens_cache_method"] not in CACHE_METHODS:
        raise HarnessError(
            "tokens_cache_method must be "
            "api_prompt_eval_count_full__journal_prompt_eval_new or "
            "api_prompt_eval_count_full__journal_unavailable"
        )
    if obj["tokens_prompt_raw"] != obj["tokens_prompt"]:
        raise HarnessError("tokens_prompt_raw must equal tokens_prompt")
    if obj["tokens_cache_method"] == TOKEN_CACHE_METHOD_UNAVAILABLE:
        if obj["tokens_prompt_cache_adjusted"] is not None or obj["journal_llm_calls"] is not None:
            raise HarnessError(
                "journal unavailable must leave cache-adjusted and journal_llm_calls null"
            )
    elif obj["tokens_prompt_cache_adjusted"] is None or obj["journal_llm_calls"] is None:
        raise HarnessError(
            "journal method requires cache-adjusted tokens and journal_llm_calls"
        )
    if obj["tokens_unattributed"] != (
        obj["tokens_prompt_unattributed"] + obj["tokens_completion_unattributed"]
    ):
        raise HarnessError(
            "tokens_unattributed must equal prompt plus completion unattributed"
        )
    return obj


def _empty_metrics():
    return {
        "tokens_prompt": 0,
        "tokens_prompt_raw": 0,
        "tokens_prompt_cache_adjusted": None,
        "tokens_cache_method": TOKEN_CACHE_METHOD_UNAVAILABLE,
        "tokens_completion": 0,
        "llm_calls": 0,
        "journal_llm_calls": None,
        "turns": 0,
        "turns_source": "llm_calls",
        "retries": 0,
        "stalls": 0,
        "tokens_unattributed": 0,
        "tokens_prompt_unattributed": 0,
        "tokens_completion_unattributed": 0,
    }


def _read_log_slice(path: Path, offset: int) -> str:
    if not path.is_file():
        return ""
    with path.open("rb") as fh:
        fh.seek(0, os.SEEK_END)
        end = fh.tell()
        start = max(0, int(offset))
        if start > end:
            start = end
        # Keep the tail of a huge slice so the read itself cannot grow without bound.
        if end - start > LOG_SLICE_MAX:
            start = end - LOG_SLICE_MAX
        fh.seek(start)
        data = fh.read(end - start)
    return data.decode("utf-8", errors="replace")


def _collab_lines(text: str) -> str:
    lines = [line for line in text.splitlines() if "[collab-trace]" in line]
    if not lines:
        return ""
    return "\n".join(lines) + "\n"


def _parse_vm_ids(text: str, channel: str):
    ids = []
    data = extract_json(text)

    def take(value) -> None:
        if isinstance(value, str) and channel and channel in value:
            ids.append(value)

    nodes = []
    if isinstance(data, list):
        nodes = data
    elif isinstance(data, dict):
        for key in ("vms", "VMs", "items", "data"):
            if isinstance(data.get(key), list):
                nodes = data[key]
                break
    for item in nodes:
        if isinstance(item, dict):
            for key in ("id", "ID", "Id", "name", "Name"):
                if key in item and item[key] is not None:
                    take(str(item[key]))
                    break
        elif isinstance(item, str):
            take(item)
    for line in text.splitlines():
        if not channel or channel not in line:
            continue
        token = line.strip().split()
        if token and channel in token[0]:
            ids.append(token[0])
    out = []
    seen = set()
    for item in ids:
        if item in seen:
            continue
        seen.add(item)
        out.append(item)
    return out[:32]


def _message_signature(messages):
    """(count, last seq, content hash). Timestamps are not part of the signature."""
    if not messages:
        return (0, 0, hashlib.sha256(b"").hexdigest())
    last_seq = max(int(message.get("seq") or 0) for message in messages)
    blob = "\n".join(
        f"{int(message.get('seq') or 0)}\n{message.get('content') or ''}" for message in messages
    )
    digest = hashlib.sha256(blob.encode("utf-8", errors="replace")).hexdigest()
    return (len(messages), last_seq, digest)


def _conclusion_params(scenario: dict):
    conclusion = scenario.get("conclusion") if isinstance(scenario.get("conclusion"), dict) else {}
    try:
        quiet_s = float(conclusion.get("quiet_s", 90))
    except (TypeError, ValueError):
        quiet_s = 90.0
    try:
        min_wait_s = float(conclusion.get("min_wait_s", 60))
    except (TypeError, ValueError):
        min_wait_s = 60.0
    try:
        timeout_s = float(scenario.get("timeout_s", 900))
    except (TypeError, ValueError):
        timeout_s = 900.0
    if quiet_s < 0:
        quiet_s = 0.0
    if min_wait_s < 0:
        min_wait_s = 0.0
    if timeout_s < 0:
        timeout_s = 0.0
    patterns = conclusion.get("final_markers") or []
    if not isinstance(patterns, list):
        patterns = []
    return quiet_s, min_wait_s, timeout_s, patterns


def _kind(scenario: dict) -> str:
    kind = scenario.get("kind")
    if isinstance(kind, str) and kind.strip():
        return kind.strip()
    return "probe"


def _run_seed(seed_rel: str, dest: Path):
    script = Path(seed_rel)
    if not script.is_absolute():
        script = PROFILE_DIR / seed_rel
    dest.mkdir(parents=True, exist_ok=True)
    if not script.is_file():
        return None, f"seed script not found: {script}"
    proc = run_bounded(["bash", str(script), str(dest)], cwd=str(script.parent), timeout=SEED_TIMEOUT_S)
    if proc.timed_out or proc.returncode != 0:
        detail = " ".join((proc.stderr or proc.stdout or "").split())[:500]
        why = "timed out" if proc.timed_out else f"exit {proc.returncode}"
        return proc, f"seed failed ({why}): {detail}"
    return proc, None


def _prepare_goal(scenario: dict, run_dir: Path, dry_run: bool):
    goal = scenario.get("goal") if isinstance(scenario.get("goal"), str) else ""
    seed = scenario.get("seed")
    error = None
    if isinstance(seed, str) and seed.strip():
        _proc, error = _run_seed(seed.strip(), run_dir / "scratch" / "seed")
        block = files_block(run_dir / "scratch" / "seed")
        if error and not dry_run:
            raise HarnessError(error)
    else:
        block = ""
    return apply_goal(goal, block), error


def _publish(out_dir: Path, run_dir: Path, result: dict) -> None:
    append_jsonl(out_dir / "runs.jsonl", result)
    write_result_last(run_dir / "result.json", result)


def _ensure_placeholders(run_dir: Path) -> None:
    text_names = ("transcript.txt", "collab_trace.txt", "daemon_log_slice.txt", "goal.txt")
    for name in text_names:
        path = run_dir / name
        if not path.exists():
            _write_text(path, "")
    if not (run_dir / "transcript.json").exists():
        _write_text(run_dir / "transcript.json", "{}\n")
    if not (run_dir / "polls.jsonl").exists():
        _write_text(run_dir / "polls.jsonl", "")
    if not (run_dir / "usage_before.json").exists():
        _write_json(run_dir / "usage_before.json", [])
    if not (run_dir / "usage_after.json").exists():
        _write_json(run_dir / "usage_after.json", [])
    if not (run_dir / "usage_delta.json").exists():
        _write_json(run_dir / "usage_delta.json", _usage_delta_doc([], [], _empty_metrics()))


def _run_paths(args, scenario_id: str):
    out_dir = Path(args.out)
    run_dir = out_dir / args.arm / scenario_id / f"n{int(args.n)}"
    run_dir.mkdir(parents=True, exist_ok=True)
    return out_dir, run_dir


def _base_identity(args, scenario, scenario_id: str, deterministic: bool):
    channel = make_channel(scenario_id, args.arm, int(args.n), deterministic=deterministic)
    run_id = args.run_id or channel
    return channel, run_id


def run_dry(args, scenario: dict, scenario_id: str) -> int:
    out_dir, run_dir = _run_paths(args, scenario_id)
    channel, run_id = _base_identity(args, scenario, scenario_id, deterministic=True)
    started_at = now_iso()
    goal, seed_error = _prepare_goal(scenario, run_dir, dry_run=True)
    _write_text(run_dir / "goal.txt", goal)
    digest = hashlib.sha256(f"{args.arm}\n{scenario_id}\n{int(args.n)}".encode()).digest()
    prompt = 100 + digest[0]
    completion = 20 + (digest[1] % 50)
    un_prompt = digest[2] % 7
    un_completion = digest[3] % 5
    wall_s = round(10 + (digest[4] % 90) + (digest[5] % 100) / 100.0, 2)
    pre = {
        "agent_id": f"coder-{channel}",
        "tokens_prompt": 1,
        "tokens_completion": 1,
        "timestamp": started_at,
    }
    attr = {
        "agent_id": f"project-manager-{channel}",
        "tokens_prompt": prompt,
        "tokens_completion": completion,
        "timestamp": started_at,
        "model": "dry-run",
    }
    unattr = {
        "agent_id": "boundary",
        "tokens_prompt": un_prompt,
        "tokens_completion": un_completion,
        "timestamp": started_at,
        "model": "dry-run",
    }
    before = [pre]
    after = [pre, attr, unattr]
    attributed, unattributed = usage_delta(before, after, channel, started_at, started_at)
    raw = {
        "id": channel,
        "messages": [
            {"seq": 1, "from": "user", "content": goal, "ts": started_at},
            {
                "seq": 2,
                "from": "project-manager",
                "content": "Plan: inspect the request and delegate one change.",
                "ts": started_at,
            },
            {
                "seq": 3,
                "from": "coder-1",
                "content": "Applied the requested change in the scratch module.",
                "ts": started_at,
            },
            {
                "seq": 4,
                "from": "project-manager",
                "content": "Closeout recorded. No further work queued.",
                "ts": started_at,
            },
        ],
    }
    messages = parse_transcript(raw)
    _write_text(run_dir / "transcript.json", json.dumps(raw, indent=2, ensure_ascii=False) + "\n")
    _write_text(run_dir / "transcript.txt", transcript_text(messages))
    _total, _agent, _pm, _non_user, senders = message_counts(messages)
    poll = {
        "t_s": 0.0,
        "n_messages": len(messages),
        "n_senders": senders,
        "turn_state": "members=2 pending=0",
    }
    _write_text(run_dir / "polls.jsonl", json.dumps(poll, ensure_ascii=False) + "\n")
    _write_json(run_dir / "usage_before.json", before)
    _write_json(run_dir / "usage_after.json", after)
    daemon_slice = (
        f"[collab-trace][project-manager][channel.turn.recv] ch={channel} since=0 new=1\n"
        f"[collab-trace][coder-1][channel.turn.recv] ch={channel} since=1 new=1\n"
        "note: retrying after empty plan\n"
    )
    trace = _collab_lines(daemon_slice)
    metrics = with_transcript_turns(
        account(trace, daemon_slice, attributed, unattributed),
        messages,
        True,
    )
    _write_text(run_dir / "daemon_log_slice.txt", daemon_slice)
    _write_text(run_dir / "collab_trace.txt", trace)
    _write_text(run_dir / "pm_goal_stdout.txt", f"Sent goal to channel {channel} (dry-run)\n")
    delta_doc = _usage_delta_doc(attributed, unattributed, metrics)
    _write_json(run_dir / "usage_delta.json", delta_doc)
    result = build_result(
        arm=args.arm,
        scenario=scenario_id,
        kind=_kind(scenario),
        court_dependent=_as_bool(scenario.get("court_dependent"), False),
        n=int(args.n),
        run_id=run_id,
        channel=channel,
        completion_signal="dry_run",
        timed_out=False,
        messages=messages,
        metrics=metrics,
        wall_s=float(wall_s),
        started_at=started_at,
        run_dir=run_dir,
        error=_clip(seed_error),
    )
    _publish(out_dir, run_dir, result)
    print(f"{result['run_id']} {result['completion_signal']} wall_s={result['wall_s']}")
    return 0


def _poll_once(arm: str, channel: str, scenario=None):
    get_proc = aegis(["channel", "get", "--json", channel], arm, timeout=CLI_TIMEOUT_S)
    turn_proc = aegis(["channel", "turn-state", "--json", channel], arm, timeout=CLI_TIMEOUT_S)
    parsed = extract_json(get_proc.stdout) if not get_proc.timed_out else None
    if parsed is None and not get_proc.timed_out:
        parsed = extract_json((get_proc.stdout or "") + "\n" + (get_proc.stderr or ""))
    fetch_ok = parsed is not None and not get_proc.timed_out
    messages = parse_transcript(parsed) if fetch_ok else None
    turn_obj = None
    if not turn_proc.timed_out:
        turn_obj = extract_json(turn_proc.stdout)
        if turn_obj is None:
            turn_obj = extract_json((turn_proc.stdout or "") + "\n" + (turn_proc.stderr or ""))
    extra = []
    mode = messaging_mode(arm, scenario)
    if mode == "dm":
        # A missing dump must not fail the poll. Channel get still stands.
        try:
            extra = collect_dm_messages(arm, channel)
        except Exception:
            extra = []
        if messages is None and extra:
            messages = []
            fetch_ok = True
        if messages is not None:
            _quiet_s, _min_wait_s, _timeout_s, patterns = _conclusion_params(
                scenario if isinstance(scenario, dict) else {}
            )
            messages = merge_transcripts(messages, extra, patterns)
    return {
        "fetch_ok": fetch_ok,
        "messages": messages,
        "raw": get_proc.stdout or "",
        "turn": turn_obj,
        "get_proc": get_proc,
        "dm_messages": len(extra),
    }


def _collect_trace(arm: str, channel: str, out_dir: Path, log_offset: int):
    daemon_log = out_dir / arm / "daemon.log"
    daemon_slice = _read_log_slice(daemon_log, log_offset)
    vm_chunks = []
    try:
        listing = aegis(["vm", "list", "--json"], arm, timeout=CLI_TIMEOUT_S)
    except (OSError, ValueError) as exc:
        listing = None
        vm_chunks.append(f"vm list failed: {exc}")
    else:
        if listing.timed_out:
            vm_chunks.append("vm list TIMEOUT")
        else:
            ids = _parse_vm_ids((listing.stdout or "") + "\n" + (listing.stderr or ""), channel)
            for vm_id in ids:
                try:
                    logs = aegis(["vm", "logs", vm_id], arm, timeout=CLI_TIMEOUT_S)
                except OSError as exc:
                    vm_chunks.append(f"vm logs {vm_id} failed: {exc}")
                    continue
                vm_chunks.append(logs.stdout or "")
                if logs.stderr:
                    vm_chunks.append(logs.stderr)
    vm_text = "\n".join(vm_chunks)
    window = daemon_slice + ("\n" if daemon_slice and vm_text else "") + vm_text
    trace = _collab_lines(window)
    return daemon_slice, trace, window


def run_real(args, scenario: dict, scenario_id: str) -> int:
    out_dir, run_dir = _run_paths(args, scenario_id)
    channel, run_id = _base_identity(args, scenario, scenario_id, deterministic=False)
    goal, _seed_error = _prepare_goal(scenario, run_dir, dry_run=False)
    _write_text(run_dir / "goal.txt", goal)
    quiet_s, min_wait_s, timeout_s, patterns = _conclusion_params(scenario)
    polls_path = run_dir / "polls.jsonl"
    _write_text(polls_path, "")

    before = usage_snapshot()
    _write_json(run_dir / "usage_before.json", before)
    daemon_log = out_dir / args.arm / "daemon.log"
    try:
        log_offset = daemon_log.stat().st_size if daemon_log.is_file() else 0
    except OSError:
        log_offset = 0

    started_at = now_iso()
    t0_mono = time.monotonic()
    messages = []
    last_raw = ""
    last_change_mono = None
    prev_sig = None
    goal_error = None
    goal_accepted_mono = None
    saw_channel = False

    try:
        pm = aegis(["pm", "goal", goal, "--channel", channel], args.arm, timeout=PM_GOAL_TIMEOUT_S)
    except (OSError, ValueError) as exc:
        raise HarnessError(f"pm goal failed to start: {exc}") from exc
    stdout = pm.stdout or ""
    stderr = pm.stderr or ""
    combined_goal = stdout if not stderr else stdout + ("\n--- stderr ---\n" + stderr)
    _write_text(run_dir / "pm_goal_stdout.txt", combined_goal)
    combined_check = stdout + "\n" + stderr
    # CLI wording varies by build: older trees print "Sent goal to"; current
    # trees print "Posted goal to channel" after the ensure + channel.post path.
    goal_accepted = (
        "Sent goal to" in combined_check
        or "Posted goal to channel" in combined_check
        or "Posted goal to" in combined_check
    )
    if pm.timed_out or not goal_accepted:
        if pm.timed_out:
            goal_error = "pm goal timed out after 240s"
        else:
            snippet = " ".join(combined_check.split())[:500]
            goal_error = (
                "pm goal output missing acceptance marker "
                f"(Sent/Posted goal): {snippet}"
            )
        short_deadline = min(t0_mono + timeout_s, time.monotonic() + SHORT_POLL_S)
    else:
        goal_accepted_mono = time.monotonic()
        # Empty baseline: the first poll moves the clock only if the set differs.
        last_change_mono = goal_accepted_mono
        prev_sig = _message_signature([])
        short_deadline = None

    hard_deadline = t0_mono + timeout_s
    signal = None
    transcript_collected = False
    poll_s = float(args.poll_s)
    sleep_s = poll_s if poll_s > 0 else 0.05

    while signal is None:
        now_mono = time.monotonic()
        if now_mono >= hard_deadline:
            signal = "timeout"
            break
        if short_deadline is not None and now_mono >= short_deadline:
            signal = "error"
            break
        remaining = hard_deadline - now_mono
        if short_deadline is not None:
            remaining = min(remaining, short_deadline - now_mono)
        if remaining <= 0:
            continue

        try:
            sample = _poll_once(args.arm, channel, scenario)
        except (OSError, ValueError) as exc:
            sample = {"fetch_ok": False, "messages": None, "raw": "", "turn": None, "dm_messages": 0}
            goal_error = goal_error or f"poll failed: {exc}"
        now_mono = time.monotonic()
        turn_available, any_pending, turn_summary = inspect_turn_state(sample.get("turn"))
        if sample.get("fetch_ok"):
            saw_channel = True
            transcript_collected = True
            messages = sample["messages"] or []
            last_raw = sample.get("raw") or ""
            sig = _message_signature(messages)
            if sig != prev_sig:
                last_change_mono = now_mono
                prev_sig = sig
            total, _agent, _pm, _non_user, senders = message_counts(messages)
            summary = turn_summary
        else:
            total, _agent, _pm, _non_user, senders = message_counts(messages)
            summary = "channel_get_failed; " + turn_summary
        poll = {
            "t_s": round(now_mono - t0_mono, 3),
            "n_messages": total,
            "n_senders": senders,
            "turn_state": summary,
            "dm_messages": int(sample.get("dm_messages") or 0),
        }
        with polls_path.open("a", encoding="utf-8") as fh:
            fh.write(json.dumps(poll, ensure_ascii=False) + "\n")

        if goal_accepted_mono is not None and saw_channel:
            elapsed_accept = now_mono - goal_accepted_mono
            quiet_for = silence_seconds(last_change_mono, now_mono)
            chosen = judge_signal(
                messages,
                elapsed_accept,
                quiet_for,
                turn_available,
                any_pending,
                min_wait_s,
                quiet_s,
                patterns,
            )
            if chosen:
                signal = chosen
                break
        if now_mono >= hard_deadline:
            signal = "timeout"
            break
        if short_deadline is not None and now_mono >= short_deadline:
            signal = "error"
            break
        nap = min(sleep_s, max(0.0, hard_deadline - time.monotonic()))
        if short_deadline is not None:
            nap = min(nap, max(0.0, short_deadline - time.monotonic()))
        if nap > 0:
            time.sleep(nap)

    decided_mono = time.monotonic()
    wall_s = round(decided_mono - t0_mono, 3)
    timed_out = signal == "timeout"
    if signal == "error" and not goal_error:
        goal_error = "goal was not accepted"
    if signal != "error":
        # Keep a goal-acceptance failure visible even when the hard cap wins.
        error_text = goal_error if signal == "timeout" else None
    else:
        error_text = goal_error or "run ended in error"

    try:
        final = _poll_once(args.arm, channel, scenario)
    except (OSError, ValueError):
        final = {"fetch_ok": False, "messages": None, "raw": ""}
    if final.get("fetch_ok"):
        transcript_collected = True
        messages = final["messages"] or []
        last_raw = final.get("raw") or ""
    if messaging_mode(args.arm, scenario) == "dm":
        _write_json(run_dir / "transcript.json", {"messages": messages})
    elif last_raw.strip():
        _write_text(run_dir / "transcript.json", last_raw if last_raw.endswith("\n") else last_raw + "\n")
    else:
        _write_text(run_dir / "transcript.json", json.dumps({"messages": messages}, indent=2) + "\n")
    _write_text(run_dir / "transcript.txt", transcript_text(messages))

    time.sleep(USAGE_SETTLE_S)
    after = usage_snapshot()
    _write_json(run_dir / "usage_after.json", after)
    t1_iso = now_iso()
    attributed, unattributed = usage_delta(before, after, channel, started_at, t1_iso)
    try:
        daemon_slice, trace, window = _collect_trace(args.arm, channel, out_dir, log_offset)
    except (OSError, ValueError) as exc:
        daemon_slice, trace, window = "", "", ""
        error_text = error_text or f"trace collection failed: {exc}"
    _write_text(run_dir / "daemon_log_slice.txt", daemon_slice)
    _write_text(run_dir / "collab_trace.txt", trace)
    metrics = with_transcript_turns(
        account(trace, window, attributed, unattributed),
        messages,
        transcript_collected,
    )
    try:
        metrics = apply_journal_cache(metrics, started_at, wall_s)
    except Exception:
        metrics["tokens_prompt_cache_adjusted"] = None
        metrics["journal_llm_calls"] = None
        metrics["tokens_cache_method"] = TOKEN_CACHE_METHOD_UNAVAILABLE
    _write_json(run_dir / "usage_delta.json", _usage_delta_doc(attributed, unattributed, metrics))
    result = build_result(
        arm=args.arm,
        scenario=scenario_id,
        kind=_kind(scenario),
        court_dependent=_as_bool(scenario.get("court_dependent"), False),
        n=int(args.n),
        run_id=run_id,
        channel=channel,
        completion_signal=signal or "error",
        timed_out=timed_out,
        messages=messages,
        metrics=metrics,
        wall_s=float(wall_s),
        started_at=started_at,
        run_dir=run_dir,
        error=error_text,
    )
    _publish(out_dir, run_dir, result)
    print(f"{result['run_id']} {result['completion_signal']} wall_s={result['wall_s']}")
    return 0


def _error_result(args, scenario_id, channel, run_id, started_at, run_dir, message) -> dict:
    return build_result(
        arm=getattr(args, "arm", ""),
        scenario=scenario_id or "",
        kind="probe",
        court_dependent=False,
        n=int(getattr(args, "n", 0) or 0),
        run_id=run_id or channel or "error",
        channel=channel or "unknown",
        completion_signal="error",
        timed_out=False,
        messages=[],
        metrics=_empty_metrics(),
        wall_s=0.0,
        started_at=started_at or now_iso(),
        run_dir=run_dir,
        error=str(message),
    )


def _emit_harness_error(args, scenario, scenario_id, exc) -> int:
    started_at = now_iso()
    try:
        scenario_id = scenario_id or (Path(args.scenario).stem if getattr(args, "scenario", None) else "unknown")
        _check_segment("arm", args.arm)
        if scenario_id != "unknown":
            _check_segment("scenario id", scenario_id)
        out_dir, run_dir = _run_paths(args, scenario_id if scenario_id != "unknown" else "unknown")
        channel, run_id = _base_identity(args, scenario or {}, scenario_id, deterministic=False)
    except Exception as inner:
        print(f"harness error: {exc}; also failed to record it: {inner}", file=sys.stderr)
        return 1
    try:
        _ensure_placeholders(run_dir)
        if not (run_dir / "goal.txt").stat().st_size and isinstance(scenario, dict):
            goal = scenario.get("goal") if isinstance(scenario.get("goal"), str) else ""
            _write_text(run_dir / "goal.txt", goal)
        result = _error_result(args, scenario_id, channel, run_id, started_at, run_dir, exc)
        _publish(out_dir, run_dir, result)
    except Exception as inner:
        print(f"harness error: {exc}; also failed to write result: {inner}", file=sys.stderr)
        return 1
    print(f"harness error: {exc}", file=sys.stderr)
    return 1


def parse_args(argv=None):
    parser = argparse.ArgumentParser(description="Run one scenario on one profiling arm")
    parser.add_argument("--arm", required=True)
    parser.add_argument("--scenario", required=True)
    parser.add_argument("--n", type=int, required=True)
    parser.add_argument("--out", required=True)
    parser.add_argument("--dry-run", action="store_true")
    parser.add_argument("--run-id", default=None)
    parser.add_argument("--poll-s", type=float, default=5)
    return parser.parse_args(argv)


def main(argv=None) -> int:
    args = parse_args(argv)
    if not math_ok(args.poll_s) or int(args.n) < 1:
        print("run_one: --n must be >= 1 and --poll-s must be finite", file=sys.stderr)
        return 2
    try:
        _check_segment("arm", args.arm)
    except HarnessError as exc:
        print(f"run_one: {exc}", file=sys.stderr)
        return 2
    try:
        args.arm = canonical_arm(args.arm)
    except KeyError as exc:
        print(f"run_one: {exc}", file=sys.stderr)
        return 2
    except (OSError, ValueError, json.JSONDecodeError) as exc:
        print(f"run_one: arms.json: {exc}", file=sys.stderr)
        return 2
    try:
        scenario = load_scenario(args.scenario)
        scenario_id = _check_segment("scenario id", _scenario_id(scenario, args.scenario))
    except (OSError, ValueError, json.JSONDecodeError, HarnessError) as exc:
        print(f"run_one: scenario: {exc}", file=sys.stderr)
        return 1
    try:
        if args.dry_run:
            return run_dry(args, scenario, scenario_id)
        return run_real(args, scenario, scenario_id)
    except HarnessError as exc:
        if args.dry_run:
            print(f"dry-run harness error: {exc}", file=sys.stderr)
            try:
                return _emit_dry_error(args, scenario, scenario_id, exc)
            except Exception as inner:
                print(f"dry-run failed to record error: {inner}", file=sys.stderr)
                return 1
        return _emit_harness_error(args, scenario, scenario_id, exc)
    except Exception as exc:
        if args.dry_run:
            print(f"dry-run harness error: {exc}", file=sys.stderr)
            try:
                return _emit_dry_error(args, scenario, scenario_id, exc)
            except Exception as inner:
                print(f"dry-run failed to record error: {inner}", file=sys.stderr)
                return 1
        return _emit_harness_error(args, scenario, scenario_id, exc)


def math_ok(value) -> bool:
    try:
        number = float(value)
    except (TypeError, ValueError):
        return False
    return number == number and number not in (float("inf"), float("-inf"))


def _emit_dry_error(args, scenario, scenario_id, exc) -> int:
    """Dry-run still writes a dry_run line and exits 0 when the out dir is usable."""
    if scenario_id is None:
        scenario_id = Path(args.scenario).stem if getattr(args, "scenario", None) else "unknown"
    try:
        _check_segment("arm", args.arm)
        _check_segment("scenario id", scenario_id)
    except HarnessError:
        return 1
    out_dir, run_dir = _run_paths(args, scenario_id)
    channel, run_id = _base_identity(args, scenario or {}, scenario_id, deterministic=True)
    started_at = now_iso()
    _ensure_placeholders(run_dir)
    result = build_result(
        arm=args.arm,
        scenario=scenario_id,
        kind=_kind(scenario or {}),
        court_dependent=_as_bool((scenario or {}).get("court_dependent"), False) if isinstance(scenario, dict) else False,
        n=int(args.n),
        run_id=run_id,
        channel=channel,
        completion_signal="dry_run",
        timed_out=False,
        messages=[],
        metrics=_empty_metrics(),
        wall_s=0.0,
        started_at=started_at,
        run_dir=run_dir,
        error=str(exc),
    )
    _publish(out_dir, run_dir, result)
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except BrokenPipeError:
        sys.exit(0)
