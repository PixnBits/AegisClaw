# Network Boundary VM Specification

**Status:** Draft  
**Last Updated:** May 2026

## Purpose

The Network Boundary VM is the **only** component in the system authorized to hold secrets or make outbound network requests. All outbound traffic from any agent or skill must pass through this VM.

## Core Security Rule

Secrets must never exist in plaintext outside of this VM.

## Responsibilities

- Run Envoy as the outbound proxy for the entire system
- Dynamically load and enforce `network-access.yaml` declarations
- Inject secrets into requests without exposing them to skills or agents
- Log all outbound network activity to the audit trail

## Architecture

- Single dedicated Firecracker microVM
- Runs Envoy + a small Go control plane
- Control plane reads approved network declarations from the Store VM

## Internal Isolation Strategy

- Secrets are stored in isolated memory regions per skill
- Envoy routes are strictly partitioned by skill
- Rate limiting and circuit breakers are applied per skill

## Secret Loading Process

1. Store VM sends an encrypted blob containing only the secrets needed by currently active skills
2. Control plane decrypts the blob inside the VM
3. Secrets are loaded into isolated memory regions
4. The original encrypted blob is immediately wiped from memory

## Test Requirements

The following behaviors must be enforced by automated tests:

- **Secret Containment**: No secret can ever be logged, written to disk, or transmitted outside this VM
- **Network Containment**: Any attempt to make a network request from any microVM other than the Network Boundary VM must be blocked
- **Declaration Enforcement**: A skill can only contact hosts explicitly declared in its `network-access.yaml`
- **Unauthorized Request Rejection**: Requests to undeclared hosts must be rejected with a clear audit log entry
- **Secret Isolation**: Compromise of one skill's credentials must not expose other skills' secrets
- **Crash Safety**: If the Network Boundary VM crashes or fails to start, all outbound networking must be blocked
- **Audit Completeness**: Every outbound request must generate an audit log entry containing skill ID, destination, and result

## Extensibility

The design must make it easy to add support for WebSockets, gRPC, and custom signing logic in the future.
