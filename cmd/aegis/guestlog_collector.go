package main

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/mdlayher/vsock"
	"github.com/sirupsen/logrus"

	"AegisClaw/internal/transport/hubclient"
)

// guestLogCollector accepts structured logs from guests over vsock (Phase 1).
// It is deliberately simple for the lessons-learned tree.
//
// Guests connect to vsock:2:18099 and send newline-delimited JSON.
// Each line should be a Record (see internal/guest/log/client.go).
//
// Logs are written to:
//   - <stateDir>/guest-logs.jsonl (all logs, with vm_id when present)
//   - Per-VM files when we can determine the ID: <stateDir>/<vmID>.guest.log
type guestLogCollector struct {
	stateDir string
	mu       sync.Mutex
	file     *os.File // central jsonl
	closed   bool
}

func newGuestLogCollector(stateDir string) (*guestLogCollector, error) {
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		return nil, err
	}

	f, err := os.OpenFile(filepath.Join(stateDir, "guest-logs.jsonl"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, err
	}

	c := &guestLogCollector{
		stateDir: stateDir,
		file:     f,
	}
	return c, nil
}

func (c *guestLogCollector) Start() {
	go func() {
		l, err := vsock.Listen(hubclient.LogVsockPort, nil)
		if err != nil {
			logrus.Warnf("guest log collector: failed to listen on vsock port %d: %v (Phase 1 observability disabled)", hubclient.LogVsockPort, err)
			return
		}
		defer l.Close()

		logrus.Infof("guest log collector listening on vsock port %d (Phase 1)", hubclient.LogVsockPort)

		for {
			conn, err := l.Accept()
			if err != nil {
				if c.isClosed() {
					return
				}
				continue
			}
			go c.handleConnection(conn)
		}
	}()
}

func (c *guestLogCollector) handleConnection(conn net.Conn) {
	defer conn.Close()

	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		// Try to parse so we can enrich and route
		var rec map[string]interface{}
		if err := json.Unmarshal(line, &rec); err != nil {
			// Still write the raw line so we don't lose data
			c.writeRaw(line)
			continue
		}

		// Ensure timestamp if missing
		if _, ok := rec["ts"]; !ok {
			rec["ts"] = time.Now().UTC().Format(time.RFC3339)
		}

		// Write enriched line to central log
		c.writeRecord(rec)

		// Also write to per-VM file when we have an ID
		if vmID, ok := rec["vm_id"].(string); ok && vmID != "" {
			c.writePerVM(vmID, rec)
		}
	}
}

func (c *guestLogCollector) writeRaw(line []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.file == nil {
		return
	}
	_, _ = c.file.Write(append(line, '\n'))
}

func (c *guestLogCollector) writeRecord(rec map[string]interface{}) {
	b, _ := json.Marshal(rec)
	c.writeRaw(b)
}

func (c *guestLogCollector) writePerVM(vmID string, rec map[string]interface{}) {
	path := filepath.Join(c.stateDir, vmID+".guest.log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return
	}
	defer f.Close()

	b, _ := json.Marshal(rec)
	_, _ = f.Write(append(b, '\n'))
}

func (c *guestLogCollector) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	if c.file != nil {
		return c.file.Close()
	}
	return nil
}

func (c *guestLogCollector) isClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

// getRecentGuestLogs returns recent lines from the per-VM guest log file (if any).
// Used by the vm.logs socket handler.
func getRecentGuestLogs(stateDir, vmID string, tailLines int) string {
	path := filepath.Join(stateDir, vmID+".guest.log")
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}

	lines := splitLines(string(data))
	if tailLines > 0 && len(lines) > tailLines {
		lines = lines[len(lines)-tailLines:]
	}
	return joinLines(lines)
}

func splitLines(s string) []string {
	// Simple split that preserves empty lines at the end if any
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

func joinLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	out := ""
	for _, l := range lines {
		out += l + "\n"
	}
	return out
}

// Convenience to start the collector from daemon startup (called from startDaemon).
var guestLogCollectorInstance *guestLogCollector

func startGuestLogCollector(stateDir string) {
	if stateDir == "" {
		stateDir = "/tmp/aegis"
	}
	c, err := newGuestLogCollector(stateDir)
	if err != nil {
		logrus.Warnf("failed to start guest log collector: %v", err)
		return
	}
	guestLogCollectorInstance = c
	c.Start()
}

func stopGuestLogCollector() {
	if guestLogCollectorInstance != nil {
		_ = guestLogCollectorInstance.Close()
	}
}