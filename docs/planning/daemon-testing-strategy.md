# Additional High-Value Testing Techniques

## Recommended Additions

### 1. Chaos / Fault Injection Testing
**Why it matters**: Directly validates **Lifecycle Containment**.
- Kill the daemon process and verify all managed VMs are terminated.
- Simulate AegisHub or Store VM crashes and test watchdog behavior.
- Use tools like `chaos-mesh`, `litmus`, or simple process killing in integration tests.

### 2. Contract / Interface Testing
**Why it matters**: Protects the seams we created (`AegisHubClient`, `StoreVM`, `SandboxBackend`).
- Test that clients and backends adhere to their interfaces.
- Use interface mocks + behavior verification.
- Prevents regressions when we evolve the daemon ↔ VM communication.

### 3. Golden File / Snapshot Testing
**Why it matters**: Good for verifying hardening state.
- Snapshot expected socket permissions, capability sets, or seccomp filter state.
- Makes it obvious when hardening behavior changes unintentionally.

### 4. Threat-Model-Driven Testing
**Why it matters**: Ensures we test what actually matters.
- Maintain a lightweight threat model for the Host Daemon.
- Derive test cases from STRIDE or attack trees (especially around the Unix socket and VM lifecycle).
- Prioritize tests based on real attack scenarios rather than just code coverage.

### 5. Dependency & Supply Chain Testing
- Regular `govulncheck` runs.
- Dependency pinning + SBOM generation (future).
- Verify minimal dependency set is maintained.

These techniques complement fuzzing and static analysis well for a security-sensitive component like the Host Daemon.