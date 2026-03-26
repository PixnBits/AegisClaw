# Product Requirements Document (PRD) – AegisClaw

**Secure-by-Design Local Agent Platform**

> Your personal agent you can trust like a paranoid enterprise.

## 1. Title, Version, Revision History & Approvals

**Document Title**  
Product Requirements Document (PRD) – AegisClaw v2.0 – Secure-by-Design, Local-First, Self-Evolving AI Agent Platform for Linux

**Approvals / RACI (Roles only – names to be added as team grows)**

| Role                        | Responsibility                          | RACI     |
|-----------------------------|-----------------------------------------|----------|
| Product Owner               | Overall vision, priorities, user needs  | Responsible / Accountable |
| Security Architect          | Threat model, isolation, secrets, audit | Responsible / Consulted   |
| CISO-equivalent Reviewer    | Governance Court policy, risk acceptance| Accountable / Consulted   |
| Core Engineer(s)            | Implementation, SDLC enforcement        | Responsible               |
| User Advocate / Tester      | Persona validation, UX flows            | Consulted                 |

Approvals will be captured via GitHub PR reviews and signed commits as the project matures.

## 2. Executive Summary / Product Overview

AegisClaw is a **paranoid-by-design**, local-first, self-evolving AI agent platform that runs exclusively on Linux with Ollama. It lets security-conscious users add powerful skills (Slack, GitHub, shell, file operations, etc.) through a rigorous, enterprise-grade SDLC enforced by an internal **Governance Court** of isolated LLM personas.

Unlike existing agents that expose broad privileges or rely on cloud services, AegisClaw treats every skill as a potential threat. Skills run in per-skill microVMs with read-only filesystems, dropped capabilities, private Docker daemons, and runtime secret injection via a network proxy — API keys **never** appear in prompts, code, logs, or LLM context. Antagonistic or malicious skills are made structurally impossible.

**Key Differentiators**
- Mature security, practices, and risk management built in from day one (not bolted on).
- Your own team of enterprise experts (Coder, Tester, CISO, Security Architect, User Advocate) for sage advice and multi-persona reviews — each reviewer runs in its own isolated microVM.
- Stay local by default, go global when you choose: use Ollama-only today, with pluggable support for models and data exposure appropriate for your risk profile tomorrow.
- Full self-improvement: the system can propose and apply its own patches via the Court.
- Everything is git-backed, auditable via an append-only Merkle-tree log, and reversible.

Target users range from hobbyist Linux admins to startups and enterprises who want an agent they can actually trust with real desktop and infrastructure actions.

## 3. Problem Statement & Opportunity

Today’s desktop and infrastructure agents suffer from critical gaps that make them unsuitable for anything beyond low-risk experimentation:

- **Credential leaks and secret exposure** — keys often end up in prompts, logs, or shared memory.
- Lack of **mature software engineering and risk-management practices** — most agents allow arbitrary code execution with minimal review or isolation.
- No governance for skill addition — anyone (or any prompt injection) can introduce dangerous capabilities.
- Non-reversible or hard-to-audit actions.
- Heavy cloud dependency or opaque supply chains.
- Poor scalability across user maturity levels (hobbyist → enterprise compliance needs).

Competitors highlight these issues:
- **OpenClaw** provides a flexible multi-platform personal assistant with broad messaging and tool support, but relies on a gateway model that inherits significant privilege and lacks the enforced multi-persona SDLC or per-skill microVM isolation AegisClaw mandates.
- **Sympozium** offers strong Kubernetes-native fleet orchestration with sandboxed sidecars and policy-as-CRD, but targets cluster-scale workloads rather than a lightweight, local-first Linux desktop agent with built-in paranoid review processes.

**Opportunity**  
In 2026–2028 there is strong demand for a truly trustworthy local agent across three segments that share the same Linux + Ollama foundation but have different risk postures:
- **Hobbyists / researchers** — want quick onboarding and powerful tools without trusting cloud providers.
- **Startups / small companies** — need speed plus basic audit trails for future compliance.
- **Enterprises** — demand pre-action visibility, custom policies, expert personas, and full auditability for regulatory reviews.

AegisClaw’s design intentionally scales across these audiences: the same core isolation and Court mechanisms serve all three, with configurable strictness levels (suggested by user profile at onboarding and changeable later via Court-reviewed proposals).

## 4. Vision, Mission & Strategic Goals

**Mission**  
Safe self-hosted, self-improving agents for an abundant future.

**Vision**
- **1-month horizon**: Reach self-hosting maturity where AegisClaw can open and review its own GitHub PRs using the Governance Court.
- **2-month horizon**: Friends and early testers are running it, providing feedback that the Court itself helps incorporate.
- **1-year (audacious) horizon**: AegisClaw gains measurable market share over broader agents like OpenClaw by being the clearly safer and more trustworthy choice for users who value real work over convenience.

