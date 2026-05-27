# Phase 4: Real Encrypted Secrets + Production Network Boundary

**Status:** Partially Started (stub Hub path exists)  
**Priority:** P1  
**Estimated Effort:** 2–3 weeks

## Goal
Replace file/dir/env secret loading with real encrypted blobs delivered from Store VM + proper zeroization.

## Key Specifications
- `docs/specs/secret-management.md`
- `docs/specs/network-boundary.md`

## Definition of Done
- [ ] Store VM can push encrypted secret blobs to Boundary via Hub
- [ ] Boundary decrypts, injects per-skill, and zeroizes after use
- [ ] Guest vsock client implemented in Firecracker images
- [ ] No file/dir/env fallback remains in production path
- [ ] Full audit trail for every secret access

## Detailed Tasks

### 4.1 Encrypted Blob Path (Week 1)
- Implement `BuildEncryptedSecretsUpdatePayload` in `internal/boundarycrypto`
- Add AES-256-GCM encryption + signing in Store VM
- Wire `secrets.push` Hub message from Store to Boundary

### 4.2 Decryption + Zeroization (Week 1–2)
- Implement decryption + zeroization in Boundary on receipt
- Update `injectSecretForHost` to use decrypted per-skill secrets only
- Add strict-mode enforcement (fail closed if decryption fails)

### 4.3 Guest vsock Client (Week 2)
- Add vsock client code to Firecracker guest images (or reference implementation)
- Update kernel cmdline to pass `aegis.egress_boundary` and `aegis.skill_id`
- Test end-to-end vsock egress with real secrets

### 4.4 Removal of Legacy Paths (Week 3)
- Remove file/dir/env secret loading from production Boundary code
- Keep only for dev/testing with clear warnings
- Update all docs and threat model

## Success Criteria
Secrets are delivered encrypted from Store, decrypted only inside Boundary, injected per-skill, and zeroized after use — with no legacy fallbacks in production.
