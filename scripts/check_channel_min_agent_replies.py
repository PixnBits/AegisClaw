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


def normalize_channel_llm_reply(content: str) -> tuple[str, bool]:
    """Mirror collab.NormalizeChannelLLMReply for E2E checkers."""
    text = usable_content(content).strip()
    if not text:
        return "", True
    if text.upper() == "NO_REPLY":
        return "", True
    first_line = text.split("\n", 1)[0].strip()
    if first_line.upper() == "NO_REPLY":
        return "", True
    while True:
        text = text.strip()
        if "\n" in text:
            head, last_line = text.rsplit("\n", 1)
        else:
            head, last_line = "", text
        if last_line.strip().upper() == "NO_REPLY":
            text = head.strip()
            if not text:
                return "", True
            continue
        break
    return text, False


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
