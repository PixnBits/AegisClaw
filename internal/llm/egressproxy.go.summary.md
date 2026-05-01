# egressproxy.go

## Purpose
Implements `EgressProxy`, which provides outbound HTTPS/WSS tunneling for skill VMs running inside Firecracker sandboxes. It listens on per-VM Unix domain socket paths (`<vsock_path>_1026`) and enforces a per-skill FQDN allowlist before forwarding TCP traffic.

## Key Types / Functions
- **`EgressProxy`** – core struct holding per-VM listeners and FQDN allowlist policies.
- **`NewEgressProxy(logger)`** – constructor.
- **`StartForVM(vmID, vsockPath, allowedHosts)`** – binds the UDS socket for a VM and starts a goroutine to serve connections.
- **`StopForVM(vmID)`** / **`Stop()`** – graceful shutdown for one or all VMs.
- **`handleConn(vmID, conn)`** – reads the TLS ClientHello, validates the SNI, dials the real upstream on port 443, and splices bidirectionally — no TLS termination.
- **`peekSNI(conn)`** – reads the first TLS record and parses the SNI extension without consuming the bytes (returns raw bytes for upstream replay).
- **`extractSNIFromClientHello(data)`** – parses ClientHello binary structure per RFC 5246.
- **`isHostAllowed(host, allowed)`** – case-insensitive exact-match check against the allowlist.

## System Role
Part of the sandbox egress-control layer. Operates alongside `OllamaProxy` (port 1025). Every outbound HTTPS connection from a skill VM must pass through this proxy; any hostname not in the per-VM allowlist is dropped and audit-logged.

## Notable Dependencies
- `net`, `io` – raw socket I/O and bidirectional splice.
- `encoding/binary` – manual TLS record parsing.
- `go.uber.org/zap` – structured audit logging.
