# cmd/aegishub/vsock_linux.go

## Purpose
Implements a raw AF_VSOCK server socket listener and connection wrapper for Linux, used by AegisHub to accept connections from the host daemon inside a Firecracker microVM.

## Why Custom Implementation
Go's `net.FileListener` cannot handle AF_VSOCK because `getsockname` returns an address family the net package does not understand, so the fd must be wrapped manually.

## Key Types / Functions
- `listenAFVsock(port uint32) (net.Listener, error)` — opens an AF_VSOCK `SOCK_STREAM` socket bound to `VMADDR_CID_ANY:port` and wraps it in `vsockListener`.
- `vsockListener` — implements `net.Listener` over a raw fd. `Accept()` calls `unix.Accept` and wraps the new fd in a `vsockConn`.
- `vsockAddr` — implements `net.Addr`; `Network()` returns `"vsock"`, `String()` returns `"vsock://:<port>"`.
- `vsockConn` — wraps an `os.File` over an AF_VSOCK fd as a `net.Conn`; delegates all I/O to `os.File` methods (including `SetDeadline`).

## System Fit
Provides the AF_VSOCK transport layer for `main.go`'s `listenVsock`. Only compiled on Linux (no build tag needed since AegisHub targets Linux microVMs exclusively).

## Notable Dependencies
- `golang.org/x/sys/unix` — `Socket`, `Bind`, `Listen`, `Accept`, `SockaddrVM`, `VMADDR_CID_ANY`
- `os` — `os.NewFile` for wrapping raw fds
