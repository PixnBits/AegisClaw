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

Base unit: 4px (multiples of 4 recommended). Generous application for breathing room in command surfaces and feeds.

## Interaction States

- Hover: subtle surface lift or border highlight
- Focus: cyan ring (`--color-focus`) with 2px offset
- Active / Pressed: slight scale or background shift
- Disabled: muted text and reduced opacity
- Loading / Skeleton: calm pulse or static placeholder blocks matching surface colour

## Component Patterns

### Cards
- Elevated surface background
- Consistent padding (16px or 24px)
- Subtle elevation via shadow or border

### Badges & Status
- Semantic color coding as defined above
- Small, readable, consistent across views
- Compact variants for Activity Summary chips

### Timeline / Trace
- Clear phase separation
- Expandable tool call details
- Good visual hierarchy
- Collapsible by default for completed sections in feeds

### Pipeline Stages (Compact Harness)
- Horizontal or vertical representation of Plan → Delegate → Execute → Propose → Court Review → Apply
- Current stage highlighted with semantic colour
- Completed stages in success colour
- Compact version for Channels and Home headers

### Member Representation
- Grouped by category (Core Court, Project Roles, Humans) with collapsible sections
- Status indicators and consistent chip/avatar treatment
- Searchable within groups

### Collapsible Sections
- Clear affordance (chevron or "Show/Hide" text)
- Smooth height transition
- Default collapsed for reasoning steps (post-decision), member groups, and long traces
- Live/in-flight sections start expanded
- Persist user preference where appropriate (e.g. per-channel reasoning collapse state)

### Bottom Sheets (Mobile Context)
- Slide-up from bottom with backdrop
- Header with title and close affordance
- Scrollable content area
- Safe-area padding for device notches
- Used for right-context content (member management, harness details, operator controls) on mobile
- Dismiss on backdrop tap or swipe down

### Agent Activity Summary
- Lightweight horizontal chip row or small card group
- Shows: active narrow tasks/personas, overall stage progress, token usage (when exposed)
- Semantic colour for status
- Clickable to open deeper view (Dashboard or Canvas)
- Appears in Home header (desktop), Channels header area (desktop + mobile), and Dashboard
- Graceful empty state when no active work

### Policy / Preset Toggles
- Clear segmented control or dropdown for Progressive / Paranoid / Velocity presets
- Visual indicator of current policy
- Per-channel override option with clear inheritance note
- Enterprise lock state shown with admin-only messaging
- Placed in Settings, Channel header menu, or operator context panel

### Pipeline Stages (Expanded / Canvas)
- Richer visual representation when opened from Compact Harness strip
- Supports grid/kanban or lightweight flow view of parallel tasks
- Direct links to traces and proposals

## Accessibility

- WCAG AA minimum contrast
- Visible focus states (cyan ring)
- Proper ARIA for dynamic content (expanded/collapsed states, live regions for activity feed and Agent Activity Summary)
- Minimum 44px touch targets on mobile
- Keyboard navigation support for all interactive elements including bottom sheets and policy toggles

This document should be referenced by all other specification files when describing visual elements. New components (Agent Activity Summary, bottom sheets, policy toggles) must follow the patterns defined here.