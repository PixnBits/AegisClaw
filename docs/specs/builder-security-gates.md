# Builder Security Gates Specification

## Overview
The Builder VM enforces multiple mandatory automated security gates before a skill can be deployed. These gates are non-negotiable.

## Security Gates (in execution order)

1. **SAST (Static Application Security Testing)**
   - Detects common vulnerability patterns, unsafe practices, and anti-patterns
   - Runs language-specific rule sets

2. **SCA (Software Composition Analysis)**
   - Scans dependencies for known vulnerabilities
   - Enforces license compliance policies

3. **Secrets / Sensitive Data Scanning**
   - Blocks **any potential secret or high-entropy value**
   - Uses multiple detection methods (entropy, patterns, context)
   - **Deliberately vague error messaging**: “Potential sensitive value detected – commit blocked for security reasons.”  
     (No specific details are given to avoid helping LLMs or attackers learn detection patterns.)

4. **Policy-as-Code**
   - Enforces custom security and architecture policies (using Rego or similar)
   - Examples: “Must route all outbound through Network Boundary”, “No direct credential usage”, etc.

5. **Composition & Health Validation**
   - Build artifact integrity
   - Basic smoke test
   - Prepares rollback to previous good version

## Failure Behavior

- Any gate failure → build marked **Failed**
- Detailed (but non-leaking) report sent to Court for review
- Previous stable version remains active (atomic deployment)
- Full audit trail entry created

## Related Documents
- `../builder-vm.md`
- `../governance-court.md`
- `../secrets-vault.md`

## Traceability
**Driven by:**
- Previous `internal/builder/securitygate` implementation
- Strong paranoid stance on secrets and vulnerabilities
- Need to avoid information leakage in error messages