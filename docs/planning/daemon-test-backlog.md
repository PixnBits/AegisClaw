# Host Daemon TCB — Test Backlog

Prioritized gaps derived from [docs/implementation-plan/03-daemon-minimal-tcb-refactor.md](../implementation-plan/03-daemon-minimal-tcb-refactor.md) (traceability matrix). Use this file to open focused PRs without losing context.

**Priority**: P0 = blocks a credible “secure default” claim; P1 = hardening completeness; P2 = observability and ergonomics.

| ID | Priority | Gap | Suggested test type | Suggested location / approach |
|----|----------|-----|---------------------|-------------------------------|
| DB-01 | P0 | Kill daemon process → all child microVMs terminated within bounded time | `integration` + subprocess | **Partial:** `terminateManagedHubAndStoreVMs`, `shutdownAllSandboxes` + unit tests in `cmd/aegisclaw/`; still need subprocess + stub/Firecracker ([06](../implementation-plan/06-sandbox-lifecycle-containment.md)) |
| DB-02 | P0 | Merkle **root signing loop** in daemon (interval, failure modes) | Integration or unit with clock interface | **Partial:** `internal/kernel/kernel_test.go` `TestKernel_MerkleAuditChainMultiEntries` (sequential `SignAndLog`); ticker-based interval still **gap** |
| DB-03 | P1 | **Idle memory** under 20 MB on reference Linux | Benchmark test (`testing.B`) or cgroup-scoped sample | `cmd/aegisclaw/` — document hardware baseline in test header |
| DB-04 | P1 | **Keypair distribution**: private key never appears in daemon logs or cross-VM APIs | Integration / fuzz of logging redaction | Cross `internal/vault`, sandbox bootstrap |
| DB-05 | P1 | **SO_PEERCRED** allow-list: reject unexpected UID/GID with stable audit | Unit + Linux integration | `internal/api/` — extend [server_peeruid_linux_test.go](../../internal/api/server_peeruid_linux_test.go); see [04](../implementation-plan/04-unix-socket-hardening.md) |
| DB-06 | P1 | **Rate limit and max message size** on Unix IPC | Unit / integration | `internal/api/server.go` handlers |
| DB-07 | P1 | **TCB handler regression table**: each removed/stub RPC returns documented error | Unit (table-driven) | **Partial:** `cli_api_contract_test.go` + `tcb_handler_denial_test.go` (`TestExtendedDaemonAPI_TCBStableDenialsFullTable`) for `registerExtendedDaemonAPI` denial paths |
| DB-08 | P2 | **No legacy path migration**: removed symbols never invoked from config load | Unit or static test | **Implemented (initial):** `load_migration_regression_test.go` + `legacy_migration_guard_test.go` — extend forbidden symbol list if new removals land |
| DB-09 | P2 | **Watchdog restart** path: simulate consecutive health failures → restart signal | Integration | Expand `lifecycle_integration_test.go` beyond struct-only checks |

When closing an item, update the **Status** column in Task 03’s matrix so this backlog and the matrix stay aligned.
