# Testing Standards for AegisClaw v2

## Core Requirements

- **Unit Test Coverage**: ≥ 80% for all new code
- **Integration Tests**: All User Journeys (1–9) must have automated integration tests (see [docs/implementation-plan/15-user-journey-automation.md](implementation-plan/15-user-journey-automation.md))
- **E2E Tests**: Web Portal flows must be covered with Playwright
- **Chaos Testing**: Regular testing of component failures and recovery
- **Traceability**: Every normative behavior in a component spec (for example [docs/specs/host-daemon.md](specs/host-daemon.md)) maps to at least one automated test, or to an explicitly documented exception (for example “requires KVM in CI”, “manual release gate only”)

## Testing Philosophy

- Test first where possible
- Every feature must have tests before it is considered complete
- Paranoid testing: assume components can fail or be compromised
- **Negative and abuse tests** are first-class: deny-by-default surfaces (Host Daemon TCB, IPC, vault paths) must prove stable rejection, not only happy paths

## Test Taxonomy

Use the narrowest test type that still proves the requirement.

| Type | Purpose | Typical scope | Example in repo |
|------|---------|---------------|-----------------|
| **Unit** | Fast, hermetic logic | Single package, temp dirs, mocks | `go test ./internal/paths/...` |
| **Package integration** | Multiple types in one process, real FS where cheap | Same module, no Firecracker | `internal/api/server_test.go` |
| **`integration` build tag** | Longer or environment-sensitive flows | `go test -tags=integration` (see Makefile) | `cmd/aegisclaw/lifecycle_integration_test.go` |
| **Contract / API** | Stable CLI or HTTP contracts, golden output | Cross-package boundaries | `cmd/aegisclaw/cli_api_contract_test.go` |
| **E2E** | Full user-visible flows | Playwright against portal/daemon | `cmd/aegisclaw/web_portal_e2e_sdlc_test.go` |
| **Fuzz** | Parser and boundary robustness | Bounded wall time | `make fuzz` |
| **Security / Builder gates** | Supply chain and policy enforcement | Builder VM pipeline | `internal/builder/securitygate/` |
| **Chaos / fault injection** | Recovery when dependencies die | Staged environments, not necessarily every PR | Documented in component plans |

**Host Daemon TCB** tests complement User Journey automation: they prove privileged bootstrap and containment; they do **not** replace journey tests for chat, Court, or SDLC flows. See [docs/implementation-plan/03-daemon-minimal-tcb-refactor.md](implementation-plan/03-daemon-minimal-tcb-refactor.md).

## CI Tiers

The Makefile encodes two default tiers (`make test` vs `make test-integration`):

| Tier | Command | When it runs | Rationale |
|------|---------|----------------|-----------|
| **PR / default** | `make test` (`go test ./...`) | Every push and merge request | Fast feedback; no KVM; no long VM lifecycles |
| **Rich integration** | `make test-integration` (`-tags=integration`, filtered runs) | Local dev, optional CI job, or labeled PRs | May assume Linux details, longer timeouts, or future KVM/Firecracker hooks |
| **Static binary gate** | `make build-static` | Release hygiene and hardening verification | Proves fully static `aegisclaw` per Host Daemon spec |
| **Fuzz** | `make fuzz` | Nightly or pre-release | Time-bounded exploration |

Contributors should document new environment-sensitive tests with an explicit `t.Skip` reason or build tags, not silent `t.Log`-only passes that look green without proving the property.

## Coverage Scope

- **New code**: default ≥ 80% line coverage for packages touched by the change (as reported by CI).
- **Security-critical packages** (daemon startup, IPC, `internal/paths`, vault access, audit signing entrypoints): treat **any substantive edit** as requiring the same bar as new code, even if the file predates the change, unless the PR documents a scoped exception and follow-up issue.
- **Generated code**: exclude from coverage targets in CI configuration when present.

## Required Test Types (checklist)

1. Unit tests
2. Integration tests (full system with sandboxes where the journey requires it)
3. E2E tests for CLI and Web Portal
4. Security gate tests in Builder VM
5. Safe Mode recovery tests

## CI/CD Requirements

- All tests in the default PR tier must pass before merge
- Coverage report generated on every PR (where CI is wired)
- Rich integration and fuzz tiers must not silently rot: if they are optional in CI, they are still listed in [docs/planning/daemon-testing-strategy.md](planning/daemon-testing-strategy.md) with expected cadence

## Golden Rule and Journeys

No feature is done until its **User Journey** has automated tests ([docs/roadmap.md](roadmap.md), [docs/implementation-plan/15-user-journey-automation.md](implementation-plan/15-user-journey-automation.md)). Daemon and layout specs are orthogonal: they gate **privilege and containment**, not product UX alone.

## Related Documents

- [docs/implementation-plan/](implementation-plan/) — numbered tasks and acceptance criteria
- [docs/implementation-plan/03-daemon-minimal-tcb-refactor.md](implementation-plan/03-daemon-minimal-tcb-refactor.md) — Host Daemon TCB requirements traceability matrix
- [docs/specs/](specs/) — component norms that drive test rows
- [docs/planning/daemon-testing-strategy.md](planning/daemon-testing-strategy.md) — when to run which daemon-related suites
- [docs/planning/daemon-test-backlog.md](planning/daemon-test-backlog.md) — prioritized gaps
