# workspace.go

## Purpose
Loads optional prompt-injection Markdown files from the user's `~/.aegisclaw/workspace/` directory to customise agent behaviour. Each file (AGENTS.md, SOUL.md, TOOLS.md, SKILL.md) is silently ignored if absent, making workspace customisation entirely opt-in. Files are capped at 16 KiB to prevent prompt-stuffing attacks where a large workspace file could overwhelm the agent's context window. This design is inspired by OpenClaw's workspace model.

## Key Types and Functions
- `Content`: struct with four string fields — `Agents`, `Soul`, `Tools`, `Skill` (empty string when the corresponding file is absent)
- `IsEmpty() bool`: returns true when all four fields are empty strings; useful for skipping workspace injection when no customisation is present
- `Load(dir string) (*Content, error)`: reads AGENTS.md, SOUL.md, TOOLS.md, and SKILL.md from `dir`; silently ignores missing files; returns an error only if a file exceeds the 16 KiB cap or if `dir` exists but is not a directory
- `readCapped(path string) (string, error)`: reads at most `maxFileBytes = 16384` bytes; returns a trimmed string; returns an error if the file is larger, protecting against prompt-stuffing

## Role in the System
Called during daemon startup and by the `aegisclaw chat` command to load per-workspace agent customisations. The loaded `Content` is injected into the system prompt of the ReAct agent, allowing users to define custom agent personas, tool descriptions, and skill context without modifying the daemon binary.

## Dependencies
- `os`: file existence and directory checks
- `path/filepath`: path joining
- `strings`: whitespace trimming
