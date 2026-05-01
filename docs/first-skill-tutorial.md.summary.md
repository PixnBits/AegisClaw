# `docs/first-skill-tutorial.md` — Summary

## Purpose

A comprehensive step-by-step tutorial guiding new users through the complete lifecycle of creating their first AegisClaw skill — from a plain-English description to an activated, running skill. Uses the **time-of-day greeter** as the worked example.

## What It Covers

1. **Prerequisites**: Linux host (x86_64), Go 1.26+, Ollama running with `mistral-nemo` pulled, Firecracker + jailer.
2. **Install AegisClaw**: `git clone`, `go build` for `aegisclaw` and `guest-agent`.
3. **Init and start the daemon**: `aegisclaw init` + `sudo ./aegisclaw start`.
4. **Submit a natural language request** in `aegisclaw chat`:
   > "please add a skill that says hello to the user with a message appropriate for the time of day…"
5. **Proposal creation**: the agent creates a `SkillSpec` and draft proposal.
6. **Governance Court review**: five AI persona reviewers evaluate the proposal inside isolated microVMs; their weighted verdicts determine approval.
7. **Builder pipeline**: approved proposal triggers code generation in a sandboxed builder VM; security gates (SAST, SCA, secrets scanning, policy-as-code) run automatically.
8. **Activation**: composition manifest is updated; the skill is deployed and available.
9. **Invocation**: call the new `greet` tool via chat.
10. **Rollback**: how to revert if needed.

## Fit in the Broader System

The primary onboarding document for new users. Cross-referenced from `README.md`. Corresponds to `TestFirstSkillTutorialLive` in `cmd/aegisclaw/` and the VCR-backed `TestFirstSkillTutorialInProcess`. Illustrates the full system end-to-end: `internal/proposal`, `internal/court`, `internal/builder`, `internal/composition`, `internal/sandbox`.
