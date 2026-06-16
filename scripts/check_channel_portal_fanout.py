#!/usr/bin/env python3
"""Validate agent replies after a portal/API channel post (since message index)."""
import json
import re
import sys

KEYWORDS = {
    "project-manager": r"project manager|coordinate|plan",
    "court-persona-ciso": r"ciso|security officer|chief information security",
    "court-persona-security-architect": r"security architect|attack surface",
    "court-persona-architect": r"system architect|modularity|design",
    "court-persona-senior-coder": r"senior coder|code quality|implementation",
    "court-persona-tester": r"tester|testing strategy|coverage",
    "court-persona-efficiency": r"efficiency|performance|resource",
    "court-persona-user-advocate": r"user advocate|usability|accessibility",
}


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


def check_new_replies(data: dict, since_index: int, roles: list[str], marker: str) -> tuple[list[str], bool]:
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
        content = str(m.get("content") or "")
        if frm:
            by_from[frm] = by_from.get(frm, "") + "\n" + content

    missing = []
    for role in roles:
        content = by_from.get(role, "").lower()
        if not content.strip():
            missing.append(role)
            continue
        pat = KEYWORDS.get(role, re.escape(role))
        if not re.search(pat, content, re.I):
            missing.append(role)

    return missing, marker_ok


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

    missing, marker_ok = check_new_replies(data, since_index, roles, marker)
    if not marker_ok:
        print("MISSING:portal_post_marker")
        return 1
    if missing:
        print("MISSING:" + ",".join(missing))
        return 1
    print("OK")
    return 0


if __name__ == "__main__":
    sys.exit(main())
