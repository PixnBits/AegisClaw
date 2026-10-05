#!/usr/bin/env python3
"""Shared helpers for the profiling harness. Python 3 standard library only."""

from __future__ import annotations

import json
import os
import signal
import subprocess
import urllib.error
import urllib.request
from datetime import datetime, timezone
from pathlib import Path

SENDER_KEYS = ("from", "sender", "From", "Sender", "author", "Author")
CONTENT_KEYS = ("content", "text", "Content", "Text", "body")
_MESSAGE_LIST_KEYS = ("messages", "Messages")
_NEST_KEYS = ("channel", "Channel", "data", "payload", "result", "Data", "Payload")
_HARNESS_ROLES = {"user", "pm", "court", "agent", "system"}
# Tried only when the arm does not document its own dm_cli / harness_dm.json cli.
_DEFAULT_DM_CLI = (
    ["dm", "dump", "--json", "{channel}"],
    ["chat", "dump", "--json", "{channel}"],
)
_DEFAULT_DM_FILE = "scripts/profile/dm-dump/{channel}.json"
DM_DUMP_TIMEOUT_S = 5
_MISSING_DM_CMDS: set[tuple] = set()


def repo_root() -> Path:
    """Repository root that contains scripts/profile."""
    return Path(__file__).resolve().parents[2]


def _profile_dir() -> Path:
    return Path(__file__).resolve().parent


def load_arms() -> dict:
    path = _profile_dir() / "arms.json"
    with path.open(encoding="utf-8") as fh:
        data = json.load(fh)
    if not isinstance(data, dict):
        raise ValueError("arms.json must be a JSON object")
    return data


def canonical_arm(arm: str) -> str:
    """Arm id as stored in arms.json. Aliases dm and ste resolve to A and B."""
    data = load_arms()
    arms = data.get("arms") if isinstance(data, dict) else None
    if not isinstance(arms, dict):
        raise KeyError("arms.json missing arms")
    name = (arm or "").strip()
    if name in arms:
        return name
    aliases = data.get("aliases") if isinstance(data, dict) else None
    if isinstance(aliases, dict):
        target = aliases.get(name)
        if isinstance(target, str) and target in arms:
            return target
    known = sorted(list(arms) + [key for key in (aliases or {}) if isinstance(key, str)])
    raise KeyError(f"unknown arm {name!r}; known: {known}")


def arm_spec(arm: str) -> dict:
    name = canonical_arm(arm)
    spec = load_arms()["arms"][name]
    if not isinstance(spec, dict):
        raise KeyError(f"arm {name!r} spec must be an object")
    return spec


def arm_build_dir(arm: str) -> Path:
    name = canonical_arm(arm)
    spec = arm_spec(name)
    build = spec.get("build_dir")
    if not isinstance(build, str) or not build.strip():
        raise KeyError(f"arm {name!r} missing build_dir")
    return (repo_root() / build).resolve()


def arm_messaging(arm: str) -> str:
    """channel (default) or dm, from the arm spec. Scenario override is the caller's."""
    raw = arm_spec(arm).get("messaging")
    if isinstance(raw, str) and raw.strip():
        return raw.strip().lower()
    return "channel"


def load_scenario(path) -> dict:
    candidate = Path(path)
    if not candidate.is_file():
        alt = _profile_dir() / path
        if alt.is_file():
            candidate = alt
    with candidate.open(encoding="utf-8") as fh:
        data = json.load(fh)
    if not isinstance(data, dict):
        raise ValueError(f"scenario must be a JSON object: {path}")
    return data


def classify_sender(name: str) -> str:
    """user | pm | court | agent | system.

    Court names start with 'court'; PM starts with 'project-manager'.
    Facilitator status posts use sender 'system' and are none of the others.
    """
    n = (name or "").strip().lower()
    if n in {"user", "human"}:
        return "user"
    if n.startswith("project-manager"):
        return "pm"
    if n.startswith("court"):
        return "court"
    if n == "system":
        return "system"
    return "agent"


def harness_role(entry: dict, sender: str) -> str:
    """Prefer an explicit harness role. Other role strings (member roles) do not count."""
    if isinstance(entry, dict):
        for key in ("role", "Role"):
            value = entry.get(key)
            if isinstance(value, str) and value.strip().lower() in _HARNESS_ROLES:
                return value.strip().lower()
    return classify_sender(sender)


