# Design Tokens & Component Patterns Specification

**Status**: Target State (Canonical Source)

## Overview

This document is the **single source of truth** for design tokens and component patterns in the AegisClaw Web Portal. All other documents should reference this file for visual and structural consistency.

## Color Palette (Canonical)

**Base**
- Background: `#0a0a0a`
- Surface / Cards: `#1f1f1f`
- Elevated surfaces: `#2a2a2a`

**Text**
- Primary: `#e0e0e0`
- Secondary / Muted: `#8b949e`

**Semantic Colors**
- Primary / Focus / Links: `#00d4ff`
- Success / Running / Approved: `#22ff88`
- Warning / Pending: `#ffcc00`
- Danger / Rejected / Error: `#ff3333`

**Borders**
- Default: `#333333`
- Focus: `#00d4ff`

All colors should be defined as CSS custom properties (e.g., `--color-bg`, `--color-primary`, `--color-success`).

## Typography

- System UI stack for body text
- Monospace stack for code, logs, and traces

## Spacing

Base unit: 4px (multiples of 4 recommended).

## Component Patterns

### Cards
- Elevated surface background
- Consistent padding (16px or 24px)
- Subtle elevation via shadow or border

### Badges & Status
- Semantic color coding as defined above
- Small, readable, consistent across views

### Timeline / Trace
- Clear phase separation
- Expandable tool call details
- Good visual hierarchy

### Pipeline Stages
- Horizontal or vertical representation of Plan → Delegate → Execute → Propose → Court Review → Apply
- Current stage highlighted
- Completed stages in success color

### Member Representation
- Grouped by category (Core Court, Project Roles, Humans)
- Status indicators
- Consistent chip/avatar treatment

## Accessibility

- WCAG AA minimum contrast
- Visible focus states
- Proper ARIA for dynamic content

This document should be referenced by all other specification files when describing visual elements.