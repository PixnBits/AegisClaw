# Threat Model Specification

## Philosophy
AegisClaw assumes **hostile agents, compromised components, and malicious users**. We design for the worst case at every layer ("paranoid by default").

## Core Assumptions
- The Host Daemon is the only trusted component on the machine.
- All microVMs can be fully compromised.
- The user (or their local network) can be hostile.
- Supply-chain attacks on dependencies and skills are expected.
- LLM outputs and tool results are treated as malicious until proven otherwise.

## Key Assets to Protect
1. User secrets (API keys, tokens, credentials)
2. Host machine integrity and resources
3. Audit log / tamper-evident history
4. Skill code and execution environment
5. User data in Memory / Store VM

## Threat Categories & Mitigations

### 1. Compromised Agent Runtime VM
- **Threat**: Malicious code execution, data exfiltration, privilege escalation
- **Mitigations**: Firecracker isolation, no direct network, secrets never loaded, mandatory Network Boundary, strict resource limits

### 2. Malicious Skill (Supply Chain)
- **Threat**: Backdoored or vulnerable skill approved by Court
- **Mitigations**: Builder Security Gates (SAST, SCA, secrets scanning, policy-as-code), Court review by 7 personas, SBOM generation, rollback capability

### 3. Compromised Network Boundary VM
- **Threat**: Secrets leak, unrestricted outbound
- **Mitigations**: Secrets injected per-call only, strict `allowed_domains` per-VM, crash = block all outbound, audit every request

### 4. Host Daemon Compromise
- **Threat**: Game over (worst case)
- **Mitigations**: Extremely minimal TCB, static binary, runs with least privileges, Safe Mode as kill switch, clear watchdog behavior

### 5. Web Portal / Browser Attacks
- **Threat**: XSS, malicious input, session hijacking
- **Mitigations**: All traffic proxied through Host Daemon, no secrets in UI, input treated as hostile, sandboxed Web Portal VM

### 6. User / Local Machine Attacks
- **Threat**: Malicious user trying to bypass Court or extract secrets
- **Mitigations**: Secrets only via CLI (interactive or stdin), no chat-based secret entry, audit everything, Safe Mode available

### 7. Persistence / Reboot Attacks
- **Threat**: Malicious state surviving restarts
- **Mitigations**: Ephemeral Agent Runtimes, tamper-evident Store VM, validation on boot

## Open Questions / Future Work
- Multi-user isolation model
- Remote attestation of microVMs
- Hardware root of trust (TPM) integration
- Formal verification of critical paths

## Related Documents
- `../security-model.md` (PRD)
- `../builder-security-gates.md`
- `../secrets-vault.md`
- `../safe-mode.md`
- `../host-daemon.md`

## Traceability
**Driven by:**
- "Paranoid by design" principle
- Lessons learned from v1
- Real-world agent supply chain risks