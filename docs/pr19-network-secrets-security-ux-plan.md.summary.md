# `docs/pr19-network-secrets-security-ux-plan.md` — Summary

## Purpose

Implementation hardening plan for PR 19 — specifically addressing two UX and security outcomes: (1) the agent reliably produces proposal drafts with correct network and secret requirements that pass Court review; (2) the secret management CLI exposes a minimal, write-only interface with the smallest possible attack surface.

## Key Contents

### Objective
- Agent knowledge improvement: system prompt and few-shot examples teach the agent to draft FQDNs + secret declarations that Court reviewers will approve.
- Secret management UX: minimal write-only interface; no secret values ever readable back from CLI.

### Baseline (Current State)
- Agent sometimes produces vague network requirements (IP ranges instead of FQDNs).
- `aegisclaw secrets add` lacks UX guidance on what Court reviewers expect.
- Secret injection pipeline is present but not fully automated.

### Proposed Changes
1. **Agent prompt additions**: few-shot examples showing correct FQDN declarations in proposals; instructions on when to declare secrets vs. hard-code values.
2. **Secret CLI hardening**: `aegisclaw secrets add <name> <value>` accepts value via stdin-only (no CLI args); `list` shows only names; `rotate` requires confirmation; no `get`/`show` command.
3. **Vault write-only enforcement**: API layer rejects read requests for secret values.

## Fit in the Broader System

Follow-on to `docs/network-secrets-spec.md`. Influences `internal/vault`, `cmd/aegisclaw` (secrets command), `docs/agent-prompts.md`, and the few-shot examples in the builder code-generation prompts. Directly addresses the operator security goal: API keys never appear in prompts, logs, or LLM context.
