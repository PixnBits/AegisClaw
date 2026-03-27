# PRD Deviations Review

Date: 2026-03-26

Scope:
- Compared the implementation in this repository against [docs/PRD.md](docs/PRD.md) only.
- Ignored other documents under [docs](docs) unless they were directly contradicted by code.
- Treated code paths that are wired into the running product as authoritative over package-level scaffolding.

Summary:
- The repository has solid foundations for proposals, audit logging, Firecracker sandboxing, secret storage, and Court personas.
- The biggest gaps are in product wiring: the live Court review path bypasses reviewer isolation, the builder and deploy path is not connected end-to-end, and several PRD security/operations guarantees are only partially implemented or not implemented at all.

## Deviation List

| ID | PRD Requirement | Current Implementation | Evidence | Deviation |
| --- | --- | --- | --- | --- |
| D1 | Governance Court reviewers must run in isolated microVMs. | The live Court engine is initialized with a host-side DirectLauncher that calls Ollama directly without creating a sandbox. | [cmd/aegisclaw/court_cmd.go](cmd/aegisclaw/court_cmd.go), [internal/court/direct_launcher.go](internal/court/direct_launcher.go) | The primary review path violates the PRD's core isolation model for reviewers. |
| D2 | The main agent should operate as a sandboxed component and route to skills safely. | The chat interface runs on the host process, talks to Ollama directly, and invokes daemon APIs from the CLI process. | [cmd/aegisclaw/chat.go](cmd/aegisclaw/chat.go) | The PRD's main-agent sandbox boundary is not implemented in the actual user flow. |
| D3 | A newly approved skill should build, deploy, activate in its own microVM, and become immediately usable. | There is a builder runtime and pipeline package, but the CLI only exposes builder status, the court flow does not trigger the pipeline, and the guest agent returns a stub when no deployed tool exists. | [cmd/aegisclaw/builder_cmd.go](cmd/aegisclaw/builder_cmd.go), [internal/builder/pipeline.go](internal/builder/pipeline.go), [cmd/guest-agent/main.go](cmd/guest-agent/main.go) | The end-to-end skill-addition workflow described by the PRD is incomplete. |
| D4 | Skill runtime should execute reviewed skill artifacts, not a generic placeholder environment. | Skill activation starts a sandbox from the shared rootfs template and registers it, but there is no link from an approved proposal or built artifact to the activated VM. | [cmd/aegisclaw/start.go](cmd/aegisclaw/start.go), [cmd/aegisclaw/skill_activate.go](cmd/aegisclaw/skill_activate.go) | Skill activation exists, but it is not tied to reviewed, versioned skill outputs as required by the PRD. |
| D5 | Secrets must be injected at runtime through a dedicated proxy and used by real skill execution. | The vault and proxy are implemented, and the guest agent supports secrets.inject, but proxy resolution is only exercised in tests and is not wired into activation or invocation paths. | [internal/vault/vault.go](internal/vault/vault.go), [internal/vault/proxy.go](internal/vault/proxy.go), [cmd/guest-agent/main.go](cmd/guest-agent/main.go), [internal/vault/proxy_test.go](internal/vault/proxy_test.go) | The secret-handling architecture exists, but the live skill path does not enforce PRD-compliant secret delivery end to end. |
| D6 | All proposals, reviews, code changes, deployments, and runtime actions must be covered by the append-only Merkle audit log and support why-style inspection. | The Merkle log is real and proposal/builder/activation events are logged, but deployment is not implemented, skill invocation is not logged in the daemon path, and there is no why-query feature beyond raw exploration. | [internal/audit/merkle.go](internal/audit/merkle.go), [cmd/aegisclaw/start.go](cmd/aegisclaw/start.go), [cmd/aegisclaw/audit_explorer.go](cmd/aegisclaw/audit_explorer.go) | Audit infrastructure exists, but PRD-required coverage and explanation capabilities are incomplete. |
| D7 | Court interactions should use strict schema validation with high structured-JSON reliability and repeated reviewer consistency checks. | Reviewer responses are unmarshaled into Go structs and lightly validated, but there is no JSON Schema enforcement, no three-consistent-output check per reviewer, and no evidence of success-rate instrumentation. | [internal/court/reviewer.go](internal/court/reviewer.go), [internal/court/direct_launcher.go](internal/court/direct_launcher.go), [config/personas.yaml](config/personas.yaml) | The PRD's schema-governed, consistency-checked Court contract is only partially implemented. |
| D8 | Policy-as-code, SAST, SCA, secrets scanning, and adversarial testing should gate changes. | The sandbox spec validates a few invariants in code, but there is no OPA/Rego enforcement, no SAST/SCA tooling integrated into the build path, and no adversarial or red-team harness in the live workflow. | [internal/sandbox/spec.go](internal/sandbox/spec.go), [internal/builder/analysis.go](internal/builder/analysis.go), [cmd/aegisclaw/builder_cmd.go](cmd/aegisclaw/builder_cmd.go) | Security gates are below the PRD baseline. |
| D9 | Build artifacts and microVM assets should be signed, hashed, SBOM-backed, and verified before launch. | Firecracker runtime checks that the rootfs exists, but there is no signature verification, no SBOM generation, no provenance emission, and no composition manifest. | [internal/sandbox/firecracker.go](internal/sandbox/firecracker.go), [internal/config/config.go](internal/config/config.go) | Supply-chain controls promised in the PRD are not implemented. |
| D10 | Deployment should use versioned compositions with rollback and health-based recovery. | Safe mode exists, but rollback is explicitly unimplemented and there is no composition manifest or deployment controller. | [cmd/aegisclaw/start.go](cmd/aegisclaw/start.go), [cmd/aegisclaw/audit_explorer.go](cmd/aegisclaw/audit_explorer.go) | Recovery and release management are materially behind the PRD. |
| D11 | High-risk actions should require explicit human confirmation and the system should expose decision explanations. | Safe mode blocks execution globally, but the current skill invocation path executes directly once requested and there is no typed high-risk approval gate or structured why endpoint. | [cmd/aegisclaw/chat.go](cmd/aegisclaw/chat.go), [cmd/aegisclaw/start.go](cmd/aegisclaw/start.go) | Human-in-the-loop controls are weaker and less explicit than the PRD requires. |
| D12 | The PRD describes multi-step, interactive refinement for new skills through the main agent. | New skill creation is handled by a CLI wizard or flags rather than by a sandboxed main agent conversation that refines requirements and feeds the Court. | [cmd/aegisclaw/propose_skill.go](cmd/aegisclaw/propose_skill.go), [internal/wizard/wizard.go](internal/wizard/wizard.go) | The current UX is functional, but it is not the product flow described in the PRD. |

## Observations That Reduce Risk But Do Not Close Gaps

- Proposal lifecycle management is well structured in [internal/proposal/proposal.go](internal/proposal/proposal.go).
- Firecracker runtime support is substantial in [internal/sandbox/firecracker.go](internal/sandbox/firecracker.go), so the isolation gap is mostly a wiring and guest-protocol problem, not a complete absence of sandbox machinery.
- The Merkle audit chain is a real, signed append-only log in [internal/audit/merkle.go](internal/audit/merkle.go).
- The vault and proxy design in [internal/vault/vault.go](internal/vault/vault.go) and [internal/vault/proxy.go](internal/vault/proxy.go) are consistent with the PRD, but not yet enforced by the operational path.

## Most Important Root Causes

1. The product path favors host-side fallbacks over PRD-mandated sandbox boundaries.
2. The proposal, review, build, deploy, and runtime subsystems were implemented as separate capabilities but not connected into one enforced workflow.
3. Supply-chain and policy controls were designed conceptually but not turned into launch-time or CI/CD enforcement.
4. Some CLI text promises PRD behavior that the running code does not yet deliver.