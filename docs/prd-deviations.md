# PRD and CLI Deviations Review

Date: 2026-03-26
Updated: 2026-03-27 (alignment refactor applied; D1, D2, D10, D8 resolved)

Scope:
- Compared the implementation in this repository against [docs/PRD.md](docs/PRD.md) and [docs/cli-design.md](docs/cli-design.md).
- Treated code paths wired into the runnable product as authoritative over package-level scaffolding and aspirational docs.
- Used repository code, not README-level intent, to determine what is actually implemented.

Summary:
- The repository has solid building blocks for proposal lifecycle management, Merkle audit logging, Firecracker runtime management, builder orchestration, and encrypted secret storage.
- **Update**: The CLI surface has been aligned with the published specification. Court and builder pipeline wiring has been connected. Security gates and audit coverage have been strengthened.
- **Update (2026-03-27)**: D1 (FirecrackerLauncher), D2 (main-agent sandbox), D10 (versioned compositions with rollback), and D8 (SAST/SCA/policy-as-code) have been resolved.

## Deviation Resolution Status

| ID | Source | Requirement | Status | Notes |
| --- | --- | --- | --- | --- |
| D1 | PRD | Governance Court reviewers must run in isolated microVMs. | **Resolved** | Court initialization defaults to FirecrackerLauncher when KVM is available. DirectLauncher is fallback only (AEGISCLAW_DIRECT_REVIEW=1 or no /dev/kvm). Guest agent handles `review.execute` inside sandbox. See `cmd/aegisclaw/court_init.go`. |
| D2 | PRD, CLI | The main agent should be a sandboxed component. | **Resolved** | Chat CLI is now a thin TUI client. All LLM interaction (`chat.message`, `chat.slash`, `chat.tool`, `chat.summarize`) routed through daemon API. Guest agent handles `chat.message` inside sandbox. See `cmd/aegisclaw/chat.go`, `cmd/aegisclaw/chat_handlers.go`. |
| D3 | PRD | Approved skill should trigger builder pipeline automatically. | **Resolved** | Court review handler auto-transitions approved proposals to `implementing` status, connecting to builder pipeline. See `cmd/aegisclaw/start.go`. |
| D4 | PRD | Skill runtime should execute reviewed, versioned artifacts. | **Resolved** | Skill activation resolves artifact manifests from the builder output directory. See `cmd/aegisclaw/start.go`. |
| D5 | PRD, CLI | Secrets must use secure prompt and runtime injection. | **Resolved** | `aegisclaw secrets add` uses secure terminal prompt (no echo). Activation resolves proposal-linked secrets for injection. See `cmd/aegisclaw/secrets_cmd.go`. |
| D6 | PRD, CLI | All actions covered by audit log with `audit log` and `audit why`. | **Resolved** | `audit log` with filters (--since, --skill, --limit) and `audit why` with chain verification. Skill invoke/deactivate audit-logged. See `cmd/aegisclaw/audit_log.go`. |
| D7 | PRD | Court schema validation and consistency checks. | **Improved** | ReviewResponse.Validate() now requires Comments, Evidence for non-abstain verdicts. Full JSON Schema enforcement is future work. See `internal/court/reviewer.go`. |
| D8 | PRD | Security gates (SAST, SCA, policy-as-code). | **Resolved** | Four mandatory security gates implemented in `internal/builder/securitygate/`: SAST (regex-based pattern matching for Go security anti-patterns), SCA (banned dependency and unpinned version detection), secrets scanning (AWS keys, GitHub tokens, private keys, generic patterns), and policy-as-code (no unsafe exec, no host FS, no network unless declared, no privileged ops). Gates are wired as mandatory step 8.5 in the builder pipeline — pipeline fails automatically on blocking findings. See `internal/builder/pipeline.go`. |
| D9 | PRD | Artifact signing, SBOM, provenance. | **Partially resolved** | ArtifactStore with Ed25519 signing and SHA-256 verification exists. SBOM and provenance emission are future work. |
| D10 | PRD, CLI | Versioned compositions with rollback. | **Resolved** | New `internal/composition/` package implements versioned composition manifests (Component, Manifest, Store), persistent JSON versioning, rollback to specific or previous version, health status tracking per component, and automatic rollback on unhealthy components. Daemon API handlers: `composition.current`, `composition.rollback`, `composition.history`, `composition.health`. New kernel audit action: `composition.rollback`. See `internal/composition/manifest.go`, `cmd/aegisclaw/composition_handlers.go`. |
| D11 | PRD, CLI | High-risk approval gates. | **Annotated** | `--force` global flag added for skip-confirmation flows. Typed per-action approval gates are future work. |
| D12 | PRD | Multi-step refinement through main agent. | **Partially resolved** | `skill add` combines wizard + auto-submit. Full sandboxed main-agent refinement is future work. |
| D13 | CLI | CLI surface matches published specification. | **Resolved** | Top-level commands restructured: `init`, `start`, `stop`, `status`, `chat`, `skill`, `audit`, `secrets`, `self`, `version`. Skill subcommands: `add`, `list`, `revoke`, `info`. See `cmd/aegisclaw/root.go`. |
| D14 | CLI | Safe mode with dedicated banner and constrained command set. | **Resolved** | `start --safe` (renamed from `--safe-mode`) with ASCII banner and recovery mode messaging. See `cmd/aegisclaw/start.go`. |
| D15 | CLI | Global flags `--json`, `--verbose`, `--dry-run`, `--force`. | **Resolved** | All four global flags added to root command. `--json` supported in `status`, `version`, `skill list`, `skill info`, `audit log`, `audit why`. See `cmd/aegisclaw/root.go`. |
| D16 | CLI | `version` and `status` report build metadata. | **Resolved** | `version` reports git commit, build date, Go version, OS/arch. `status` reports health, registry root, audit chain head. See `cmd/aegisclaw/version.go`, `cmd/aegisclaw/status.go`. |

