# PRD Alignment Plan

Date: 2026-03-26

Source:
- Derived from [docs/prd-deviations.md](docs/prd-deviations.md).
- Goal is implementation alignment with [docs/PRD.md](docs/PRD.md), not PRD simplification.

## Priority Order

The fastest path to real PRD alignment is not to add more surface commands. It is to connect the existing subsystems into one enforced workflow and remove host-side bypasses.

## Implementation Reality Check

The deviation review shows that alignment is not a matter of layering new commands onto the current binary. The implemented CLI surface and the live runtime wiring diverge substantially from both the PRD and [docs/cli-design.md](docs/cli-design.md):

- The current top-level command model includes legacy and implementation-specific surfaces such as `sandbox`, `propose`, `court`, `builder`, `secret`, and `model`, while the published CLI centers on `chat`, `skill`, `audit`, `secrets`, `self`, and a simpler operator flow.
- The current runtime still depends on host-side orchestration for chat and Court review, which is structurally different from the PRD's sandbox-first architecture.
- Several subsystems exist as partial capabilities with their own command entrypoints, but PRD alignment requires them to become daemon-managed stages in one enforced workflow rather than user-visible standalone control surfaces.

Implementation consequence:

- A meaningful alignment effort will likely remove or heavily shrink significant portions of existing code rather than preserving all current commands and flows.
- Some current commands should probably become internal or disappear entirely once their responsibilities move behind the daemon or behind higher-level `skill`, `audit`, `chat`, and `self` workflows.
- We should prefer deleting obsolete host-side bypasses and mismatched CLI pathways over carrying compatibility code that preserves non-PRD behavior.

This should be treated as an expected part of alignment work, not as churn or regression. In several areas, removal of existing code is the cleanest path to making the architecture match the trust model.

## Phase 1: Restore Required Isolation Boundaries

### 1. Replace host-side Court review with Firecracker-backed review execution

Why:
- D1 is the highest-severity deviation because it breaks the platform's core trust model.

Code changes:
- Switch Court engine initialization from DirectLauncher to FirecrackerLauncher in [cmd/aegisclaw/court_cmd.go](cmd/aegisclaw/court_cmd.go).
- Implement the review.execute request path inside the guest agent in [cmd/guest-agent/main.go](cmd/guest-agent/main.go).
- Fix control-plane and vsock plumbing for reviewer sandboxes in [internal/court/reviewer.go](internal/court/reviewer.go), [internal/kernel/controlplane.go](internal/kernel/controlplane.go), and [internal/ipc/bridge.go](internal/ipc/bridge.go).
- Ensure reviewer sandbox networking allows only the local Ollama endpoint or an approved proxy path in [internal/sandbox/firecracker.go](internal/sandbox/firecracker.go) and [internal/sandbox/netpolicy.go](internal/sandbox/netpolicy.go).

Acceptance criteria:
- court review launches one microVM per reviewer model/persona.
- No review request reaches Ollama from the host CLI or daemon process.
- Reviewer sandboxes can complete a review and return structured results over vsock.

### 2. Move the main agent orchestration behind a sandbox boundary or explicitly split host UI from sandboxed agent runtime

Why:
- D2 remains a PRD violation even if the Court is fixed.

Code changes:
- Introduce a dedicated main-agent sandbox runtime that owns LLM conversation and tool-routing logic now embedded in [cmd/aegisclaw/chat.go](cmd/aegisclaw/chat.go).
- Keep the current CLI/TUI as a thin UI client that forwards user input to the daemon and receives agent responses.
- Reuse the existing daemon API layer in [internal/api/server.go](internal/api/server.go) and [internal/api/client.go](internal/api/client.go) for the host-to-daemon boundary.

Acceptance criteria:
- The host process no longer calls Ollama for chat directly.
- The sandboxed main agent becomes the only component authorized to plan tool usage.

## Phase 2: Complete the Skill Lifecycle

### 3. Wire Court approval to the builder pipeline automatically

Why:
- D3 and D4 are the main reason the product cannot satisfy the PRD's end-to-end skill workflow.

Code changes:
- Add a daemon API and service path for starting builder pipeline runs after approval in [internal/api/server.go](internal/api/server.go) and [cmd/aegisclaw/start.go](cmd/aegisclaw/start.go).
- Instantiate and use [internal/builder/pipeline.go](internal/builder/pipeline.go) from the daemon runtime instead of leaving it as a disconnected package.
- Expand [cmd/aegisclaw/builder_cmd.go](cmd/aegisclaw/builder_cmd.go) with run, inspect, and retry commands instead of status-only behavior.
- Transition approved proposals into implementing and complete states automatically via [internal/proposal/store.go](internal/proposal/store.go) and [internal/proposal/proposal.go](internal/proposal/proposal.go).

