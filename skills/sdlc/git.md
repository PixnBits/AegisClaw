# GitSkill Prompt

You are generating **GitSkill** — a secure, local-only version control skill for tracking generated skills in SeedClaw.

## Purpose
- Commit generated skill artifacts (source code, prompt template, binary hash, metadata) to a local git repo
- Support basic ops: init repo, commit, log, diff, branch, checkout/rollback
- Bulk commit pre-git archived skills from MemoryReflectionSkill on first use
- Enable SDLC: track changes, allow CriticSkill to review diffs before commit

## Strict Rules
1. Use only pure-Go git lib: github.com/go-git/go-git (no os/exec("git")).
2. Repo path from env var GIT_REPO_DIR (default /tmp/seedclaw-skills-repo); init if missing.
3. Input via stdin JSON messages: type "commit" with payload {skill_name, source_code, prompt_template, binary_hash, metadata: {prompt_used, llm_model, timestamp, dependencies}}
4. For bulk: on startup, send "retrieve" to MemoryReflectionSkill (category: "generated_skill", all) → receive batch → commit each as separate file (e.g., skills/{name}.go, {name}.prompt.md, {name}.meta.json)
5. Commit message: "Generated {skill_name} via {llm_model} - {metadata summary}"
6. No remote pushes — local-only forever.
7. Register on startup with {"type":"register","payload":{"name":"GitSkill"}}
8. Sandbox invariants: readonly mounts except for repo dir volume; no network.
9. After first bulk commit, send "event" to SelfModSkill: "git_repo_initialized" for potential evolutions.

## Output JSON Format

Respond **only** with:

```json
{
  "skill_name": "GitSkill",
  "description": "Local git version control for generated skills; bulk commits from memory",
  "prompt_template": "You are GitSkill. Commit skills safely, track changes, enable rollbacks.",
  "go_package": "main",
  "source_code": "... complete Go code including main + _test.go ...",
  "binary_name": "gitskill",
  "build_flags": ["-trimpath", "-ldflags=-s -w"],
  "tests_included": true,
  "test_command": "go test -v ./..."
}
```

Generate full implementation: use go-git for ops, listen for messages, handle bulk from memory on init + tests (mock commits, diff, rollback).