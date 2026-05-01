# egressproxy_test.go

## Purpose
Unit and integration tests for the `EgressProxy` type defined in `egressproxy.go`. Covers SNI extraction correctness, allowlist enforcement, and per-VM socket lifecycle management.

## Key Types / Functions
- **`buildTestClientHello(t, sni)`** – helper that constructs a minimal but spec-compliant TLS 1.2 ClientHello frame (including the 5-byte record header) for a given SNI hostname.
- **`writeSNIToConn(t, conn, sni)`** – convenience wrapper for writing a synthesized ClientHello to a `net.Conn`.
- **`startEchoTLSServer(t)`** – spins up a self-signed TLS listener on a random port for integration tests.
- **`TestEgressProxy_AllowedHost`** – validates that `isHostAllowed` accepts and denies hosts correctly.
- **`TestEgressProxy_DeniedHost`** – table-driven tests for edge cases (subdomain tricks, empty host, nil list).
- **`TestPeekSNI`** / **`TestPeekSNI_InvalidInput`** – verifies `peekSNI` against real synthesized frames and rejects non-TLS input.
- **`TestEgressProxy_StartStopForVM`** – confirms socket creation and teardown.
- **`TestEgressProxy_ProxyMode_SNIExtraction`** – round-trip test writing ClientHellos over `net.Pipe` and verifying extracted SNIs for real-world hostnames.

## System Role
Provides confidence that the egress-control security boundary cannot be bypassed via crafted SNIs or lifecycle races. All tests are self-contained and use in-process pipes or `t.TempDir()` sockets.

## Notable Dependencies
- `crypto/tls`, `crypto/ecdsa`, `crypto/x509` – TLS server setup for echo helper.
- `go.uber.org/zap/zaptest` – test-scoped logger.
