# Security Boundaries & Sanitization Specification

**Status**: Target State

## Overview

This document defines the security boundaries, input validation, output sanitization, and rate-limiting requirements for the Web Portal. It is written from the perspective of maintaining AegisClaw’s paranoid security model while enabling a rich, real-time user experience.

The core principle is simple: **the portal is untrusted from the browser’s perspective and has no privileges**. All actions and data are mediated through the vsock bridge under strict controls.

## Core Security Principles

- Presentation-only: The portal never executes business logic, stores secrets, or makes direct external calls.
- All mutations route through the Host Daemon + Court where required.
- Input from the browser is always treated as untrusted.
- Output to the browser must be sanitized to prevent leakage of internal state or secrets.
- The attack surface must remain minimal and auditable.

## Input Validation Rules

Every action originating from the browser must be validated at multiple layers:

1. **HTTP Layer** — Method, path, content-type, size limits.
2. **STOMP Frame Layer** — Command allow-list, frame size, header validation.
3. **Application Layer** — Schema validation, authorization checks, business rule validation (delegated to bridge where possible).

High-risk actions (proposals, approvals, agent control, member management) require additional confirmation flows and are rate-limited more aggressively.

## Output Sanitization Rules

All data sent to the browser must be sanitized:

- **Tool I/O in traces**: Inputs and outputs are sanitized. Secrets, credentials, internal IPs, file paths outside allowed scopes, and large binary blobs are stripped or redacted.
- **Memory context**: Only non-sensitive, user-relevant excerpts are shown.
- **Agent thoughts / plans**: May be shown in full if they do not contain sensitive material; otherwise summarized.
- **Proposal diffs and artifacts**: Must pass through existing sanitization used by the git/workspace layers.
- **Error messages**: Must not leak internal paths, stack traces, or implementation details.

A centralized sanitization package (`internal/dashboard/sanitize`) should be used consistently across chat rendering, trace views, and activity feeds.

## Rate Limiting & Abuse Protection

- Per-connection rate limits on STOMP frames and HTTP requests.
- Tighter limits on high-impact actions (create proposal, approve/reject, pause/cancel agent).
- Burst and sustained limits with clear backoff behavior.
- Monitoring and alerting on anomalous patterns (implemented on the Host side).

Rate limiting must not degrade the experience for legitimate users under normal load.

## STOMP-Specific Security

- Only allow-listed topics may be subscribed to.
- Subscription requests are validated against the current user’s permissions and channel membership.
- `MESSAGE` frames are never echoed back to the sender in a way that could be used for amplification attacks.
- Heartbeat and connection timeouts are enforced to prevent resource exhaustion.

## Bridge Action Allow-list

The portal may only call a restricted set of bridge actions. This allow-list is maintained in code (e.g., `isTrustedPortalBridgeAction`) and must be kept minimal.

Examples of allowed categories:
- Read-only queries (stats, lists, status)
- Chat message sending (routed through PM)
- Approval decisions (routed through Court)
- Limited control actions (pause/resume/cancel) with confirmation

Actions that create persistent state or high-risk changes must go through the proper governance path.

## Threat Mitigation

- **Injection / XSS**: All Markdown rendering uses a strict, audited sanitizer. No raw HTML from agents or users is rendered.
- **Information Disclosure**: Strict output sanitization + principle of least privilege on what data is even fetched for a view.
- **Denial of Service**: Rate limiting, connection limits, frame size limits, and subscription cleanup.
- **Privilege Escalation**: All actions are re-validated on the Host side; the portal cannot bypass Court or permission checks.
- **Session Fixation / Hijacking**: WebSocket connections are tied to authenticated sessions with proper origin and token validation.

## Implementation Recommendations (Go)

- Centralize sanitization logic in a single, well-tested package.
- Use context-aware sanitizers (different rules for traces vs. chat vs. proposals).
- Make rate limiting middleware configurable but always enabled in production.
- Log security-relevant events (validation failures, rate limit hits) at the Host level for auditability.
- Keep the STOMP and HTTP handlers as small as possible; push complex logic to the bridge where it is already audited.

## Open Areas

- Exact redaction rules and patterns for different data types.
- How to handle very large tool outputs or memory contexts gracefully.
- Metrics and alerting thresholds for security events.

This specification ensures the Web Portal can deliver a rich experience without compromising the paranoid security posture of AegisClaw.