def now_iso() -> str:
    """UTC timestamp, RFC3339."""
    return datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%S.%fZ")


def parse_iso(value):
    """Parse an RFC3339/ISO-8601 timestamp to aware UTC, or return None."""
    if not isinstance(value, str):
        return None
    text = value.strip()
    if not text:
        return None
    if text.endswith("Z"):
        text = text[:-1] + "+00:00"
    try:
        parsed = datetime.fromisoformat(text)
    except ValueError:
        return None
    if parsed.tzinfo is None:
        parsed = parsed.replace(tzinfo=timezone.utc)
    return parsed.astimezone(timezone.utc)


def extract_json(text):
    """Parse a JSON value, tolerating leading log lines. Dict/list inputs pass through."""
    if isinstance(text, (dict, list)):
        return text
    if text is None:
        return None
    if isinstance(text, bytes):
        text = text.decode("utf-8", errors="replace")
    if not isinstance(text, str):
        return None
    stripped = text.strip()
    if not stripped:
        return None
    try:
        return json.loads(stripped)
    except json.JSONDecodeError:
        pass
    decoder = json.JSONDecoder()
    for index, ch in enumerate(stripped):
        if ch not in "{[":
            continue
        try:
            obj, _end = decoder.raw_decode(stripped[index:])
        except json.JSONDecodeError:
            continue
        return obj
    return None


def _has_sender(entry: dict) -> bool:
    for key in SENDER_KEYS:
        value = entry.get(key)
        if isinstance(value, str) and value.strip():
            return True
    return False


def _has_content(entry: dict) -> bool:
    for key in CONTENT_KEYS:
        if key in entry and entry[key] not in (None, ""):
            return True
    return False


def _list_is_messages(items) -> bool:
    if not isinstance(items, list) or not items:
        return False
    for item in items:
        if isinstance(item, dict) and _has_sender(item) and _has_content(item):
            return True
    return False


def _find_messages(data):
    """Prefer a messages/Messages list; else any list of sender+content dicts."""
    keyed = []
    guessed = []

    def walk(node, depth: int) -> None:
        if depth > 8 or node is None:
            return
        if isinstance(node, dict):
            for key, value in node.items():
                if key in _MESSAGE_LIST_KEYS and isinstance(value, list):
                    keyed.append(value)
                    continue
                walk(value, depth + 1)
            return
        if isinstance(node, list):
            if _list_is_messages(node):
                guessed.append(node)
            for item in node:
                walk(item, depth + 1)

    walk(data, 0)
    for items in keyed:
        if items:
            return items
    for items in guessed:
        if items:
            return items
    if keyed:
        return keyed[0]
    if guessed:
        return guessed[0]
    return []


def _message_from(entry: dict) -> str:
    for key in SENDER_KEYS:
        value = entry.get(key)
        if isinstance(value, str) and value.strip():
            return value.strip()
    return ""


def _message_content(entry: dict) -> str:
    for key in CONTENT_KEYS:
        if key not in entry:
            continue
        value = entry[key]
        if isinstance(value, str):
            return value
        if isinstance(value, dict):
            nested = _message_content(value)
            if nested:
                return nested
        if isinstance(value, list):
            parts = []
            for item in value:
                if isinstance(item, str):
                    parts.append(item)
                elif isinstance(item, dict):
                    parts.append(_message_content(item))
            joined = "\n".join(part for part in parts if part)
            if joined:
                return joined
    return ""


def _message_seq(entry: dict):
    for key in ("seq", "Seq", "sequence"):
        if key not in entry:
            continue
        value = entry[key]
        if isinstance(value, bool) or value is None:
            continue
        if isinstance(value, int):
            return value
        if isinstance(value, float):
            return int(value)
        if isinstance(value, str):
            try:
                return int(float(value.strip()))
            except ValueError:
                continue
    return None


def _message_ts(entry: dict):
    for key in ("ts", "timestamp", "time", "Timestamp"):
        value = entry.get(key)
        if isinstance(value, str) and value.strip():
            return value.strip()
    return None


