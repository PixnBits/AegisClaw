# Security Boundaries & Sanitization Specification

**Status**: Target State

## Overview

Defines security boundaries, input validation, output sanitization, and rate limiting for the Web Portal.

## Core Principles

- Portal is strictly presentation-only.
- All input from browser is untrusted.
- All output to browser must be sanitized.
- High-impact actions require confirmation + rate limiting.

## Input Validation

Multi-layer:
1. HTTP layer (method, size, content-type)
2. STOMP frame validation (allow-list, size)
3. Application-level schema + authorization (via bridge)

## Output Sanitization Rules (Concrete Examples)

### In Single-Agent Traces
- **Redact**: Raw credentials, API keys, internal IPs/hostnames, file paths outside user scope, large binary blobs, stack traces with internal paths.
- **Allow**: Tool names, high-level descriptions, sanitized inputs/outputs (e.g., "search query: Zig security model", result summary).
- **Example**: A tool call that read `/etc/passwd` would show `tool: read_file, path: [REDACTED], result: [REDACTED]`.

### In Chat / Activity Feed
- Never render raw HTML from agents.
- Redact any content that looks like secrets or internal system details.
- Use strict Markdown sanitizer (headings, lists, code, links with allow-list).

### In Proposals & Court Views
- Diffs are passed through existing git/workspace sanitization.
- Rationales and comments are shown in full unless they contain sensitive material.

## Rate Limiting

- Per-connection limits on frames/requests.
- Tighter limits on create proposal, approve/reject, pause/cancel agent.
- Clear backoff behavior on limit hits.

## Bridge Action Allow-list

Portal may only call a restricted set of actions. High-risk actions must go through Court or confirmation flows.

## Threat Mitigation

- XSS: Strict Markdown + output sanitization.
- Information Disclosure: Least privilege + aggressive redaction.
- DoS: Rate limiting + connection cleanup.
- Privilege Escalation: All actions re-validated on Host + Court where required.

## Implementation Recommendations

- Central `sanitize` package with context-aware rules (trace vs chat vs proposal).
- Log security events (validation failures, rate limit hits) on the Host for auditability.