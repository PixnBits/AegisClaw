# Host Daemon TCB — Test Backlog

Prioritized gaps derived from [docs/implementation-plan/03-daemon-minimal-tcb-refactor.md](../implementation-plan/03-daemon-minimal-tcb-refactor.md) (traceability matrix). Use this file to open focused PRs without losing context.

**Priority**: P0 = blocks a credible “secure default” claim; P1 = hardening completeness; P2 = observability and ergonomics.

**Note (Phase 5 – 04-unix-socket-hardening branch, May 2026)**: DB-05 and DB-06 are now **Implemented** under `feature/04-unix-socket-hardening`. Full `SO_PEERCRED` allow-list (root reject + DefaultUnixPeerAllow), rate/size enforcement, capability tokens, correlation ID audit logging, and expanded tests landed. See [04-unix-socket-hardening.md](../implementation-plan/04-unix-socket-hardening.md) for full details and Phase 5 closeout.

| ID | Priority | Gap | Suggested test type | Suggested location / approach |
|----|----------|-----|---------------------|-------------------------------|
| DB-01 | P0 | Kill daemon process → all child microVMs terminated within bounded time | `integration` + subprocess | **Partial:** `terminateManagedHubAndStoreVMs`, `shutdownAllSandboxes` + unit tests; **subprocess slice:** `daemon_shutdown_sigterm_e2e_test.go` (SIGTERM → Stop+Delete); real Firecracker still [06](../implementation-plan/06-sandbox-lifecycle-containment.md) |
| DB-02 | P0 | Merkle **root signing loop** in daemon (interval, failure modes) | Integration or unit with clock interface | **Partial:** `kernel_test.go` sequential `SignAndLog` + **`RunPeriodicAuditSync`** early-return / cancel tests; optional `AEGISCLAW_AUDIT_SYNC_INTERVAL` in `start.go` |
| DB-03 | P1 | **Idle memory** under 20 MB on reference Linux | Benchmark test (`testing.B`) or cgroup-scoped sample | **Scaffold:** `cmd/aegisclaw/idle_memory_linux_bench_test.go` (`BenchmarkProcVmRSS_DB03`) — manual baseline against idle daemon |
| DB-04 | P1 | **Keypair distribution**: private key never appears in daemon logs or cross-VM APIs | Integration / fuzz of logging redaction | **Partial:** `internal/kernel/kernel_test.go` `TestKernelInit_LogsDoNotContainRawPrivateKeyMaterial`; extend to vault / API surfaces as needed |
| DB-05 | P1 | **SO_PEERCRED** allow-list: reject unexpected UID/GID with stable audit | Unit + Linux integration | **Implemented (04 branch)**: `internal/api/server_unix_policy_linux_test.go` (root reject + DefaultUnixPeerAllow + correlation audit); wired in `server.go`. Full closure complete. |
| DB-06 | P1 | **Rate limit and max message size** on Unix IPC | Unit / integration | **Implemented (04 branch)**: `internal/api/server_unix_policy_linux_test.go` (413 / 429 + rate tests); defaults + per-PID readiness in `server.go`. Full closure complete. |
| DB-07 | P1 | **TCB handler regression table**: each removed/stub RPC returns documented error | Unit (table-driven) | **Partial:** `cli_api_contract_test.go` + `tcb_handler_denial_test.go` (`TestExtendedDaemonAPI_TCBStableDenialsFullTable`) for `registerExtendedDaemonAPI` denial paths |
| DB-08 | P2 | **No legacy path migration**: removed symbols never invoked from config load | Unit or static test | **Implemented (initial):** `load_migration_regression_test.go` + `legacy_migration_guard_test.go` — extend forbidden symbol list if new removals land |
| DB-09 | P2 | **Watchdog restart** path: simulate consecutive health failures → restart signal | Integration | **Partial:** `lifecycle_integration_test.go` exercises `OnHealthCheckFailed` / `ResetHealthFailures`; full restart signal wiring still manual / higher tier |

When closing an item, update the **Status** column in Task 03’s matrix so this backlog and the matrix stay aligned.