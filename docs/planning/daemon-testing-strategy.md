# Host Daemon Testing Strategy

**Goal**: Build high confidence that the Host Daemon remains minimal, hardened, and free of regressions — especially for paranoid users.

## 1. Testing Philosophy

- **Trust through verification**, not just code review.
- Prevent erosion of the minimal TCB over time.
- Make regressions **loud and early** (fail fast in CI).
- Balance thoroughness with practicality (focus on high-risk areas).
- Combine multiple layers: unit tests, build-time checks, integration tests, and static analysis.

## 2. Core Test Categories

### A. Functional / Behavioral Tests
- Unix socket server behavior
- VM lifecycle management (start/stop/monitor)
- Keypair generation and distribution
- Merkle root signing
- AegisHub and Store VM watchdog behavior

### B. Security & Hardening Tests
- Capability dropping correctness
- seccomp filter application
- Lifecycle containment (VM termination on daemon death)
- Unix socket permission enforcement (0700/0600)
- Peer credential / authorization checks
- Absence of secret handling paths

### C. Non-Functional / Constraint Tests
- Idle memory usage (< 20 MB target)
- Static binary verification
- Lines of Code (LOC) budget
- Dependency minimalism

### D. Regression Prevention
- Forbidden pattern detection (business logic, governance, secret handling)
- Interface contract tests (e.g., `SandboxBackend`)
- Snapshot-style tests for hardening setup functions

## 3. Recommended Test Implementation

### Unit Tests (`cmd/aegisclaw/*_test.go`)
- Test individual functions in isolation (e.g., `createSecureSocket`, `dropCapabilities`).
- Use table-driven tests for permission checks and error cases.
- Mock external dependencies where possible.

### Build-Time / CI Checks
- `make build-static` must succeed.
- Memory measurement job (run daemon + capture RSS).
- `go test -race` on all packages.
- Static analysis: `gosec`, `staticcheck`, `govulncheck`.

### Integration / End-to-End
- Test daemon + AegisHub interaction (launch, health, restart).
- Test daemon + Store VM interaction.
- Test full lifecycle containment (kill daemon → verify VMs are gone).
- Unix socket authorization matrix tests.

### Static Analysis & Policy
- Grep / regex rules in CI for forbidden patterns:
  - Business logic keywords (`proposal`, `chat`, `memory`, `eventbus`, etc.)
  - Secret-related terms
  - Governance logic
- Enforce via pre-commit hooks or CI job.

## 4. High-Priority Test Areas (Paranoid Focus)

| Area                        | Risk Level | Test Type          | How to Test                              | Priority |
|-----------------------------|------------|--------------------|------------------------------------------|----------|
| Lifecycle Containment       | High       | Integration        | Kill daemon, verify VMs terminated       | Critical |
| Capability Dropping         | High       | Unit + Build       | Assert dropped caps, test function       | High     |
| seccomp Filter              | High       | Unit + CI          | Apply filter, test violation behavior    | High     |
| Unix Socket Permissions     | High       | Unit               | Create socket, verify 0600 permissions   | High     |
| No Secret Handling          | Critical   | Static + Review    | Grep + policy tests + code review        | Critical |
| No Business Logic           | Critical   | Static + Review    | Forbidden pattern detection in CI        | Critical |
| Memory Usage                | Medium     | Build/CI           | Automated RSS measurement                | High     |
| Static Binary               | Medium     | Build              | `CGO_ENABLED=0` build + file check       | High     |
| Key Distribution            | High       | Integration        | Verify keys only go to correct VMs       | High     |

## 5. Automation & CI Recommendations

- Run full test suite on every PR.
- Dedicated "Hardening" CI job for seccomp, capabilities, and permissions.
- Nightly job for memory + LOC measurement.
- Pre-commit hook for forbidden pattern grep.
- Use GitHub Actions (or equivalent) with matrix for Linux.

## 6. Long-term Trust Building

- Keep test coverage high on all hardening and TCB code.
- Make test failures block merges.
- Regularly review and expand this strategy as the daemon evolves.
- Document any accepted risks or exceptions clearly.

This strategy should evolve with the project. Treat it as a living document.