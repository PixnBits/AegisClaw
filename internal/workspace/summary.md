# Package: workspace

## Overview
The `workspace` package loads optional Markdown customisation files from the user's `~/.aegisclaw/workspace/` directory and exposes them as a `Content` struct for injection into the agent's system prompt. The four customisation files — AGENTS.md, SOUL.md, TOOLS.md, and SKILL.md — are all optional; missing files are silently ignored. A 16 KiB per-file cap prevents prompt-stuffing attacks. The design is inspired by OpenClaw's workspace model.

## Files
- `workspace.go`: `Content`, `IsEmpty`, `Load`, `readCapped` — workspace file loading with size enforcement
- `workspace_test.go`: Tests for empty dir, missing dir, all files present, partial files, oversized file rejection, and non-directory path

## Key Abstractions
- `Content`: the output of `Load`; four string fields corresponding to the four workspace files; empty string when a file is absent
- `Load(dir)`: the single public entry point; silently skips missing files; errors only on oversized files or non-directory paths
- `readCapped`: enforces the 16 KiB limit using `io.LimitReader`; returns a size error if the file is larger — never truncates silently
- `IsEmpty()`: allows callers to skip workspace injection when no customisation is present, avoiding unnecessary empty strings in the system prompt

## System Role
Called during `aegisclaw chat` and daemon startup to load per-user workspace customisations. The `Content` fields are concatenated into the agent's system prompt by the orchestration layer, enabling users to define custom agent personas (SOUL.md), additional tool descriptions (TOOLS.md), per-skill context (SKILL.md), and multi-agent instructions (AGENTS.md) without modifying the binary.

## Dependencies
- `os`: file reads and directory checks
- `path/filepath`: path construction
- `io`: `LimitReader` for size enforcement
- `strings`: content trimming