**Strategic Goals** (measurable)
1. Zero isolation violations in any internal or external red-team exercises by public v1.0 release.
2. Every skill-addition workflow completes end-to-end in <15 minutes of user time (average).
3. The system successfully proposes and merges at least one self-improvement patch via the Court within the first month of self-hosting.
4. Support at least 10 production-grade skills with full audit trails and reversible actions.
5. Onboarding + first skill for a new hobbyist user takes <10 minutes.
6. Enterprise mode allows full customization of reviewer personas, policies, and approval flows while preserving the immutable isolation guarantees.
7. 98%+ structured JSON success rate with schema validation across all Court interactions.
8. Append-only Merkle-tree audit log covers 100% of proposals, reviews, code changes, and deployments.

## 5. Target Users, Personas & User Journeys

### Primary Personas

**Alex Rivera – Hobbyist / Security-Conscious Linux Admin**  
- Comfortable with Ubuntu, comfortable using terminal and Docker.  
- Wants to get started quickly, add useful tools (Slack, GitHub, shell helpers), and have the agent help improve both the agent itself and their own workflows.  
- Daily workflow: texts or talks to the agent(s) multiple times per day.  
- Success: Powerful capabilities without ever worrying about credential leaks or accidental damage.

**Startup Engineer / Founder**  
- Needs the agent to accelerate work “ASAP” while still producing auditable records.  
- Trusts the system to make good decisions but wants clear “why” logs in case a compliance body or investor ever asks.  
- Same daily chat-heavy interaction pattern; values speed + reversible actions.

**Enterprise DevOps / Security Engineer**  
- Runs Ubuntu in production environments.  
- Requires visibility into why decisions are made **before** actions occur (especially for compliance audit readiness).  
- Can set custom policies, expert reviewer personas, and tailored SDLC process flows.  
- Expects the same rock-solid isolation guarantees regardless of customization.

Additional enterprise job-function personas (to be expanded as needed): Platform Engineer, Compliance Officer, SRE, Internal Tools Developer.

### Suggested Configuration Trade-offs
At onboarding (and via later Court-reviewed proposals), the system will suggest strictness/speed profiles based on the detected or declared user type. Users can always adjust (with human review required for relaxing controls).

### Key User Journeys (to be detailed in Section 6)
1. First-time installation and onboarding.
2. Adding the first skill (e.g., Slack messaging) via the full Court process.
3. Daily operation of existing skills (chat → action → response).
4. Requesting a self-improvement patch or new capability.
5. Reviewing audit logs / “why” explanations (especially important for startup & enterprise).
6. Incident response or skill revocation.
7. Customizing policies / reviewer personas (enterprise).

## 6. Key Features, Functional Requirements & User Stories

All functional requirements are derived from the core vision of a paranoid-by-design agent. Features are grouped into epics with traceable IDs.

### Epic FR-001: Skill Management & Governance Court
**Description**: Users add, extend, review, and revoke skills through a rigorous, multi-persona SDLC enforced by the Governance Court.

**User Story FR-001.1 – Add a New Skill (Slack example expanded)**
- As a user, I want to request “Add a tool to send messages to Slack” so I can integrate real communication without compromising security.
- **Detailed Flow** (refined from v1.0):
  1. User sends natural-language request to the main agent sandbox.
  2. Main agent interactively refines via questions (business need, authentication method, rate limits, potential risks, success criteria).
  3. Refined proposal generated as structured JSON and stored in git.
  4. Governance Court spins up isolated reviewer microVMs with personas: Coder, Tester, CISO, Security Architect, User Advocate.
  5. CISO persona enforces network policy, secret proxy configuration, and updates the threat model.
  6. Code is generated/edited inside a clean builder sandbox → git commit + simulated PR.
  7. Reviewers iterate (approve / reject / ask clarifying questions) until consensus or explicit user override (with audit trail).
  8. Build and test occur in a disposable sandbox; artifacts are signed.
  9. Deployment proposal (Docker/Podman compose update or equivalent) is presented for final user vote.
  10. Upon approval, the skill activates in its own isolated microVM and becomes immediately usable.
- **Acceptance Criteria**:
  - All steps are fully git-backed and reversible (snapshots + git revert).
  - Average user time for end-to-end skill addition < 15 minutes.
  - 98%+ structured JSON success rate with schema validation on all Court outputs.
  - Every proposal, review comment, code change, and deployment is recorded in the append-only Merkle-tree audit log.

**User Story FR-001.2 – Skill Revocation & Reversion**
- As a user, I can revoke any skill instantly; the system performs a clean shutdown, removes the microVM, and reverts related git changes.

**User Story FR-001.3 – Self-Improvement Proposals**
- As the system, I can propose patches or new capabilities to myself; these flow through the same Court process (with mandatory human final approval).

### Epic FR-002: Core Agent Operation
**Description**: Daily interaction with existing skills.

