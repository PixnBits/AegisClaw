# `docs/sdlc-visibility-implementation.md` — Summary

## Purpose

Tracks the implementation progress of enhanced SDLC visibility and control features added to the AegisClaw web portal. Documents specific bugs fixed, template patterns corrected, and security decisions made during the portal enhancement work (as of 2026-04-26).

## Key Contents

### Recent Updates (2026-04-26)

**Fix: Duplicate Navigation and Self Repository Removal**

- **Problem**: New page templates included full HTML structure with navigation, causing duplicate nav bars when inserted into the portal layout. Additionally, the "Self" repository (AegisClaw's own source code) was accessible for editing — a security risk.
- **Resolution**:
  - All new templates converted from full HTML pages to **fragments** (no `<!DOCTYPE html>`, no nav wrapper).
  - "Self" repository support removed; all handlers hardcode `repo="skills"`.
  - Repository selector tabs removed (only one repository is supported).
  - Templates follow the existing fragment pattern (`agentsTmpl`, `asyncTmpl`, etc.).

### Template Pattern (Correct vs. Incorrect)
Shows the before/after of full-page templates vs. fragment templates used in `internal/dashboard`.

## Fit in the Broader System

Supplementary to `internal/dashboard/summary.md`. Documents the specific template fix that prevents duplicate navigation in the web portal. Relevant to any developer adding new dashboard pages — all templates must follow the fragment pattern, not the full-HTML pattern. The "Self" repository restriction is a security decision: operators cannot browse or modify the daemon's own source through the portal.
