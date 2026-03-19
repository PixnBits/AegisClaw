# AegisClaw Core Implementation Patterns v2.1.3 (2026-03-12)
**Mandatory low-level wiring & safety patterns for the bootstrap phase**

This document is **NOT application logic**.  
It is a fixed, non-negotiable set of control-plane and safety boilerplate patterns that **MUST** appear (possibly slightly adapted for naming/context) in every generated `.go` file that touches:

- the TCP control channel (aegisclaw listener or client connections)
- audit log writing (host binary only)
- startup sequencing & concurrency model
- Docker compose.yaml generation
- registration handshake

These patterns exist to eliminate the exact classes of startup failures observed during real bootstrap iterations (March 2026).  
Any generated code that deviates from these patterns without an extremely strong, auditable justification **MUST** be rejected during sandbox vetting by aegisclaw.

Every place one of these patterns is used, the generated code **MUST** retain at minimum a comment of the form:

```go
// CONTROL_PLANE_PATTERN_v2.1.3: <short name of pattern>
// SECURITY_INVARIANT: references ARCHITECTURE.md §X.Y or PRD.md §Z.W
```

Later auditing can simply `grep -r "CONTROL_PLANE_PATTERN_v2.1.3" **/*.go` to verify compliance.

## 1. TCP Control Plane – Host Listener (aegisclaw binary only)

**Pattern – Host-side bind (MUST listen only on loopback)**

```go
package main

import (
    "net"
    "os"
)

func main() {
    port := os.Getenv("AegisClaw_CONTROL_PORT")
    if port == "" {
        port = "7124"
    }

    // CONTROL_PLANE_PATTERN_v2.1.3: Host TCP listener – loopback only
    // SECURITY_INVARIANT: ARCHITECTURE.md § Networking Architecture – 127.0.0.1 binding
    ln, err := net.Listen("tcp", "127.0.0.1:"+port)
    if err != nil {
        // panic + audit write before exit
        os.Exit(1)
    }
    defer ln.Close()

    // Accept loop with connection validation goes here
}
```

**Forbidden anti-patterns (LLM MUST NOT generate these):**
- `net.Listen("tcp", ":7124")` or `"0.0.0.0:7124"` inside the host binary
- Listening on any port inside a container skill

## 2. TCP Control Plane – Client-side Connection (message-hub, user-agent, llm-caller, ollama)

**Pattern – Retry + active scanner loop (prevents race & deadlock)**

```go
package main

import (
    "bufio"
    "net"
    "time"
)

func connectWithRetry() net.Conn {
    var conn net.Conn
    var err error

    // CONTROL_PLANE_PATTERN_v2.1.3: Client TCP retry – exponential backoff
    // SECURITY_INVARIANT: PRD.md §3.2 – startup race mitigation
    for attempt := 0; attempt < 30; attempt++ {
        conn, err = net.Dial("tcp", "host.internal:7124")
        if err == nil {
            break
        }
        time.Sleep(time.Duration(1<<uint(attempt)) * 500 * time.Millisecond)
    }
    if err != nil {
        // log + fatal (audit write happens in aegisclaw)
        os.Exit(1)
    }
    return conn
}

func main() {
    conn := connectWithRetry()
    defer conn.Close()

    // CONTROL_PLANE_PATTERN_v2.1.3: Active bufio.Scanner forever loop
    // SECURITY_INVARIANT: prevents all-goroutines-asleep deadlock
    scanner := bufio.NewScanner(conn)
    for scanner.Scan() {
        line := scanner.Text()
        // parse JSON, handle message, possibly write reply
        _ = line // placeholder
    }
}
```

**Forbidden anti-patterns:**
- Single unconditional `net.Dial` without retry
- `select {}` or empty main goroutine after dial
- `log.Println` loop with no scanner/reader

## 3. Audit Log – Host Binary Only (path resolution & atomic append)

**Pattern – Executable-relative + safe open**

```go
import (
    "crypto/sha256"
    "encoding/json"
    "os"
    "path/filepath"
)

var previousHash string // loaded from last line or "" on first write

func appendAudit(entry map[string]interface{}) error {
    execPath, err := os.Executable()
    if err != nil {
        return err
    }
    base := filepath.Dir(execPath)
    shared := filepath.Join(base, "..", "shared")
    auditDir := filepath.Join(shared, "audit")
    auditPath := filepath.Join(auditDir, "aegisclaw.log")

    // CONTROL_PLANE_PATTERN_v2.1.3: Audit path – executable-relative + mkdir
    // SECURITY_INVARIANT: ARCHITECTURE.md § Auditing & Observability
    if err := os.MkdirAll(auditDir, 0755); err != nil {
        return err
    }

    f, err := os.OpenFile(auditPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
    if err != nil {
        return err
    }
    defer f.Close()

    entry["previous_hash"] = previousHash
    data, _ := json.Marshal(entry)
    hash := sha256.Sum256(data)
    previousHash = "sha256:" + hex.EncodeToString(hash[:]) // update for next

    _, err = f.Write(append(data, '\n'))
    return err
}
```

## 4. Initial compose.yaml – Core Skills Template Snippet

**Pattern – All four core skills + healthcheck ordering**

```yaml
# CONTROL_PLANE_PATTERN_v2.1.3: Initial compose.yaml – four core skills
# SECURITY_INVARIANT: PRD.md §3.3 – bootstrap ordering & health
services:
  message-hub:
    # ... image, build, etc.
    network_mode: aegisclaw-net
    extra_hosts:
      - "host.internal:host-gateway"
    healthcheck:
      test: ["CMD", "nc", "-z", "host.internal", "7124"]
      interval: 5s
      timeout: 3s
      retries: 10
    # no depends_on here – first to start

  llm-caller:
    depends_on:
      message-hub:
        condition: service_healthy

  user-agent:
    depends_on:
      message-hub:
        condition: service_healthy

  ollama:
    depends_on:
      message-hub:
        condition: service_healthy
    # mem_limit: 16g, shm_size: 1g exception
```

## Compliance Checklist (LLM MUST verify before writing files)

- [ ] message-hub is TCP client only + uses retry dial + active scanner loop
- [ ] No skill listens on 127.0.0.1:7124 or any port
- [ ] Audit path in aegisclaw uses executable-relative + MkdirAll + 0600
- [ ] compose.yaml contains all four core skills + healthcheck on message-hub
- [ ] Every TCP/client file has active bufio.Scanner in main loop
- [ ] All used patterns include CONTROL_PLANE_PATTERN_v2.1.3 comment

Any generated file that fails this checklist or includes a forbidden anti-pattern **MUST** cause aegisclaw registration rejection (future vetting rule).

This document is the canonical reference for bootstrap reliability.  
It complements — and does not replace — ARCHITECTURE.md and PRD.md.
