# Security Model

AegisClaw is built on a **paranoid-by-design** philosophy. We assume all input from the outside world is malicious until proven otherwise.

## Core Assumptions

- The local hardware and host operating system are trusted
- All network input, LLM output, generated code, and user-provided skills are **hostile by default**
- The primary goal is to contain compromise even if a skill or LLM response is fully malicious

## Core Principle

**Every component boundary is a security boundary.**

## Isolation Strategy

- Every component that processes untrusted data runs in its own **Firecracker microVM** with:
  - Read-only root filesystem
  - `CAP_DROP=ALL`
  - Strict seccomp filters
  - Resource limits (CPU, memory, vsock rate limiting)
  - No network access unless explicitly granted through AegisHub

- All microVMs are launched using **Firecracker's jailer** with separate namespaces and cgroups
- All rootfs and kernel images are cryptographically signed and verified before launch

- The host runs only a **minimal trusted daemon** (see runtime-architecture.md)

## Communication & Mediation

All inter-component communication is mediated by **AegisHub**, a minimal privileged router that enforces strict ACLs. No two components can communicate directly.

## Key Security Guarantees

- Secrets are never injected into prompts, logs, or model context
- No component can access another component’s data without explicit ACL approval
- All persistent state is protected by a tamper-evident Merkle tree audit log
- Every action taken by any component is cryptographically logged
- The Governance Court must approve **every** code change before it can be deployed

## Supply Chain Hardening

- All base images, kernels, and Ollama models are pinned to specific hashes
- No unverified third-party code is ever executed

