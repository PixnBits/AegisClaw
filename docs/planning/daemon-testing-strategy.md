# Host Daemon and TCB — Testing Strategy

This document complements [docs/testing-standards.md](../testing-standards.md) and the canonical requirements matrix in [docs/implementation-plan/03-daemon-minimal-tcb-refactor.md](../implementation-plan/03-daemon-minimal-tcb-refactor.md).

## Goals

- Keep **default CI** fast: full `go test ./...` without KVM or long VM lifecycles.
- Keep **privileged and environmental guarantees** honest: nothing security-critical should rely only on “we ran it once by hand” without a documented tier or backlog item ([daemon-test-backlog.md](daemon-test-backlog.md)).

## What runs where

| Trigger | Commands | Expectations |
|---------|----------|--------------|
| Every PR / local before push | `make vet`, `make test` | All packages green; Linux-specific tests may skip with explicit reason |
| Before merging risky daemon/IPC changes | `make test` + targeted `go test ./cmd/aegisclaw/ ./internal/paths/ ./internal/api/ ./internal/vault/` | Same as CI focus areas |
| Weekly or pre-release | `make test-integration`, `make build-static`, `make fuzz` (time-boxed) | Integration tag tests may skip if KVM absent; static build must pass before release artifacts |
| Manual / staging | Full stack with Firecracker, `aegisclaw` as root where required | Watchdog and kill containment scenarios from [06-sandbox-lifecycle-containment.md](../implementation-plan/06-sandbox-lifecycle-containment.md) |

## Linux-first assumptions

- Peer credential and socket path tests are authoritative on **Linux**.
- macOS/Windows sandboxes use different backends; do not treat Linux-only skips as “done” for cross-platform claims without equivalent tests or documented limitations.

## Regression triage

1. **Fails in `make test`**: fix before merge (P0).
2. **Fails only in `make test-integration`**: treat as P0 if the test is tagged for a behavior we claim in docs; otherwise file or update backlog entry with owner.
3. **Environment skip**: ensure skip message names the missing capability (caps, seccomp, KVM) so dashboards do not misread green as proof.

## Related

- [docs/planning/daemon-test-backlog.md](daemon-test-backlog.md) — prioritized gaps from the Task 03 matrix
- [docs/implementation-plan/04-unix-socket-hardening.md](../implementation-plan/04-unix-socket-hardening.md) — socket-specific acceptance tests (traceability: Task 03 matrix)
- [docs/implementation-plan/06-sandbox-lifecycle-containment.md](../implementation-plan/06-sandbox-lifecycle-containment.md) — lifecycle and watchdog (traceability: Task 03 matrix)