Acceptance criteria:
- Approved proposals trigger a builder run without manual file movement.
- Builder results are persisted, visible, and linked back to the proposal.

### 4. Change skill activation to deploy reviewed artifacts, not a generic template rootfs

Why:
- The current skill activation path can start a VM, but it does not prove that the VM is running reviewed skill code.

Code changes:
- Define a deployable artifact format that the builder emits: at minimum a versioned skill bundle, and ideally the PRD's signed rootfs.ext4 plus vmconfig.json model.
- Update [internal/builder/artifact.go](internal/builder/artifact.go) and [internal/builder/pipeline.go](internal/builder/pipeline.go) to emit deployable artifacts instead of only Git diffs and hashes.
- Modify [cmd/aegisclaw/start.go](cmd/aegisclaw/start.go) so skill.activate resolves the latest approved artifact for a skill instead of always using env.Config.Rootfs.Template.
- Update [cmd/guest-agent/main.go](cmd/guest-agent/main.go) to load deployed tool code from the artifact-defined location and remove the current stub fallback once deployment exists.

Acceptance criteria:
- Activating a skill always boots the reviewed artifact for that skill version.
- Invoking a skill executes built code, not the placeholder response path.

### 5. Enforce runtime secret injection on activation/invocation

Why:
- D5 is a security gap, not just a missing feature.

Code changes:
- Read secret references from approved proposals or skill metadata and resolve them through [internal/vault/proxy.go](internal/vault/proxy.go).
- Send secrets.inject over vsock before skill use, using [cmd/guest-agent/main.go](cmd/guest-agent/main.go) support already present.
- Prevent activation of a skill that declares secrets until required secret references exist in the vault.
- Add daemon-side audit entries for secret injection events without logging plaintext values.

Acceptance criteria:
- Secrets never travel through prompts, code generation outputs, or persistent logs.
- A skill with declared secrets cannot execute unless injection succeeds.

## Phase 3: Enforce PRD Security and Supply-Chain Gates

### 6. Add strict schema validation and reviewer consistency checks

Why:
- D7 means Court outputs are more permissive than the PRD allows.

Code changes:
- Promote persona output schemas from documentation strings in [config/personas.yaml](config/personas.yaml) into versioned machine-readable JSON Schemas under docs/schemas or a new runtime schema directory.
- Validate all reviewer responses against those schemas before accepting them in [internal/court/reviewer.go](internal/court/reviewer.go).
- Add per-reviewer retry logic so each persona must produce three consistent outputs before its review is accepted.
- Record structured-JSON parse/validation success metrics in audit or runtime telemetry.

Acceptance criteria:
- Invalid or inconsistent reviewer outputs are retried or rejected automatically.
- Structured-output reliability becomes measurable instead of assumed.

### 7. Add policy-as-code and builder security gates

Why:
- D8 is a material gap between the PRD and implementation maturity.

Code changes:
- Add OPA/Rego policy bundles under docs/policies or a runtime policy directory and evaluate them from the builder and deployment flow.
- Extend [internal/builder/analysis.go](internal/builder/analysis.go) so it runs concrete gates: go test, static analysis, dependency checks, secrets scanning, and policy validation.
- Fail pipeline runs automatically when isolation or secret-handling policies are violated.

Acceptance criteria:
- No skill artifact can be deployed unless policy, static analysis, and dependency gates pass.

### 8. Add artifact signing, SBOM generation, provenance, and launch-time verification

Why:
- D9 leaves the supply chain effectively unenforced.

Code changes:
- Extend builder outputs to generate SBOM and provenance metadata in [internal/builder/artifact.go](internal/builder/artifact.go).
- Sign build artifacts using Sigstore or GPG from the builder/deploy pipeline.
- Update [internal/sandbox/firecracker.go](internal/sandbox/firecracker.go) so Create or Start verifies artifact signatures and hashes before launch.
- Add model and rootfs hash verification where assets are provisioned in [internal/provision/provision.go](internal/provision/provision.go).

Acceptance criteria:
- A sandbox cannot launch from an unsigned or hash-mismatched artifact.
- Every deployable artifact has a verifiable SBOM and provenance record.

## Phase 4: Operations, Governance, and User Controls

### 9. Implement versioned composition manifests and rollback

Why:
- D10 prevents the recovery model in the PRD from being real.