**User Story FR-002.1 – Execute Authorized Actions**
- As a user, I chat with the agent (text or voice) → agent routes to appropriate skill microVM → action executes under least-privilege rules → result returned.
- High-risk actions (shell exec with write, file deletion, network outbound beyond allow-list) require explicit human confirmation.

**User Story FR-002.2 – Explain Decisions**
- As a startup or enterprise user, I can request “why” explanations for any past or proposed action, pulling from the audit log with clear reasoning traces.

### Epic FR-003: Configuration & Customization
**User Story FR-003.1 – User Profile & Strictness Levels**
- At onboarding and via Court-reviewed proposals, the system suggests strictness/speed profiles based on declared user type (hobbyist, startup, enterprise). Users can adjust later (relaxing controls requires human + CISO-level review).

**User Story FR-003.2 – Custom Policies & Personas (Enterprise)**
- Enterprise users can define custom reviewer personas, approval policies, and process flows while the core isolation guarantees remain immutable.

**Functional Requirements Summary (Non-Exhaustive)**
- All inter-component communication uses structured JSON with strict schema validation.
- Capability-based permissions: each skill receives only the exact OS/network/file capabilities it needs.
- Pluggable sandbox backends (Docker primary, Firecracker opt-in).
- Support for multiple concurrent skills with resource isolation.

## 7. Out-of-Scope / Non-Goals / Explicit Boundaries

To maintain focus and security invariants, the following are explicitly out of scope (and protected against accidental regression):

- **Never-allowed actions**:
  - Financial transactions of any kind.
  - Deletion or modification of user data without explicit, logged confirmation and backup.
  - Execution of unsigned or un-reviewed code outside the Court process.
  - Direct exposure of any secrets to LLM context, prompts, or logs.

- **Platform support**: Windows or macOS in the future (alongside the switch to Docker Sandboxes when Linux support lands); will be Linux-only (with Ubuntu focus) at first.
- **Cloud LLM fallback** by default (Ollama-only; optional pluggable support must preserve local-first guarantees and go through Court review).
- **Multi-user concurrent sessions** on a single host without separate isolation boundaries (future consideration).
- **High-availability / clustered deployment** (Sympozium-style Kubernetes fleet orchestration is complementary, not core).

**Immutable Design Rules** (“DO NOT CHANGE”):
- Per-skill microVM isolation must always enforce read-only filesystem (except explicit writable workspace), `cap-drop ALL`, private Docker daemon, and no shared memory between skills or with the host.
- Secrets handling must never place keys in LLM prompts, generated code, or persistent logs.

Any proposal that would violate these rules must be automatically rejected by the CISO persona with a logged explanation.

## 8. Non-Functional Requirements

### Security & Privacy (Elevated from v1.0)
- **Isolation**: Primary – Docker-based sandboxes with microVM semantics. Opt-in – Firecracker. Requirements: read-only FS (except designated workspace), `cap-drop ALL`, private Docker daemon per sandbox, no shared memory, network egress restricted by per-skill allow-list + proxy.
- **Secrets Management**: Injected at runtime only via dedicated network proxy (or SOPS/age with ephemeral mount). Keys never appear in prompts, code, logs, LLM context, or git history.
- **LLM Trust**: Ollama-only. Default ensemble: Llama-3.2-3B (fast reviewers), Mistral-Nemo (reasoning), Phi-3 (small audited). All model downloads hash-verified. Court outputs cross-verified by ≥2 models. No cloud APIs by default.
- **Audit & Tamper-Evidence**: Append-only Merkle-tree log covering every proposal, review, git change, deployment, and runtime action. Tamper-evident; queryable for “why” explanations.
- **Prompt Injection & Adversarial Defense**: All inputs sanitized; structured output enforcement; guardrail checks before high-risk tool calls.

### Performance & Reliability
- Baseline resource usage: <4 GB RAM for core system + sequential low-resource mode.
- Agent response time: <2 seconds (95th percentile) for simple queries; <15 minutes user time for full skill addition.
- Structured JSON parsing success rate: ≥98% with schema validation on all Court and tool outputs.
- Reliability: All mutations reversible; graceful degradation if a reviewer microVM fails (fallback to user escalation).

### Extensibility & Maintainability
- New reviewer personas configurable via Court-approved changes.
- Signed artifacts for all builds and deployments.
- Reproducible builds where feasible.

### Scalability Across User Types
- Hobbyists: Fast onboarding, default permissive-but-safe profile.
- Startups: Balanced speed with audit trails.
- Enterprises: Full customization of policies and personas while preserving core isolation invariants.

**Success Metrics**
- Zero isolation violations in any red-team tests (internal or external).
- Every skill addition completes end-to-end in <15 min (user time).
- Self-improvement: system successfully proposes and merges at least one patch via the Court.
- 100% of actions and reviews covered by the append-only audit log.
- Support for at least 10 production-grade skills with full reversibility and auditability by v1.0 public release.
