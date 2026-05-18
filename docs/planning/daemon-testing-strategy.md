# Additional Testing Strategies

## Fuzz Testing

**Recommendation**: Yes, fuzz testing should be added for high-risk surfaces.

### High-Value Targets for Fuzzing

- Unix domain socket request parsing and handling
- Authorization / peer credential validation logic
- Any message deserialization over the control socket
- VM specification / configuration parsing
- Key distribution message formats

### How to Implement

- Use Go 1.18+ native fuzzing (`testing.F`)
- Start with the socket message handler and authorization paths
- Run fuzzing regularly in CI (or nightly)
- Seed with known-good and malformed inputs

Fuzzing is especially valuable here because the daemon accepts untrusted input over the Unix socket from CLI/TUI clients.

## Other Advanced Strategies to Consider

| Strategy              | Value for Host Daemon                  | Recommendation | Effort |
|-----------------------|----------------------------------------|----------------|--------|
| Fuzz Testing          | High (socket input, auth)              | Adopt          | Medium |
| Property-Based Testing| Medium (authorization rules, lifecycle)| Consider       | Low    |
| Chaos / Fault Injection | High (lifecycle containment)         | Consider       | Medium |
| Mutation Testing      | Medium (test quality)                  | Later          | High   |
| Formal Verification   | Very High but heavy                    | Not yet        | Very High |

## Updated Recommendation

Add **native Go fuzzing** targeting the Unix socket layer and authorization logic as a priority. This directly addresses the attack surface exposed to CLI/TUI clients.
