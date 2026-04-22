# PR 19 Security Plan: Agent Knowledge + Secrets UX

Status: Proposed implementation plan for PR 19 follow-up hardening.
Date: 2026-04-08
Owner: AegisClaw core

## 1. Objective

This plan addresses two outcomes:

1. The agent reliably produces proposal drafts that pass Court review for network and secret requirements.
2. Secret management exposes a minimal, write-only interface with as few attack surface touchpoints as possible.

This plan aligns with:

- docs/network-secrets-spec.md
- paranoid-by-design principles (default deny, least privilege, auditable control plane)

## 2. Baseline (Current State)

Already present in code:

- Proposal model supports network_policy.allowed_hosts, allowed_ports, allowed_protocols, egress_mode and capabilities.secrets.
- Validation rejects dangerous hosts and unsupported egress_mode values.
- Host-side SNI egress proxy exists and is started for proxy mode skills.
- Vault exists (age encrypted), secret injection into skill VM is wired, and secrets.refresh exists.
- CLI has secrets add/list/rotate/refresh with secure terminal prompt behavior.
- Pre-activation check warns/blocks activation when required secrets are missing.

Remaining concern:

- Secret write path has multiple touchpoints (CLI opens vault directly), which increases tampering and exfiltration risk versus a single daemon-mediated path.

## 3. Threat-Driven Design Rules

Non-negotiable controls:

- No API returns secret plaintext after write.
- Secret values are accepted only through local secure prompt and sent over local Unix socket to daemon.
- The daemon is the only process that can open and mutate vault files.
- Agent/tooling path cannot read vault values.
- All secret mutations are signed and audited.
- Proposal and chat logs never contain secret values.

## 4. Plan A (Preferred): Single Control-Plane Secret Service

### 4.1 Minimal Touchpoint Architecture

Move all vault mutation out of CLI process into daemon handlers.

New daemon API handlers:

- secrets.put
  - Inputs: name, skill, value (opaque bytes)
  - Behavior: create or update atomically
  - Output: metadata only (name, skill, updated_at)
- secrets.list
  - Inputs: optional skill filter
  - Output: metadata only (name, skill, created_at, updated_at)
- secrets.rotate
  - Inputs: name, optional skill, value
  - Behavior: overwrite existing entry or fail if missing (strict mode)
  - Output: metadata only

Explicitly excluded API:

- secrets.get
- any endpoint that returns plaintext values

CLI becomes thin transport:

- Read value through no-echo prompt
- Immediately send to daemon API
- Zero memory buffers after request completion
- Never instantiate Vault in CLI

### 4.2 Authorization and Integrity

- Daemon accepts secret write/list only over local root-owned Unix socket.
- Require kernel signing of every secret mutation event (already used for audit action patterns).
- Add optional confirmation guard for destructive operations (delete, future scope).

### 4.3 Network/Secret Coupling Guards

On skill activate:

- If proposal has secrets_refs but capabilities.secrets is missing or mismatched, fail policy check.
- If network capability true and egress_mode missing, default to proxy and emit explicit audit note.
- If missing secrets, follow configurable mode:
  - warn mode (default): activate degraded, clearly reported
  - strict mode: fail activation

## 5. Agent Knowledge Delivery Plan

Goal: make first-draft proposals correct without user having to supply deep details.

### 5.1 System Prompt Contract

Keep a compact, hard-rule section in daemon system prompt with:

- required proposal network fields
- egress_mode proxy default
- exact FQDN requirement
- forbidden host patterns
- secret_refs and capabilities.secrets mirroring rule
- instruction to tell users to run secrets add before activation
- instruction to never request secret values in chat

### 5.2 Regression Tests for Prompt Quality

Add tests that assert the system prompt contains required policy strings:

- egress_mode proxy guidance
- wildcard and 0.0.0.0/0 rejection
- secret write-only guidance
- secrets add CLI instruction

This prevents accidental prompt drift that would degrade proposal quality.

## 6. UX Plan (Security First, Still Practical)

Command UX (no value readback):

- aegisclaw secrets add NAME --skill SKILL
- aegisclaw secrets update NAME --skill SKILL (alias to rotate for clarity)
- aegisclaw secrets list [--skill SKILL]
- aegisclaw secrets refresh --skill SKILL

UX protections:

- Clear, actionable pre-activation errors showing exact missing names and add command.
- Post-proposal assistant guidance always includes a secret checklist.
- Output wording consistently states values cannot be displayed by design.

## 7. Implementation Sequence

1. Introduce daemon secrets.put/list/rotate handlers and wire audit logging.
2. Refactor CLI secrets commands to use daemon API only.
3. Remove direct vault usage from CLI code path.
4. Add strict schema consistency checks (secrets_refs vs capabilities.secrets).
5. Add prompt regression tests and secret interface tests.
6. Document operator runbook updates.

## 8. Test Matrix

Security tests:

- No API path can return plaintext secret.
- Chat/tool interfaces cannot retrieve secret values.
- Audit events emitted for add/update/rotate/refresh.
- Vault files remain mode 0600, directory 0700.

Behavior tests:

- Missing required secret shows exact add command.
- refresh re-injects updated values to running VM.
- proxy mode skill proposal with FQDN hosts passes validation.
- wildcard and any-address hosts are rejected.

Failure-mode tests:

- daemon unavailable: CLI fails closed with guidance
- vault corruption: safe error, no plaintext leak
- partial secret availability: degraded/strict behavior matches config

## 9. Acceptance Criteria

PR 19 support is complete when:

- Agent-generated networked skill drafts include compliant network_policy + secret fields by default.
- Secrets are managed through a single daemon control plane.
- Operators can add/update/list/refresh secrets with no value readback.
- Court and security reviewers can trace network intent and secret handling in audit artifacts.
