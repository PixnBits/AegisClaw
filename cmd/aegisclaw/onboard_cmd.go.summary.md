# onboard_cmd.go — cmd/aegisclaw

## Purpose
Implements the `aegisclaw onboard` command — a 5-step guided wizard for first-time setup that walks users through prerequisites, workspace directory selection, starter file generation, `init`, and next-step guidance.

## Key Types / Functions
- `runOnboard(cmd, args)` — orchestrates the five onboarding steps:
  1. Check prerequisites (KVM, Firecracker, Ollama).
  2. Prompt for workspace directory.
  3. Write starter files: `AGENTS.md`, `SOUL.md`, `TOOLS.md`, `SKILL.md`.
  4. Run `aegisclaw init` internally.
  5. Print next-steps guidance.

## System Fit
Reduces friction for new users. Idempotent — skips steps if files already exist. After completion the user is ready to run `aegisclaw start`.

## Notable Dependencies
- Standard library (`os`, `bufio`, `fmt`) for prompts and file creation.
- `init_cmd.go` — delegates to the same `runInit` logic.
