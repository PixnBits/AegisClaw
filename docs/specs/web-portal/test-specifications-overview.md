# Test Specifications Overview

**Status**: Target State

## Purpose

This set of documents provides concrete, actionable test specifications for the Web Portal. The goal is to give implementors (human or AI coding agents) clear guidance on what to test, at what level, and with what focus.

The documents are intentionally split into smaller files to stay within reasonable context windows.

## Document Structure

- `test-specifications-overview.md` — This file. Priorities and how to use the specs.
- `test-contracts.md` — Contract tests (STOMP + Bridge actions). **Highest priority layer**.
- `test-unit.md` — Unit test specifications.
- `test-component.md` — Component and small integration tests.
- `test-e2e.md` — End-to-end tests (keep minimal).

## Overall Priorities

| Priority | Layer              | Why it matters for this architecture                  | Recommended Volume |
|----------|--------------------|-------------------------------------------------------|--------------------|
| High     | Contract Tests     | Catches integration issues without full system        | High               |
| High     | Unit Tests         | Fast feedback on logic and sanitization               | High               |
| Medium   | Component Tests    | Validates UI behavior with realistic data             | Medium             |
| Low      | E2E Tests          | Expensive and harder to keep stable                   | Very Low           |

## How Coding Agents Should Use These Specs

1. Start with **Contract Tests** — they give the best signal for the daemon + microVM model.
2. Use the examples as patterns. Do not copy them verbatim if the implementation differs slightly.
3. When in doubt, ask: "Is this testing a contract boundary or pure logic?"
4. Prefer deterministic tests. Avoid relying on timing of agent microVM startup.
5. Always consider what should be mocked vs real at each layer (see `testing-strategy.md`).

## Regression Protection Goal

These specifications are designed so that future changes can be validated quickly and reliably. Contract tests in particular should catch breaking changes to payloads or bridge behavior early.