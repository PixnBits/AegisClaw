# 13 - Network Boundary Initial Implementation

## Goal
Implement the initial version of the Network Boundary VM as the single point of outbound network access.

## Acceptance Criteria
- Network Boundary VM starts successfully
- Can proxy basic outbound requests through allowed domains
- Respects per-VM allowed_domains from config
- Crashes gracefully and blocks all outbound when compromised
- All outbound traffic is audited

## Relevant Specs
- `network-boundary.md`
- `configuration-management.md`
- `secrets-vault.md`

## Test
- `aegis vm list` shows network-boundary
- Basic outbound test with allowed domain succeeds
- Blocked domain is rejected