def parse_transcript(channel_get_json) -> list:
    """Tolerant channel-get parse. Always returns seq/from/role/content/ts dicts."""
    data = channel_get_json
    if not isinstance(data, (dict, list)):
        data = extract_json(data)
    messages = _find_messages(data)
    parsed = []
    next_seq = 1
    for entry in messages:
        if not isinstance(entry, dict):
            continue
        seq = _message_seq(entry)
        if seq is None:
            seq = next_seq
        next_seq = max(next_seq, int(seq) + 1)
        sender = _message_from(entry)
        parsed.append(
            {
                "seq": int(seq),
                "from": sender,
                "role": harness_role(entry, sender),
                "content": _message_content(entry),
                "ts": _message_ts(entry),
            }
        )
    return parsed


def _dm_subst(text: str, channel: str, build: Path) -> str:
    return (
        text.replace("{channel}", channel).replace("{build_dir}", str(build))
    )


def _inside_build(path: Path, build: Path) -> bool:
    try:
        path.resolve().relative_to(build.resolve())
    except ValueError:
        return False
    return True


def _dm_artifact_path(template: str, channel: str, build: Path):
    """A documented dump path, or None when it would leave the arm build dir."""
    if not isinstance(template, str) or not template.strip():
        return None
    raw = _dm_subst(template.strip(), channel, build)
    path = Path(raw)
    if not path.is_absolute():
        path = build / path
    if not _inside_build(path, build):
        return None
    return path


def _take_dm_cli(doc: dict, clis: list, saw_cli: list) -> None:
    raw = doc.get("cli")
    if "cli" not in doc or raw is None:
        raw = doc.get("dm_cli")
        if "dm_cli" not in doc or raw is None:
            return
    saw_cli.append(True)
    if not isinstance(raw, list):
        return
    if raw and all(isinstance(part, str) for part in raw):
        clis.append([str(part) for part in raw])
        return
    for item in raw:
        if isinstance(item, list) and item and all(isinstance(part, str) for part in item):
            clis.append([str(part) for part in item])


def _take_dm_files(doc: dict, files: list) -> None:
    raw = doc.get("files")
    if raw is None and "dm_files" in doc:
        raw = doc.get("dm_files")
    if not isinstance(raw, list):
        return
    for item in raw:
        if isinstance(item, str) and item.strip():
            files.append(item.strip())


def dm_sources(spec: dict, build: Path):
    """(cli argv templates, file templates, cli_was_documented).

    File list always includes the conventional dm-dump path. CLI falls back to
    dm dump / chat dump only when the arm did not document a cli key.
    """
    clis: list = []
    files: list = []
    saw_cli: list = []
    if isinstance(spec, dict):
        _take_dm_cli(spec, clis, saw_cli)
        _take_dm_files(spec, files)
    doc_path = build / "scripts" / "profile" / "harness_dm.json"
    if doc_path.is_file():
        try:
            doc = extract_json(doc_path.read_text(encoding="utf-8"))
        except OSError:
            doc = None
        if isinstance(doc, dict):
            _take_dm_cli(doc, clis, saw_cli)
            _take_dm_files(doc, files)
    if not saw_cli:
        clis.extend([list(argv) for argv in _DEFAULT_DM_CLI])
    files.append(_DEFAULT_DM_FILE)
    unique_cli = []
    seen_cli = set()
    for argv in clis:
        key = tuple(argv)
        if key in seen_cli:
            continue
        seen_cli.add(key)
        unique_cli.append(argv)
    unique_files = []
    seen_files = set()
    for item in files:
        if item in seen_files:
            continue
        seen_files.add(item)
        unique_files.append(item)
    return unique_cli, unique_files, bool(saw_cli)


def _cmd_missing(proc) -> bool:
    text = f"{getattr(proc, 'stderr', '') or ''}\n{getattr(proc, 'stdout', '') or ''}".lower()
    return "unknown command" in text or "unknown flag" in text


