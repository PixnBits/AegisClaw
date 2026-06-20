# Contract Test Specifications

**Status**: Target State

## Overview

Contract tests are the **highest-value** layer for the Web Portal. They verify that the portal correctly produces and consumes the defined contracts with the Host Daemon without requiring a full running system.

## STOMP Contract Tests

### Test: Subscribes to correct topics on view mount
- When a user opens the Channels view, the portal should subscribe to:
  - `/topic/channel.{id}.activity`
  - `/topic/harness.{plan_id}.updates` (if a plan exists)
- Verify it does **not** subscribe to unrelated topics.

### Test: Parses known payload shapes
- Send valid payloads for all defined event types.
- Verify the UI updates correctly (activity feed, pipeline progress, etc.).

### Test: Handles unknown fields gracefully
- Send a payload with extra unknown fields.
- Portal should ignore them without error.

### Test: Unsubscribes on view navigation / unmount
- When navigating away from a channel, verify unsubscription.
- Verify no memory leaks in subscription manager.

### Test: Falls back to SSE when STOMP unavailable
- Simulate STOMP connection failure.
- Verify the portal switches to SSE `/events` and still receives updates.

## Bridge Action Contract Tests

### Test: Only calls allowed actions
- Create a test double for the bridge client.
- Perform various UI actions.
- Assert that only actions in the allow-list are called.

### Test: Correct input shapes for allowed actions
- For each allowed bridge action, verify the portal sends correctly shaped requests.

### Test: Handles error responses from bridge
- Simulate bridge returning errors (e.g., permission denied, not found).
- Verify the portal shows appropriate user-facing error (no internal details leaked).

### Test: High-impact actions require confirmation
- Actions like "Approve proposal" or "Cancel agent" should not be sent to the bridge until user confirmation.

## Recommended Structure

```
internal/dashboard/
├── bridge/
│   └── client_contract_test.go
├── stomp/
│   └── client_contract_test.go
└── subscription_manager_contract_test.go
```

Write these tests so they can run quickly in CI without starting microVMs.