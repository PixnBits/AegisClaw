#!/usr/bin/env python3
"""Validate channel roster intro posts for verify-channel-roster-e2e.sh."""
import json
import os
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


def usable_content(content: str) -> str:
    text = str(content or "")
    if text.strip().startswith("map[") and "channel_id:" in text:
        return ""
    return text


def load_channel(path: str) -> dict:
    try:
        with open(path, encoding="utf-8") as f:
            return json.load(f)
    except (OSError, json.JSONDecodeError):
        return {}


def check_roles(data: dict, roles: list[str]) -> tuple[list[str], dict[str, str]]:
    messages = data.get("messages") or []
    by_from: dict[str, str] = {}
    for m in messages:
        if not isinstance(m, dict):
            continue
        frm = str(m.get("from") or "").strip()
        content = usable_content(m.get("content"))
        if not content.strip():
            continue
        if frm:
            by_from[frm] = by_from.get(frm, "") + "\n" + content

    for key, val in list(by_from.items()):
        if key.startswith("project-manager"):
            by_from["project-manager"] = by_from.get("project-manager", "") + "\n" + val

    missing = []
    for role in roles:
        content = by_from.get(role, "").lower()
        if not content.strip():
            missing.append(role)
            continue
        pat = KEYWORDS.get(role, re.escape(role))
        if not re.search(pat, content, re.I):
            missing.append(role)
    return missing, by_from


def main() -> int:
    if len(sys.argv) < 3:
        print("usage: check_channel_roster.py <channel.json> <role>...", file=sys.stderr)
        return 2

    path = sys.argv[1]
    roles = sys.argv[2:]
    data = load_channel(path)
    missing, by_from = check_roles(data, roles)

    mode = os.environ.get("AEGIS_ROSTER_CHECK_MODE", "summary")
    if mode == "summary":
        if missing:
            print("MISSING:" + ",".join(missing))
            return 1
        print("OK")
        return 0

    exit_code = 0
    for role in roles:
        content = by_from.get(role, "")
        if not content.strip():
            print(f"  ✗ {role} (no post)")
            exit_code = 1
            continue
        pat = KEYWORDS.get(role, re.escape(role))
        if re.search(pat, content, re.I):
            snippet = content.strip().replace("\n", " ")[:120]
            print(f"  ✓ {role}: {snippet}...")
        else:
            print(f"  ✗ {role} (post present but missing expected keywords)")
            exit_code = 1
    return exit_code


if __name__ == "__main__":
    sys.exit(main())
