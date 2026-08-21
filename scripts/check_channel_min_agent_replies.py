#!/usr/bin/env python3
"""Assert a minimum number of real agent channel.post replies after a user post.

Regression guard for the hubclient decoder race: fan-out can succeed while zero
agents reach the store if LLM responses are stolen by a concurrent Receive loop.
"""
import json
import os
import re
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from check_channel_portal_fanout import (
    CANNED_INTRO_RE,
    is_canned_intro,
    load_channel,
    normalize_from,
    usable_content,
)

AGENT_FROM_RE = re.compile(r"^(project-manager|court-persona-[a-z0-9-]+)")


def is_agent_from(frm: str) -> bool:
    return bool(AGENT_FROM_RE.match(normalize_from(str(frm or "").strip())))


def _decision_token(line: str) -> str:
    s = line.strip().strip("`*_ \t").strip()
    upper = s.upper()
    for prefix in ("DECISION:", "ACTION:"):
        if upper.startswith(prefix):
            upper = upper[len(prefix) :].strip()
    return upper.rstrip(".!:").strip()


def _is_pass_token(tok: str) -> bool:
    if tok in {"PASS", "NO_REPLY", "NOREPLY", "SILENT", "SKIP"}:
        return True
    return tok.startswith("PASS:") or tok.startswith("PASS ") or tok.startswith("NO_REPLY:") or tok.startswith(
        "NO_REPLY "
    )


def _is_speak_token(tok: str) -> bool:
    if tok in {"SPEAK", "REPLY"}:
        return True
    return tok.startswith("SPEAK:") or tok.startswith("SPEAK ") or tok.startswith("REPLY:") or tok.startswith("REPLY ")


def normalize_channel_llm_reply(content: str) -> tuple[str, bool]:
    """Mirror collab.NormalizeChannelLLMReply for E2E checkers.

    Bare prose without a SPEAK/PASS first-line token is treated as PASS (skip).
    """
    text = re.sub(r"(?is)<think>.*?</think>", "", usable_content(content)).strip()
    if not text:
        return "", True
    first_line, _, rest = text.partition("\n")
    tok = _decision_token(first_line)
    if _is_pass_token(tok):
        return "", True
    if _is_speak_token(tok):
        body = rest.strip()
        for prefix in ("SPEAK:", "SPEAK ", "REPLY:", "REPLY "):
            if tok.startswith(prefix.rstrip()) or first_line.upper().lstrip("`*_ ").upper().startswith(prefix):
                raw_first = first_line.strip().strip("`*_ ")
                if raw_first.upper().startswith(prefix):
                    inline = raw_first[len(prefix) :].strip()
                    body = (inline + "\n" + rest).strip()
                break
        return (body, False) if body else ("", True)
    return "", True


def is_no_reply_content(content: str) -> bool:
    _, skip = normalize_channel_llm_reply(content)
    return skip


def check_min_agent_replies(
    data: dict,
    since_index: int,
    marker: str,
    min_count: int,
    min_court: int = 1,
) -> tuple[int, list[str], list[str], bool, list[str]]:
    messages = data.get("messages") or []
    new_msgs = messages[since_index:] if since_index < len(messages) else []

    marker_ok = any(
        isinstance(m, dict)
        and marker.lower() in str(m.get("content") or "").lower()
        and str(m.get("from") or "").lower() in ("user", "operator", "web-portal", "portal", "cli")
        for m in new_msgs
    )

    agents: list[str] = []
    canned: list[str] = []
    for m in new_msgs:
        if not isinstance(m, dict):
            continue
        frm = normalize_from(str(m.get("from") or "").strip())
        if not is_agent_from(frm):
            continue
        content, skip = normalize_channel_llm_reply(m.get("content"))
        if skip:
            continue
        if is_canned_intro(content):
            canned.append(frm)
            continue
        if frm not in agents:
            agents.append(frm)

    court_agents = [a for a in agents if a.startswith("court-persona-")]
    return len(agents), agents, canned, marker_ok, court_agents


def main() -> int:
    if len(sys.argv) < 5:
        print(
            "usage: check_channel_min_agent_replies.py <channel.json> <since_index> <marker> <min_count> [min_court]",
            file=sys.stderr,
        )
        return 2

    path = sys.argv[1]
    since_index = int(sys.argv[2])
    marker = sys.argv[3]
    min_count = max(1, int(sys.argv[4]))
    min_court = max(0, int(sys.argv[5])) if len(sys.argv) > 5 else 1
    data = load_channel(path)

    count, agents, canned, marker_ok, court_agents = check_min_agent_replies(
        data, since_index, marker, min_count, min_court
    )
    if not marker_ok:
        print("MISSING:portal_post_marker")
        return 1
    if canned:
        print("CANNED:" + ",".join(canned))
        return 1
    if count < min_count:
        print(f"INSUFFICIENT:{count}/{min_count} agents={','.join(agents) or 'none'}")
        return 1
    if len(court_agents) < min_court:
        print(
            f"INSUFFICIENT_COURT:{len(court_agents)}/{min_court} "
            f"court={','.join(court_agents) or 'none'} agents={','.join(agents)}"
        )
        return 1
    print(f"OK:{count} court={len(court_agents)} agents={','.join(agents)}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
