# Web Portal Testing Strategy

**Status**: Target State

## Philosophy

The Web Portal is a **presentation-only** component running in an isolated microVM. It has no business logic, no direct access to the Host Daemon's internals, and communicates exclusively through a narrow vsock bridge.

This architecture creates common confusion for both human developers and coding agents. A good test strategy must:

- Make the boundaries extremely clear.
- Favor **contract testing** over full integration where possible.
- Provide reliable, fast feedback loops.
- Be structured so that an AI coding agent (like Grok Build) can understand what it is allowed to mock vs what must be real.

## Adapted Testing Pyramid for AegisClaw

We use a modified testing pyramid that accounts for the daemon + microVM model:

### 1. Unit Tests (Foundation)
- **Scope**: Pure functions, sanitization logic, payload builders, UI component rendering logic (where testable).
- **Location**: `internal/dashboard/..._test.go` and small frontend test utilities.
- **Speed**: Very fast.
- **Quantity**: Highest.

**What to test**:
- Sanitization rules (with concrete examples from `security-boundaries.md`)
- Payload construction helpers
- State machines for complex UI interactions (if any)
- Pure formatting / display logic

**What NOT to test here**:
- Anything that touches the vsock bridge
- Real-time behavior
- Full page rendering (use E2E instead)

### 2. Contract Tests (Critical Layer)
This is the **most important layer** for the Web Portal.

**Purpose**: Verify that the portal correctly produces and consumes the expected contracts with the Host Daemon, without needing a full running system.

**Types**:

#### A. STOMP Contract Tests
- Verify that the portal correctly subscribes to expected topics.
- Verify that it can parse all defined payload shapes (see `real-time-contracts.md`).
- Verify graceful handling of unknown fields (forward compatibility).
- Verify proper cleanup on unsubscription / disconnect.

#### B. Bridge Action Contract Tests
- Define the exact allow-list of actions the portal may call.
- Test that the portal only calls allowed actions.
- Test input/output shapes for each allowed action.
- Use mocks or test doubles for the bridge client.

**Why this layer is critical**:
- Catches most integration issues without the complexity of full E2E.
- Much faster and more reliable than trying to spin up multiple microVMs.
- Gives coding agents a clear boundary: "You can mock the bridge here."

### 3. Component / Integration Tests
- Test individual UI components or small groups of components with realistic data.
- Can use a test harness that provides fake STOMP events or bridge responses.
- Good for testing rendering of complex views (Canvas, traces, activity feeds).

### 4. E2E Tests (Playwright) – The Tip

Use sparingly but strategically.

**High-value E2E tests** (keep this set small and reliable):
- Major user journeys that cross multiple boundaries
- Real-time behavior that is difficult to test at lower levels
- End-to-end governance flows (proposal → Court → decision)

**Recommended E2E scope**:
- Start a task from Home → see plan decomposition → channel created with visible harness state.
- Collaborative work with proactive agent updates appearing in the feed.
- Full Court review flow (vote, see rationales, approve).
- Real-time updates across multiple tabs.
- STOMP disconnect + fallback to SSE.

**What to avoid in E2E**:
- Testing every UI state
- Testing internal sanitization logic (use contract/unit tests)
- Flaky tests that depend on timing of agent microVMs

## Helping Coding Agents Understand the Architecture

Coding agents frequently get confused by the daemon + microVM model. The test strategy should include explicit guidance:

### Clear Boundaries (Document These)

- The **Portal** only renders UI and sends/receives messages over the bridge.
- The **Host Daemon** owns all business logic, agent orchestration, and governance.
- Agent microVMs are separate from the Portal microVM.
- The portal **never** directly controls agents or runs business logic.

### Recommended Test Double Strategy

| Layer                    | What to Mock                  | What Should Be Real          | Notes for Agents                     |
|--------------------------|-------------------------------|------------------------------|--------------------------------------|
| Unit Tests               | Everything external           | Pure logic                   | Very safe to mock                    |
| Contract Tests           | vsock bridge client           | Contract shapes              | Best place to catch integration bugs |
| Component Tests          | STOMP events + bridge         | Rendering logic              | Good for UI behavior                 |
| E2E Tests                | As little as possible         | Full flow (with test setup)  | Use sparingly and keep stable        |

### Naming Conventions (Helpful for Agents)

- `*_contract_test.go` → Contract tests against bridge/STOMP
- `*_component_test.go` → Component rendering tests
- `e2e_*.spec.ts` → Playwright end-to-end tests
- Use clear comments at the top of test files explaining the architecture boundary being tested.

## Recommended Test Organization

```
internal/dashboard/
├── sanitize/
│   └── sanitize_test.go          # Unit tests
├── stomp/
│   ├── client_test.go            # STOMP contract tests
│   └── subscription_manager_test.go
├── bridge/
│   └── client_contract_test.go   # Bridge action contracts
├── ui/
│   └── components/               # Component tests
└── testutil/
    └── fake_bridge.go
    └── fake_stomp.go
```

## Stability & Reliability Goals

- All tests should be deterministic.
- E2E tests should use explicit waits and stable selectors (`data-testid`).
- Avoid timing-dependent tests involving agent microVM spin-up.
- Use test fixtures and factories for realistic but controlled data.

## How This Strategy Helps Grok Build / Coding Agents

- Clear layering tells the agent exactly what level of fidelity is needed.
- Contract tests give fast, reliable feedback without needing a full daemon + multiple VMs.
- Explicit guidance on what to mock reduces hallucinations about the architecture.
- The pyramid structure naturally pushes complex logic down to faster, more reliable test layers.

This strategy is designed to maximize confidence while minimizing flakiness and confusion.