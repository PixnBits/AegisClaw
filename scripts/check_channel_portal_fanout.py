#!/usr/bin/env python3
"""Validate agent replies after a portal/API channel post (since message index).

Rejects canned FallbackIntro-style replies so E2E reflects real LLM/agent output.
"""
import json
import re
import sys

# Legacy canned intros from collab.FallbackIntro (must not appear after agent changes).
CANNED_INTRO_RE = re.compile(
    r"^I'm the .+\. I (evaluate|assess|review|coordinate|consider|participate)",
    re.I,
)


def usable_content(content: str) -> str:
    text = str(content or "")
    if text.strip().startswith("map[") and "channel_id:" in text:
        return ""
    return text


def is_canned_intro(content: str) -> bool:
    return bool(CANNED_INTRO_RE.match(usable_content(content).strip()))


def load_channel(path: str) -> dict:
    try:
        with open(path, encoding="utf-8") as f:
            return json.load(f)
    except (OSError, json.JSONDecodeError):
        return {}


def normalize_from(frm: str) -> str:
    if frm.startswith("project-manager"):
        return "project-manager"
    return frm


def check_new_replies(data: dict, since_index: int, roles: list[str], marker: str) -> tuple[list[str], list[str], bool]:
    messages = data.get("messages") or []
    new_msgs = messages[since_index:] if since_index < len(messages) else []

    marker_ok = any(
        isinstance(m, dict)
        and marker.lower() in str(m.get("content") or "").lower()
        and str(m.get("from") or "").lower() in ("user", "operator", "web-portal", "portal", "cli")
        for m in new_msgs
    )

    by_from: dict[str, str] = {}
    for m in new_msgs:
        if not isinstance(m, dict):
            continue
        frm = normalize_from(str(m.get("from") or "").strip())
        content = usable_content(m.get("content"))
        if not content.strip():
            continue
        if frm:
            by_from[frm] = by_from.get(frm, "") + "\n" + content

    missing = []
    canned = []
    for role in roles:
        content = by_from.get(role, "").strip()
        if not content:
            missing.append(role)
            continue
        if is_canned_intro(content):
            canned.append(role)

    return missing, canned, marker_ok


def main() -> int:
    if len(sys.argv) < 4:
        print(
            "usage: check_channel_portal_fanout.py <channel.json> <since_index> <marker> <role>...",
            file=sys.stderr,
        )
        return 2

    path = sys.argv[1]
    since_index = int(sys.argv[2])
    marker = sys.argv[3]
    roles = sys.argv[4:]
    data = load_channel(path)

    missing, canned, marker_ok = check_new_replies(data, since_index, roles, marker)
    if not marker_ok:
        print("MISSING:portal_post_marker")
        return 1
    if canned:
        print("CANNED:" + ",".join(canned))
        return 1
    if missing:
        print("MISSING:" + ",".join(missing))
        return 1
    print("OK")
    return 0


if __name__ == "__main__":
    sys.exit(main())
