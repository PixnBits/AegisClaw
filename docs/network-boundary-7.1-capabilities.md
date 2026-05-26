# Network Boundary 7.1 Capabilities

**Status**: Stub-complete with clear production path (as of the current milestone in the Grok Build Execution Plan).

This document summarizes what a running Network Boundary can demonstrably do today, with honest disclaimers about what remains stub. It is intended as a public-facing closure artifact for Phase 7.1.

## What is Now Demonstrably Real

### Zero-Trust Egress Enforcement
- VMs started with `EgressViaBoundary=true` receive **no hypervisor network interfaces**.
- All outbound traffic is forced through the Network Boundary (production path: vsock; development fallback: TCP).
- The boundary participates in the full policy chain (routing, rate limits, ExtAuthz, secret injection, audit).

### Per-Skill Network Policy
- Global and per-skill allowlists loaded from protected files (`AEGIS_SKILL_SECRETS_DIR/*.secret`, `AEGIS_SKILL_NETWORK_RULES_DIR/*.domains`), single files, or environment variables.
- Strict mode (`AEGIS_BOUNDARY_STRICT=1`) enforces secure permissions and required configuration at startup (fail-closed).
- Policy is enforced at multiple layers:
  - Go direct egress paths (`/egress` and legacy `network.request`).
  - Envoy dynamic routing + ExtAuthz (header-based per-skill clusters, rate limiting, circuit breakers).

### Real Secrets via Hub (Major TCB Achievement)
The "real secrets" story is now end-to-end functional in stub form:

- **Loading**: `loadSkillSecrets()` supports file, directory, and env sources with best-effort 0600 checks and strict-mode enforcement.
- **Dynamic Delivery** (`secrets.update` over the Hub):
  - Full cryptographic signature verification (ed25519).
  - Canonical data construction + timestamp freshness checks.
  - Bounded nonce-based replay protection.
  - Support for both full replacement and incremental operations (add/replace/remove per skill).
  - Early defensive rate limiting on the privileged path.
  - Extensive SECURITY logging and audit events.
- **Mutual Authentication**:
  - Store signs updates → boundary verifies (key preferably delivered during registration).
  - Boundary signs reconciliation responses (`secrets.get`) → Store can verify (using `signer_pubkey` included in the response + exported `VerifyBoundarySignedResponse` helper).
- **Reconciliation & Observability**:
  - `secrets.get` / `secrets.request`: Safe metadata only (skill list + count).
  - `secrets.status`: Health metrics (count, last update, nonce cache size, etc.).
- **Trust Integration**: Store signer public key can be delivered via the authenticated "register" exchange.

### Integration & Observability
- Live secret store shared between direct paths and ExtAuthz server.
- `boundarycrypto` package contains the reusable core (canonicalization, timestamp checks, `NonceCache`, `RateLimiter`, verification helpers).
- All sensitive paths fail closed on signature failure, replay, rate limit, unhealthy boundary, or weak configuration.
- Strong audit trail for all security-relevant events.

## Honest Stub Limitations

- Secrets remain in-memory (no production-grade zeroization or encrypted blob handling from a real Store VM yet).
- Signature verification is real when a public key is configured, but the full certificate/attestation model and Store-side signing ceremony are future work.
- Rate limiting and nonce cache are pragmatic and bounded (not distributed or hardened for multi-boundary deployments).
- No real production Store VM or encrypted secret blobs — all paths are explicitly stubbed with detailed comments on the intended real behavior.
- Guest-side vsock client implementation and deeper Firecracker-level policy enforcement remain future.
- The package refactor and test coverage are good but not yet exhaustive.

## Key Integration Points

- **Registration**: Boundary sends its public key; Hub/Store can return the expected signer public key.
- **Secrets Path**: `secrets.update` (push), `secrets.get` (pull/reconcile), `secrets.status` (health).
- **Egress**: Direct Go paths + Envoy/ExtAuthz (with per-skill routing and secret injection).
- **Transport**: vsock (production) and TCP (dev) both participate in the full policy chain.

## Future Evolution Path (Documented in Code)

- Encrypted per-skill secret blobs from the Store VM.
- Full xDS-driven Envoy configuration.
- Real guest vsock client in Firecracker images.
- Certificate-based trust instead of raw public key configuration.
- Reuse of the signed message + verification + rate limit + reconciliation patterns for other Hub flows (policy, audit, etc.).

## Summary

As of this milestone, the Network Boundary provides a **demonstrably functional, cryptographically protected, mutually authenticated, per-skill secrets and egress control plane** via the Hub. The "real secrets via Hub" capability — one of the largest remaining TCB items required for safe autonomy — is now end-to-end stub-complete with clear, honest documentation of the production path.

This is a major step toward a trustworthy zero-trust foundation for the rest of Phase 7.

---

*See the main execution plan (`grok-build-execution-plan.md`) for the detailed 7.1 Closure Status, prioritized remaining work, and milestone definition.*
