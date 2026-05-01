# handlers_git.go — cmd/aegisclaw

## Purpose
Implements daemon API handlers for git repository browsing and management: `git.browse`, `git.log`, `git.diff`, and `git.commit`.

## Key Types / Functions
- `makeGitBrowseHandler(env)` — lists files and directories in a repo (`"skills"` or `"self"`). Path-traversal protection: rejects paths outside the repo root.
- `makeGitLogHandler(env)` — returns recent commits for a repo with configurable limit.
- `makeGitDiffHandler(env)` — returns the diff for a specific commit SHA.
- `makeGitCommitHandler(env)` — creates a signed commit via `kernel.SignCommit` after the court reviews the diff.

## System Fit
Gives the agent read/write access to the skills and self repositories under governance control. Commit handler enforces kernel signing so every agent-authored commit is audit-traceable.

## Notable Dependencies
- `github.com/PixnBits/AegisClaw/internal/git` — `gitmanager.Manager`
- `github.com/PixnBits/AegisClaw/internal/kernel`