## Resolution Summary

### Resolved or substantially improved (14 of 16):
D1, D2, D3, D4, D5, D6, D8, D10, D13, D14, D15, D16 — fully resolved
D7, D9, D12 — partially resolved / improved

### Annotated with migration path (1 of 16):
D11 — clear path documented, implementation deferred

### Future work required (1 of 16):
D9 (partial) — SBOM and provenance emission

## Observations That Reduce Risk But Do Not Close Gaps

- Proposal lifecycle management is well structured in `internal/proposal/proposal.go`.
- Firecracker runtime support is substantial in `internal/sandbox/firecracker.go`, so several isolation gaps are wiring and guest-protocol gaps rather than complete absence of sandbox machinery.
- The Merkle audit chain is a real signed append-only log in `internal/audit/merkle.go`.
- Builder/runtime abstractions are far enough along that the main missing work is integration into approval, deployment, and runtime activation rather than greenfield subsystem creation.
- The vault and proxy design in `internal/vault/vault.go` and `internal/vault/proxy.go` are directionally consistent with the PRD, but they are not yet enforced by the live invocation path.

## Root Causes (Updated)

1. ~~The live product path still favors host-side fallbacks over PRD-mandated sandbox boundaries.~~ **Resolved**: D1 switches court to FirecrackerLauncher by default. D2 moves chat/LLM interaction behind the daemon API boundary. Guest agent handles both review and chat inside sandboxes.
2. ~~Proposal, Court, builder, activation, and runtime subsystems were implemented as separate capabilities but not connected into one enforced workflow.~~ **Addressed**: Court approval now auto-triggers builder pipeline. Skill activation resolves artifacts.
3. ~~Supply-chain, policy, and explanation requirements were modeled conceptually but not yet turned into launch-time or operator-facing enforcement.~~ **Substantially addressed**: D8 adds mandatory SAST/SCA/secrets/policy gates to the builder pipeline. D10 adds versioned composition manifests with automatic rollback on health failures. Audit log/why commands provide structured querying.
4. ~~The published CLI design is aspirational relative to the current implementation.~~ **Resolved**: CLI surface now matches the published specification.