def collect_dm_messages(arm, channel, *, run_cmd=None, spec=None, build=None) -> list:
    """Best-effort DM transcript. Missing commands and bad files yield no rows.

    Does not raise for a dump the product has not shipped. `run_cmd` receives
    the substituted argv and returns an object with stdout, stderr, timed_out.
    """
    name = (arm or "").strip()
    if spec is None or build is None:
        name = canonical_arm(arm)
        if spec is None:
            spec = arm_spec(name)
        if build is None:
            build = arm_build_dir(name)
    build = Path(build)
    channel_id = "" if channel is None else str(channel)
    clis, files, _documented = dm_sources(spec if isinstance(spec, dict) else {}, build)
    messages = []
    for template in files:
        path = _dm_artifact_path(template, channel_id, build)
        if path is None or not path.is_file():
            continue
        try:
            payload = extract_json(path.read_text(encoding="utf-8"))
        except OSError:
            continue
        messages.extend(parse_transcript(payload))
    if run_cmd is None:
        def run_cmd(argv, arm_name=name):
            return aegis(argv, arm_name, timeout=DM_DUMP_TIMEOUT_S)
    for template in clis:
        key = (name, tuple(template))
        if key in _MISSING_DM_CMDS:
            continue
        argv = [_dm_subst(part, channel_id, build) for part in template]
        try:
            proc = run_cmd(argv)
        except FileNotFoundError:
            _MISSING_DM_CMDS.add(key)
            continue
        except (OSError, ValueError):
            continue
        if _cmd_missing(proc):
            _MISSING_DM_CMDS.add(key)
            continue
        if getattr(proc, "timed_out", False):
            continue
        stdout = getattr(proc, "stdout", "") or ""
        stderr = getattr(proc, "stderr", "") or ""
        payload = extract_json(stdout)
        parsed = parse_transcript(payload)
        if not parsed:
            parsed = parse_transcript(extract_json(stdout + "\n" + stderr))
        messages.extend(parsed)
    return messages


def http_get_json(url, timeout=10):
    timeout = 10.0 if timeout is None else float(timeout)
    if timeout <= 0:
        raise ValueError("timeout must be positive")
    request = urllib.request.Request(url, headers={"Accept": "application/json"})
    with urllib.request.urlopen(request, timeout=timeout) as response:
        raw = response.read()
    return json.loads(raw.decode("utf-8"))


def usage_snapshot(base="http://localhost:8080", limit=500) -> list:
    """GET /api/llm-usage/recent?limit=N -> records. Empty list on any failure."""
    try:
        limit_n = int(limit)
    except (TypeError, ValueError):
        limit_n = 500
    if limit_n <= 0:
        limit_n = 500
    root = (base or "http://localhost:8080").rstrip("/")
    url = f"{root}/api/llm-usage/recent?limit={limit_n}"
    try:
        data = http_get_json(url, timeout=10)
    except (OSError, urllib.error.URLError, ValueError, json.JSONDecodeError, TimeoutError):
        return []
    raw = None
    if isinstance(data, dict):
        for key in ("records", "Records", "items"):
            if isinstance(data.get(key), list):
                raw = data[key]
                break
        if raw is None and isinstance(data.get("data"), list):
            raw = data["data"]
        if raw is None and isinstance(data.get("data"), dict):
            inner = data["data"]
            for key in ("records", "Records"):
                if isinstance(inner.get(key), list):
                    raw = inner[key]
                    break
    elif isinstance(data, list):
        raw = data
    if not isinstance(raw, list):
        return []
    return [row for row in raw if isinstance(row, dict)]


def _canon_record(record: dict) -> str:
    return json.dumps(record, sort_keys=True, default=str, ensure_ascii=False)


def _record_time(record: dict):
    for key in ("timestamp", "ts", "time", "created_at"):
        parsed = parse_iso(record.get(key))
        if parsed is not None:
            return parsed
    return None


def _in_window(record: dict, t0, t1) -> bool:
    stamped = _record_time(record)
    if stamped is None or t0 is None or t1 is None:
        return True
    start, end = (t0, t1) if t0 <= t1 else (t1, t0)
    return start <= stamped <= end


def _agent_id(record: dict) -> str:
    value = record.get("agent_id")
    if value is None:
        value = record.get("agent")
    if isinstance(value, dict):
        value = value.get("id") or value.get("agent_id") or ""
    return "" if value is None else str(value)


