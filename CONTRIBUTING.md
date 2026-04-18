# Contributing to AegisClaw

Thank you for contributing to AegisClaw! This document covers the development
workflow, testing strategy, and — most importantly — the security rules that
protect the Firecracker isolation model.

---

## Table of Contents

1. [Development Setup](#development-setup)
2. [Running Tests](#running-tests)
   - [Unit & Integration Tests](#unit--integration-tests)
   - [Journey Tests](#journey-tests)
   - [Golden Trace Tests](#golden-trace-tests)
   - [Fuzz / Property Tests](#fuzz--property-tests)
   - [In-Process Integration Tests ⚠️](#in-process-integration-tests)
3. [Security Model & Build Tag Rules](#security-model--build-tag-rules)
4. [Submitting Changes](#submitting-changes)

---

## Development Setup

```bash
git clone https://github.com/PixnBits/AegisClaw.git
cd AegisClaw
go build ./...   # verify everything compiles
go test ./...    # run the full test suite
```

You do **not** need Firecracker, KVM, or Ollama for the standard test suite.

---

## Running Tests

### Unit & Integration Tests

```bash
# Run everything (no KVM/Ollama required):
go test ./...

# Or via Makefile:
make test

# Fast mode — skips -short-flagged journey tests:
make test-short
```

### Journey Tests

Journey tests in `cmd/aegisclaw/react_journey_test.go` cover the full agent
lifecycle end-to-end:

- Simple single-tool use
- Multi-step tasks (create → list → get → submit)
- Explicit task completion with no tool call
- Unknown tool handling
- Tool failure and error recovery
- Wrong namespace auto-correction
- Duplicate submit idempotency
- Multiple proposal isolation
- Exposition JSON not parsed as a tool call
- Update-then-submit lifecycle
- Proposal ID prefix resolution
- Portal event emission (ToolEvents + ThoughtEvents)

These tests run as part of `go test ./cmd/aegisclaw/` without any special flags.
Skip heavy scenarios with `-short`:

```bash
go test ./cmd/aegisclaw/ -short
```

### Golden Trace Tests

Golden trace tests in `cmd/aegisclaw/react_journey_test.go` capture the full
ReAct trace (LLM thoughts, tool calls + args, observations, final answer) as a
JSON snapshot and compare future runs against it.

**First run / update snapshots:**

```bash
UPDATE_SNAPSHOTS=1 go test ./cmd/aegisclaw/ -run TestGolden -v
```

**Normal run (compare against golden):**

```bash
go test ./cmd/aegisclaw/ -run TestGolden -v
```

Golden files live in `cmd/aegisclaw/testdata/golden/*.json`. Commit them
alongside code changes. When LLM-generated text changes slightly but tool call
sequences stay the same, update snapshots rather than loosening the comparison
thresholds.

The comparison uses:
- **Exact match** on deterministic fields: tool names, args, event types, trace IDs.
- **Fuzzy match** (≥ 90% token overlap) on LLM-generated text to tolerate minor
  phrasing differences.

---

### Fuzz / Property Tests

Fuzz tests in `cmd/aegisclaw/fuzz_test.go` provide property-based coverage of
the tool lookup and ReAct termination logic without requiring any external
services (Issue #24).

**Run the seed corpus (fast, part of `go test ./...`):**

```bash
go test ./cmd/aegisclaw/ -run 'Fuzz' -v
```

**Run in continuous fuzz mode (for local security auditing):**

```bash
go test ./cmd/aegisclaw -fuzz=FuzzParseSkillToolName  -fuzztime=60s
go test ./cmd/aegisclaw -fuzz=FuzzToolRegistryExecute -fuzztime=60s
go test ./cmd/aegisclaw -fuzz=FuzzReActTermination     -fuzztime=60s
```

The three fuzz targets are:

| Target | What it covers |
|---|---|
| `FuzzParseSkillToolName` | Invariants on skill/tool name parsing: symmetric return, reserved prefixes, UTF-8 preservation |
| `FuzzToolRegistryExecute` | `ToolRegistry.Execute` never panics for arbitrary tool names + args JSON, including nil-env guard |
| `FuzzReActTermination` | `reactMaxIterations` cap is always honoured; constant is sane |

Fuzz-found bugs are fixed in the production code before merging. Failing inputs
are committed as seed corpus entries in `testdata/fuzz/`.

---

### In-Process Integration Tests

> ### ⚠️ SECURITY WARNING — READ THIS BEFORE USING
>
> The in-process executor (`internal/runtime/exec/inprocess_executor.go`) runs
> agent logic **directly in the test process** with **ZERO Firecracker isolation**.
> There is no microVM, no jailer, and no capability dropping.
>
> **This mode exists ONLY for fast integration testing during development.**
>
> It is compiled ONLY when the `inprocesstest` build tag is present. It can
> never appear in a production binary or release build.
>
> If you encounter this executor outside of a test run, treat it as a security
> incident and report it immediately.

#### How to run in-process tests

```bash
# Via Makefile (recommended):
make test-inprocess

# Manually:
AEGISCLAW_INPROCESS_TEST_MODE=unsafe_for_testing_only \
  go test ./cmd/aegisclaw \
    -tags=inprocesstest \
    -run 'Integration|Journey|InProcess' \
    -count=1 \
    -v
```

#### Two safety guards (both required)

1. **Build tag**: `//go:build inprocesstest` — excluded from all normal builds.
2. **Runtime env var**: `AEGISCLAW_INPROCESS_TEST_MODE=unsafe_for_testing_only`
   — `NewInProcessExecutor` panics immediately if this is not set.

#### What the in-process executor does

`InProcessTaskExecutor` (in `internal/runtime/exec/inprocess_executor.go`)
implements the `TaskExecutor` interface by calling a configurable `AgentFunc`
directly in the current process, bypassing all Firecracker VM machinery.

In tests, `AgentFunc` is typically a deterministic stub that returns
pre-scripted responses, enabling fast, reproducible integration tests without
a live Ollama or KVM environment.

#### What the in-process executor does NOT do

- It does **not** call Ollama (unless the `AgentFunc` explicitly does so).
- It does **not** spin up Firecracker or any VM.
- It does **not** enforce any security policy, capability restriction, or
  network isolation.

#### CI gating

In-process tests are **opt-in only**:

- They are excluded from the standard `go test ./...` run (no build tag).
- They can be enabled via `workflow_dispatch` or a specific PR label.
- They are **never** part of the default release CI matrix.

---

## Security Model & Build Tag Rules

AegisClaw enforces a strict isolation model:

> All agent logic, skill execution, and court reviews run exclusively inside
> Firecracker microVMs. There is no in-process fallback in production.

To preserve this guarantee:

| Rule | Enforcement |
|------|-------------|
| `inprocess_executor.go` is compiled only with `-tags=inprocesstest` | `//go:build inprocesstest` at top of file |
| `NewInProcessExecutor` panics without the safety env var | Runtime `os.Getenv` check in constructor |
| A loud ASCII-art warning is printed to stderr on every activation | `printInProcessWarning()` in constructor |
| Normal `go build`, `go test ./...`, and release CI never set the tag | CI pipeline config |

**Never** add `//go:build inprocesstest` or `NewInProcessExecutor` to any file
that is also compiled without the tag. If you need a test helper that touches
production code, use interfaces (the `TaskExecutor` interface exists for this
purpose).

---

## Submitting Changes

1. Run `make test` and `make vet` before opening a PR.
2. For changes touching `cmd/aegisclaw/`, run the journey tests explicitly:
   ```bash
   go test ./cmd/aegisclaw/ -run 'Integration|Journey' -v
   ```
3. For changes touching the agent loop or executor:
   ```bash
   make test-inprocess
   ```
4. Update or regenerate golden traces if your change intentionally alters
   tool call sequences or final answers:
   ```bash
   UPDATE_SNAPSHOTS=1 go test ./cmd/aegisclaw/ -run TestGolden
   ```
5. Ensure `go build ./...` (no extra tags) produces a binary that contains
   **no** in-process executor code.
