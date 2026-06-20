# Onboarding, Suggestions Engine & Empty States Specification

**Status**: Target State

## Overview

This document defines the behavior for first-time user guidance, contextual suggestions on Home, and standardized empty states across the portal.

## First-Time User Experience

When a user has no previous activity:
- Home should emphasize Quick Start options (Research a topic, Start a feature channel, Audit security posture, Propose custom skill).
- Guidance should be helpful and non-intrusive.
- The command bar should still be prominent so power users are not slowed down.

## Contextual Suggestions

Suggestions on Home should be driven by:
- Recent activity in the user’s channels (e.g., "Review Court decision on discord_monitor").
- Current background tasks or pending proposals.
- Opted-in external signals (e.g., relevant news or releases related to active topics), with clear privacy controls.

Suggestions must be dismissible and should not block the primary command bar.

## Empty States

Every major view should have a consistent, helpful empty state pattern:
- Relevant icon or illustration
- Clear headline
- Supportive description
- Optional primary action (e.g., "Start a new task" linking back to Home command bar)

Tone should be encouraging rather than apologetic.

## Implementation Notes

- Detection of "first-time" vs "returning with activity" should be simple and reliable (based on presence of channels, recent events, or stored preferences).
- Suggestion generation logic should be lightweight and respect user privacy settings.
- Empty state content should be easy to update without code changes where possible.