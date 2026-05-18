# Phase 3.6: Migration Table & Attack Surface Verification

## Responsibility Migration Table

| Responsibility                    | Before Phase 3                  | After Phase 3                          | Owner Now      |
|-----------------------------------|---------------------------------|----------------------------------------|----------------|
| Chat orchestration & tool dispatch| Daemon (heavy)                  | Thin proxy → AegisHub                  | AegisHub       |
| Session management                | Daemon                          | Proxied to AegisHub                    | AegisHub       |
| Worker coordination               | Partial in Daemon               | Proxied                                | AegisHub       |
| EventBus / Approvals / Timers     | Direct in Daemon                | Proxied                                | AegisHub       |
| Tool Registry                     | Heavy in Daemon                 | Seam established (moving to AegisHub)  | AegisHub (in progress) |
| AegisHub Launch & Monitoring      | Basic                           | Hardened (Firecracker + health + restart) | Daemon (watchdog) |
| Persistent Store ownership      | Partially in Daemon             | Fully moved to Store VM                | Store VM       |
| Core TCB (VM lifecycle, socket, keys, Merkle) | Always | Still in Daemon (minimal)         | Host Daemon    |

## Attack Surface Reduction Analysis

**Has the daemon's attack surface meaningfully shrunk?** → **Yes.**

### Before Phase 3
- Large amount of control-plane logic executed directly in the privileged daemon:
  - Chat message handling & tool execution
  - Session state and routing
  - Worker spawning and tracking
  - Event timers, approvals, and signals
- These paths involved complex business logic, external calls, and state management inside the TCB.

### After Phase 3
- Most of the above logic has been moved behind thin proxy handlers that forward requests to AegisHub.
- The daemon now primarily acts as:
  - A launcher and watchdog for AegisHub and Store VM
  - A privileged Unix socket server
  - The root of trust for keys and Merkle signing
- The volume of complex, high-risk code paths directly in the daemon has been significantly reduced.

### Quantitative Feel
- Dozens of handler implementations converted from direct logic to simple forwarding.
- EventBus, Chat, Sessions, and Worker coordination largely removed from daemon execution path.
- AegisHub now owns the majority of inter-component coordination.

### Remaining Attack Surface (Intentional)
- VM lifecycle management (still requires host privileges)
- Unix socket + authorization
- Cryptographic operations (key distribution, Merkle signing)

These are the **minimal necessary** responsibilities for a host daemon in this architecture.

**Conclusion**: Phase 3 has achieved a meaningful and visible reduction in the Host Daemon’s attack surface while improving AegisHub’s reliability and autonomy.