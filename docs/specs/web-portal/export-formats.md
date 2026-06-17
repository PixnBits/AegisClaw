# Export Formats & Compliance Artifacts Specification

**Status**: Target State

## Overview

This document defines the expected export capabilities from the Court / Governance Hub and related areas.

## Export Use Cases

- Investor / diligence reports (Jordan Hale persona)
- Compliance and regulatory mapping (Dr. Lena Moreau persona)
- Internal audit trails

## Required Export Types

- Structured proposal + decision report (including per-persona rationales and votes)
- SBOM and security gate results for approved changes
- Regulatory mapping exports (where applicable)
- Full audit trail for a proposal or channel

## Format Recommendations

- Primary format: Well-structured Markdown or JSON for machine readability
- Secondary: PDF for human review and sharing
- Include clear metadata (timestamps, proposal ID, originating channel, participants)

## Security & Privacy

- Exports must respect the same sanitization rules as the UI (no secrets or internal details leaked).
- Access to export functionality should be governed by appropriate permissions.

## Implementation Notes

- Exports should be generated on demand from the authoritative data in the Store / Host.
- Consider adding a "Generate Report" action in the Court detail view with format options.

This capability is particularly important for building trust with security-conscious and enterprise users.