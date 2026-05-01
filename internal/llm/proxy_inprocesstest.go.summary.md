# proxy_inprocesstest.go

## Purpose
Exposes an in-process test shim on `OllamaProxy` that is compiled **only** under the `inprocesstest` build tag and must never appear in any production binary.

## Key Types / Functions
- **`OllamaProxy.InferDirect(vmID, req)`** – calls `handleRequest` directly, bypassing the vsock Unix-socket transport layer. Returns a `ProxyResponse` as if the request had arrived over the socket.

## System Role
Enables `InProcessSandboxLauncher` (in `internal/court`) to drive LLM inference in the same OS process as the test without starting a Firecracker VM. The full audit-write path inside `handleRequest` is preserved, so `llm.infer` entries still appear in the tamper-evident kernel log during in-process tests. This keeps security-sensitive code paths exercised in tests while avoiding VM overhead.

## Notable Dependencies
- `//go:build inprocesstest` build constraint – ensures strict binary exclusion.
- `OllamaProxy.handleRequest` (proxy.go) – the production code path being exercised.

## Important Warning
This file carries a prominent warning comment. It **must not** be included in release builds, default `go test ./...` runs, or any deployment artifact.
