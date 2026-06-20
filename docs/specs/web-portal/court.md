# Court / Governance Hub Specification

**Status**: Target State

## Overview

The Court is the dedicated governance surface where adversarial multi-persona review happens. It makes the structured validation pipeline (proposals → security gates → per-persona review and voting → decisions) transparent, actionable, and exportable.

It embodies the Cloudflare-inspired principle of deliberate disagreement and structured validation, turning what could be opaque agent behavior into auditable, explainable governance.

## Goals

- Provide clear visibility into pending and recent proposals with status, votes, and rationales.
- Enable efficient review and decision-making (approve, reject, defer, comment).
- Support export of structured artifacts for compliance, diligence, and audit needs.
- Make the adversarial nature of the Court (multiple personas in deliberate review) visible and trustworthy.
- Link seamlessly back to originating Channels and forward to implementation artifacts.

## Layout

- Top: Filters (Status, Channel, Persona, Date range, Urgency, Search).
- Main area: Proposal list (cards or table) + detail pane or modal.
- Optional right panel: Summary stats, quick filters, and export actions.

### Proposal List

Each proposal shows:
- Title and short description
- Originating channel (link)
- Current status (Pending, Approved, Rejected, etc.)
- Vote summary (e.g., "7/7 Approved" or "4/7 – 2 needed")
- Security gate status (green checks or warnings for SAST, SCA, secrets, etc.)
- Age and last activity

Clicking a proposal opens the detail view.

### Detail View

Shows:
- Full metadata and structured pipeline stages reached.
- Per-persona vote cards with short rationales, timestamps, and verdict badges.
- Diffs or links to changed artifacts.
- Full security scan results.
- Comments and discussion thread.

Actions available:
- Approve / Reject / Defer (with optional note)
- Comment
- Batch actions when multiple proposals are selected
- Export (structured report, SBOM mapping, regulatory export)

## Real-Time Behavior

Live updates when:
- New proposals appear
- Votes are cast
- Status changes occur

STOMP topics keep the list and detail view current without full page reloads.

## Edge Cases

- No pending proposals: Calm state with link to recent history and overall governance health.
- High volume: Excellent filters, search, and prioritization (e.g., oldest first or highest risk).
- Export needs: One-click structured exports tailored for Jordan (investor diligence) and Lena (compliance) use cases.

## Persona Considerations

- **Alex Rivera**: Plain-English rationales, easy deferral, clear path to traces.
- **Jordan Hale**: Exportable audit trails and "simulate stricter review" context.
- **Dr. Lena Moreau**: Policy alignment, structured exports, and fleet-level visibility.
- All personas benefit from visible adversarial review (deliberate disagreement across personas).

## Open Areas

- Exact export formats and compliance mapping.
- Batch decision UX patterns.
- How proposal priority/urgency is calculated and displayed.

This specification defines the target Court experience.