Code changes:
- Add a composition manifest data model for active component versions.
- Store manifest revisions in Git and use them as the source of truth for deployment.
- Replace the placeholder rollback callback in [cmd/aegisclaw/audit_explorer.go](cmd/aegisclaw/audit_explorer.go) with a real rollback engine that restores the previous manifest and restarts affected sandboxes.
- Add health checks for sandboxes and automatic rollback triggers in the daemon runtime.

Acceptance criteria:
- The system can roll back a deployment to a known prior composition.
- Health failures trigger a deterministic recovery path.

### 10. Add explicit high-risk approval gates and why queries

Why:
- D6 and D11 are governance gaps that affect enterprise trust and auditability.

Code changes:
- Classify tool actions by risk and require explicit approval before executing high-risk operations.
- Add a daemon/API path to query the audit log for action rationale, proposal history, and review evidence.
- Expose that functionality through CLI and TUI, not only raw log browsing.

Acceptance criteria:
- High-risk operations pause for human approval with a logged decision.
- Users can ask why an action was proposed, approved, rejected, or executed and receive a structured answer from audit records.

### 11. Bring the proposal UX closer to the PRD's refinement flow

Why:
- D12 is lower risk than isolation or deployment, but it matters for product fidelity.

Code changes:
- Preserve the wizard as a fallback, but let the sandboxed main agent conduct requirement refinement interactively.
- Persist refinement questions, answers, and generated proposal JSON to the audit trail.
- Ensure CISO and User Advocate feedback can appear before full Court review begins.

Acceptance criteria:
- A user can request a new skill conversationally and the system refines requirements before submission.

## Recommended Execution Sequence

1. Fix reviewer isolation first.
2. Wire approval to build and deployable artifacts.
3. Enforce secret injection and launch-time artifact verification.
4. Add schema, policy, and supply-chain gates.
5. Implement rollback, why queries, and high-risk approvals.
6. Improve proposal refinement UX after the security-critical workflow is real.

## Expected Code Reduction Areas

The following parts of the current codebase are candidates for major simplification, internalization, or removal during alignment:

- Host-side chat orchestration in [cmd/aegisclaw/chat.go](cmd/aegisclaw/chat.go) once the main agent moves behind a sandbox boundary.
- Host-side Court launcher wiring in [cmd/aegisclaw/court_cmd.go](cmd/aegisclaw/court_cmd.go) once review execution is daemon-managed and Firecracker-backed.
- Standalone proposal and builder command flows in [cmd/aegisclaw/propose_skill.go](cmd/aegisclaw/propose_skill.go) and [cmd/aegisclaw/builder_cmd.go](cmd/aegisclaw/builder_cmd.go) if those stages become internal parts of the approved workflow.
- Legacy CLI surface in [cmd/aegisclaw/root.go](cmd/aegisclaw/root.go) that does not match the published contract, especially where commands expose implementation details rather than product operations.
- Placeholder guest execution behavior in [cmd/guest-agent/main.go](cmd/guest-agent/main.go) once artifact-backed deployment replaces the current stub path.

The goal should be architectural convergence, not preservation of every current entrypoint.

## Files Most Likely To Change

- [cmd/aegisclaw/court_cmd.go](cmd/aegisclaw/court_cmd.go)
- [cmd/aegisclaw/start.go](cmd/aegisclaw/start.go)
- [cmd/aegisclaw/chat.go](cmd/aegisclaw/chat.go)
- [cmd/aegisclaw/builder_cmd.go](cmd/aegisclaw/builder_cmd.go)
- [cmd/aegisclaw/audit_explorer.go](cmd/aegisclaw/audit_explorer.go)
- [cmd/guest-agent/main.go](cmd/guest-agent/main.go)
- [internal/court/reviewer.go](internal/court/reviewer.go)
- [internal/builder/pipeline.go](internal/builder/pipeline.go)
- [internal/builder/artifact.go](internal/builder/artifact.go)
- [internal/sandbox/firecracker.go](internal/sandbox/firecracker.go)
- [internal/kernel/controlplane.go](internal/kernel/controlplane.go)
- [internal/ipc/bridge.go](internal/ipc/bridge.go)
- [internal/vault/proxy.go](internal/vault/proxy.go)
- [internal/provision/provision.go](internal/provision/provision.go)
- [internal/api/server.go](internal/api/server.go)

## Suggested First Implementation Slice

If we want the smallest change set that meaningfully improves PRD alignment, the first slice should be:

1. Firecracker-backed court reviews.
2. Builder pipeline trigger after approval.
3. Deployment of built skill artifacts into activated skill VMs.
4. Secret proxy injection on skill startup.

That slice closes the most serious trust and functionality gaps while preserving the existing architecture.