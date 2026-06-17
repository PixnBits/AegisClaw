# Design Tokens & Component Patterns Specification

**Status**: Target State

## Overview

This document defines the design tokens and reusable component patterns for the AegisClaw Web Portal. It provides the visual and structural foundation needed for consistent, high-quality implementation across all views (Home, Channels, Dashboard, Court, Canvas, etc.).

The portal uses a self-contained, GitHub-inspired dark theme with a strong emphasis on clarity, security posture visibility, and low cognitive load.

## Design Principles

- **Clarity over decoration**: Every visual element should communicate state, priority, or action clearly.
- **Paranoid but calm**: Security indicators should be visible and reassuring without causing alarm.
- **High contrast & accessibility**: Must meet WCAG AA standards at minimum.
- **Consistency**: The same concept (e.g., "active agent", "pending proposal", "narrow task") should look and behave the same across views.
- **Minimal dependencies**: All styling and components must be self-contained (no external CSS frameworks or icon libraries).

## Color Palette (Target)

**Base**
- Background: `#0a0a0a` or `#0d1117`
- Surface / Cards: `#1f1f1f` / `#161b22`
- Elevated surfaces: `#2a2a2a` / `#21262d`

**Text**
- Primary: `#e0e0e0`
- Secondary / Muted: `#8b949e` or `#aaaaaa`
- Inverse (on dark accents): `#0d1117`

**Accent / Semantic**
- Primary / Links / Focus: `#00d4ff` or `#58a6ff`
- Success / Approved / Running: `#22ff88` or `#3fb950`
- Warning / Pending: `#ffcc00` or `#d29922`
- Danger / Rejected / Error: `#ff3333` or `#f85149`
- Info / Neutral: `#58a6ff`

**Borders**
- Default: `#333333` or `#30363d`
- Focus: `#00d4ff`

**Status Indicators**
- Active / Online: `#22ff88`
- Idle: `#ffcc00`
- Error / Blocked: `#ff3333`

These colors should be defined as CSS custom properties (design tokens) for easy theming and consistency.

## Typography

- **Primary**: `system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif`
- **Monospace** (code, logs, traces): `ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", monospace`

**Scale**
- Headings: 1.5rem, 1.25rem, 1.125rem
- Body: 0.875rem – 1rem
- Small / Secondary: 0.75rem
- Monospace in traces/logs: 0.8125rem

Line height should support readability in dense interfaces (activity feeds, traces).

## Spacing Scale

Define a consistent spacing scale (e.g., 4px, 8px, 12px, 16px, 24px, 32px) as CSS custom properties. Use these for padding, margins, and gaps in component layouts.

Recommended base unit: `4px` (so values are multiples of 4).

## Focus & Interactive States

- Focus ring: 2px solid `#00d4ff` with 2px offset
- Hover: Subtle background or border change (e.g., surface lightening by 5-10%)
- Active / Pressed: Further darkening or color shift on primary actions
- Disabled: Reduced opacity + muted colors

All interactive elements must be keyboard-focusable and have visible focus indicators.

## Core Component Patterns

### Cards / Sections
- Background: Elevated surface color
- Border: Subtle border or none (use shadow for elevation)
- Padding: 16px or 24px depending on density
- Header: Bold title + optional actions on the right
- Hover: Optional subtle lift or border highlight for interactive cards

### Badges / Status Pills
- Small, rounded or pill-shaped
- Color-coded by semantic meaning (Success, Warning, Danger, Neutral)
- Consistent padding and font size
- Examples: role badges, proposal status, agent state, security gate results

### Timeline / Trace View
- Vertical list with clear visual hierarchy
- Phase indicators (icons or colored dots)
- Expandable sections for tool calls and details
- Timestamps right-aligned or in a consistent column
- Good visual separation between phases

### Pipeline / Stage Indicators
- Horizontal or vertical representation of stages (Plan → Delegate → Execute → Propose → Court Review → Apply)
- Current stage highlighted
- Completed stages shown with success color
- Progress bars or percentage within active stages
- Clickable to navigate to relevant detail (e.g., proposal)

### Member Chips / Avatars
- Small circular or square avatars for humans
- Text-based or icon-based for agent personas
- Status dot overlay (active/idle)
- Consistent sizing and spacing in grouped lists

### Buttons & Actions
- Primary: Filled with primary accent color
- Secondary: Outlined or subtle background
- Danger: Red-filled or outlined in red
- Small / Compact variants for dense areas (feeds, traces)
- Clear disabled state

### Forms & Inputs
- Consistent border, focus ring, and padding
- Error states with danger color and helper text
- Search inputs with clear icon and placeholder

### Empty States & Guidance
- Centered icon + headline + supporting text
- Optional primary action button
- Consistent tone (helpful, not apologetic)

### Loading States
- Skeleton screens preferred over spinners for content areas
- Inline loading indicators for individual actions
- Graceful degradation when real-time updates are delayed

## Accessibility (a11y)

- Minimum WCAG AA contrast ratios
- All interactive elements keyboard accessible
- Proper ARIA labels and roles for dynamic content (live regions for activity feeds, proposal updates, etc.)
- Focus management when modals or drawers open
- Screen reader friendly structure in timelines and feeds

## Implementation Recommendations

- Define all tokens as CSS custom properties at the root level.
- Create a small set of reusable classes or web components for the core patterns above.
- Keep JavaScript minimal and progressive (enhance existing HTML structure).
- Document any deviations from these patterns in code comments.

This foundation enables consistent, accessible, and maintainable implementation across the entire portal.