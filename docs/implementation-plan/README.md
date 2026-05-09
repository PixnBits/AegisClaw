# AegisClaw v2 Implementation Plan

## Context
This is the detailed, sequential implementation plan for AegisClaw v2 (the lessons-learned revision).

The plan is based on:
- All specifications in `docs/specs/`
- User journeys in `docs/specs/user-journeys/`
- Threat model and security requirements
- Lessons learned from v1

## Principles
- Small, focused steps optimized for Grok Code Fast and OpenCode
- Testing from day one
- Paranoid security at every layer
- Incremental validation of user journeys

## How to Follow This Plan
1. Work through the steps **in strict numerical order**
2. Complete each step fully (including tests) before moving to the next
3. Use small, targeted prompts with Grok Code Fast
4. After each step, run all relevant tests
5. Update this plan if new steps or adjustments are needed

## Directory Structure
- `01-` to `99-`: Individual implementation steps
- Each file contains: Goal, Acceptance Criteria, Test Requirements, and references to relevant specs

Start with `01-initial-project-setup.md`

Last updated: May 09, 2026