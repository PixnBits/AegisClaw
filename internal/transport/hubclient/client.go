// client.go — thin public surface for the hubclient package.
// The concrete implementation and all helpers live in types.go for the initial Phase 1 1.1a
// deliverable (keeps the foundational vsock/unix transport in one reviewable file).
//
// All symbols are re-exported at package level so importers write:
//     import "AegisClaw/internal/transport/hubclient"
//     c, err := hubclient.DialUnix(...)
//     resp, err := c.Register(...)
//
// See types.go for the full paranoid implementation and all spec citations.

package hubclient

// Re-export the primary constructors and interface so callers have the expected names
// in client.go (matching the approved plan file list).
//
// DialUnix and DialVsock are the two ways to obtain a Client.
// Client is the interface that the future 6-step agent loop, Memory VM, etc. will depend on
// (see agent-runtime.md §Key Interfaces and memory-vm.md §Communication Interface).

// Note: the actual func bodies are in types.go (same package). This file exists purely
// for the "separate client.go + types.go" structure requested in the execution plan.