def usage_delta(before, after, channel, t0_iso, t1_iso):
    """Multiset difference after-before.

    Attributed records are those whose agent_id contains the channel id.
    Records with a timestamp outside [t0, t1] are dropped; missing timestamps stay.
    Returns (attributed_records, unattributed_records).
    """
    before_counts = {}
    for record in before or []:
        if not isinstance(record, dict):
            continue
        key = _canon_record(record)
        before_counts[key] = before_counts.get(key, 0) + 1
    remaining = dict(before_counts)
    t0 = parse_iso(t0_iso)
    t1 = parse_iso(t1_iso)
    attributed = []
    unattributed = []
    channel_id = "" if channel is None else str(channel)
    for record in after or []:
        if not isinstance(record, dict):
            continue
        key = _canon_record(record)
        if remaining.get(key, 0) > 0:
            remaining[key] -= 1
            continue
        if not _in_window(record, t0, t1):
            continue
        agent = _agent_id(record)
        if channel_id and channel_id in agent:
            attributed.append(record)
        else:
            unattributed.append(record)
    return attributed, unattributed


class CmdResult:
    def __init__(self, returncode: int, stdout: str, stderr: str, timed_out: bool):
        self.returncode = returncode
        self.stdout = stdout
        self.stderr = stderr
        self.timed_out = timed_out


def _decode(data) -> str:
    if not data:
        return ""
    if isinstance(data, str):
        return data
    return data.decode("utf-8", errors="replace")


def _kill_pgroup(proc: subprocess.Popen) -> None:
    if proc.poll() is not None:
        return
    try:
        os.killpg(proc.pid, signal.SIGTERM)
    except (ProcessLookupError, PermissionError, OSError):
        try:
            proc.kill()
        except (ProcessLookupError, PermissionError, OSError):
            return
    try:
        proc.wait(timeout=2)
        return
    except subprocess.TimeoutExpired:
        pass
    try:
        os.killpg(proc.pid, signal.SIGKILL)
    except (ProcessLookupError, PermissionError, OSError):
        try:
            proc.kill()
        except (ProcessLookupError, PermissionError, OSError):
            return
    try:
        proc.wait(timeout=2)
    except subprocess.TimeoutExpired:
        pass


def run_bounded(argv, cwd, timeout, env=None) -> CmdResult:
    """Run argv with a hard timeout. Never waits forever. Kills the process group."""
    timeout_s = float(timeout)
    if timeout_s <= 0:
        raise ValueError("timeout must be positive")
    proc = subprocess.Popen(
        [str(part) for part in argv],
        cwd=None if cwd is None else str(cwd),
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        stdin=subprocess.DEVNULL,
        start_new_session=True,
        env=env,
    )
    try:
        out_b, err_b = proc.communicate(timeout=timeout_s)
    except subprocess.TimeoutExpired:
        _kill_pgroup(proc)
        try:
            out_b, err_b = proc.communicate(timeout=5)
        except subprocess.TimeoutExpired:
            _kill_pgroup(proc)
            out_b, err_b = b"", b""
            try:
                proc.wait(timeout=2)
            except subprocess.TimeoutExpired:
                pass
        return CmdResult(124, _decode(out_b), _decode(err_b) + "\nTIMEOUT", True)
    code = proc.returncode if proc.returncode is not None else 1
    return CmdResult(code, _decode(out_b), _decode(err_b), False)


def aegis(args, arm, timeout) -> CmdResult:
    """Run <build_dir>/bin/aegis with cwd=build_dir. Never uses sudo."""
    if timeout is None or float(timeout) <= 0:
        raise ValueError("timeout must be positive")
    build = arm_build_dir(arm)
    binary = build / "bin" / "aegis"
    return run_bounded([str(binary), *[str(part) for part in args]], cwd=build, timeout=float(timeout))


def as_int(value) -> int:
    if isinstance(value, bool) or value is None:
        return 0
    if isinstance(value, int):
        return value
    if isinstance(value, float):
        return int(value)
    if isinstance(value, str):
        try:
            return int(float(value.strip()))
        except ValueError:
            return 0
    return 0


if __name__ == "__main__":
    print(f"repo_root={repo_root()}")
    loaded = load_arms()
    names = sorted((loaded.get("arms") or {}))
    print("arms=" + ",".join(names))
