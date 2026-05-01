# `aegishub_test.go` — Integration Tests for the Message Hub

## Purpose
Tests the `MessageHub` (AegisHub) end-to-end: startup/shutdown lifecycle, skill registration/unregistration, message routing with ACL enforcement, identity verification, and hub statistics.

## Key Test Cases

| Test | What It Verifies |
|---|---|
| `TestMessageHub_StartStop` | Hub transitions from `stopped` → `running` → `stopped`; double-start returns error |
| `TestMessageHub_RegisterAndRoute` | Registered skill receives a message routed through the hub |
| `TestMessageHub_ACLEnforcement` | Message from an `agent` role with forbidden type is rejected; permitted type is delivered |
| `TestMessageHub_IdentitySpoofing` | Message with mismatched `From`/sender identity is rejected |
| `TestMessageHub_SelfRouting` | `from == to` returns error |
| `TestMessageHub_UnregisterSkill` | After unregister, routing to that skill ID returns "no route" failure |
| `TestMessageHub_Stats` | Counters for routed/rejected messages increment correctly |
| `TestMessageHub_NoKernelConstructor` | `NewMessageHubNoKernel` works without audit logging |

## Role in the System
Provides confidence that the central IPC router enforces all security invariants before messages reach their destinations.

## Notable Dependencies
- Package under test: `ipc`
- `internal/kernel`
- Standard library
