# AegisHub Specification

**Status:** Draft  
**Last Updated:** May 2026

## Purpose

AegisHub is the central, privileged message router for AegisClaw. **No microVM is allowed to communicate directly with another microVM.** All communication must be routed through AegisHub.

## Responsibilities

- Authenticate and route messages between microVMs
- Enforce strict ACL rules
- Reject and audit unauthorized communication attempts
- Support hot-reloading of ACL rules

## Traceability

**Driven by:**
- [../prd/runtime-architecture.md](../prd/runtime-architecture.md) — Mediator and isolation requirements
- [../prd/security-model.md](../prd/security-model.md) — Zero-trust and secret isolation rules

**See also:**
- [../architecture.md](../architecture.md)
- [../prd/glossary.md](../prd/glossary.md)

## ACL Management

ACL rules are defined in `config/acls.yaml` and follow a **deny-all** default policy.

Only two components may modify ACLs:
- The **Host Daemon** (during microVM lifecycle events)
- The **Store VM** (when the Governance Court approves new skills or architectural changes)

Changes to the ACL file trigger an automatic hot-reload.

## Message Format

```json
{
  "source":      "component-type:uuid",
  "destination": "component-type:uuid",
  "command":     "category.action",
  "payload":     { ... },
  "timestamp":   "2026-05-08T12:34:56Z",
  "signature":   "base64-encoded-signature"
}
```

**Command Naming:**
- Use dot notation: `memory.update`, `store.proposal.submit`, `court.review`
- For skills: `skill.<skill-name>.<action>` (e.g. `skill.discord.send_message`)

## Authentication

- Each microVM is issued a unique **Ed25519 keypair** by the Host Daemon at startup.
- The private key never leaves the microVM.
- The public key is registered with AegisHub during the handshake.
- All messages must be signed using the microVM’s private key.
- AegisHub verifies signatures against registered public keys.

## Handshake Sequence

1. MicroVM connects to AegisHub via vsock
2. MicroVM sends its component ID and public key
3. AegisHub validates the component ID and registers the key
4. AegisHub returns the ACL rules applicable to that component

## Error Handling

Unauthorized or malformed messages must return clear error codes:
- `ERR_UNAUTHORIZED`
- `ERR_INVALID_SIGNATURE`
- `ERR_ACL_VIOLATION`
- `ERR_INVALID_COMMAND`

All violations are logged to the Store VM’s audit trail.

## Test Requirements

The following behaviors must be enforced by automated tests:

- **Default Deny**: Any message sent between two microVMs with no matching ACL rule must be rejected
- **Signature Validation**: Messages with invalid or missing signatures must be rejected with `ERR_INVALID_SIGNATURE`
- **Source Authentication**: A microVM cannot impersonate another component ID
- **ACL Hot Reload**: Updating `config/acls.yaml` must take effect without restarting AegisHub
- **Handshake Enforcement**: A microVM that has not completed the handshake must not be allowed to send messages
- **Audit on Denial**: Every denied message must generate an audit log entry in the Store VM with the source, destination, command, and reason
- **No Direct Communication**: No two microVMs can establish a connection that bypasses AegisHub
- **Component ID Uniqueness**: Duplicate component IDs must not be allowed to register

