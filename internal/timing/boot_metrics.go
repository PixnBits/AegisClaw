// Package timing provides lightweight, high-resolution boot phase instrumentation
// for microVM guests (and host-side callers) when AEGIS_BOOT_TIMING=1.
//
// It is deliberately stdlib-only (no logrus) so thin guest binaries (agent, memory,
// court-*, etc.) can import it with zero extra surface or dependencies.
//
// Usage (guests):
//
//	import "AegisClaw/internal/timing"
//
//	func runAgent(...) {
//	    timing.RecordPhase("main_entry")
//	    ... after LoadDistributedVMKey ...
//	    timing.RecordPhase("key_loaded")
//	    client, _ := dial...
//	    timing.RecordPhase("hub_dialed")
//	    _, _ = client.Register(...)
//	    timing.RecordPhase("register_complete")
//	    timing.WriteComponentReadySentinel()
//	    ... prepare loop ...
//	    timing.RecordPhase("message_loop_ready")
//	    for { ... Receive ... }
//	}
//
// All emissions (human + machine BOOT_TIMING lines) go to stdout, which for
// Firecracker guests is captured to fc-*-console.log by the backend. The machine
// lines are easy to parse for `aegis vm boot-metrics` and scripts.
//
// Host side can also call RecordPhase (it will be per-process; orchestrator
// prefers its own per-VM maps + backend.BootPhases for precision across VMs).
package timing

import (
	"fmt"
	"os"
	"sync"
	"time"

	"AegisClaw/internal/bootargs"
)

var (
	enabledOnce sync.Once
	enabled     bool

	phasesMu sync.Mutex
	phases   = make(map[string]int64) // phase -> unix nano ts
)

// IsEnabled reports whether boot timing collection + emission is active.
// It is true if AEGIS_BOOT_TIMING=1 (env, set by host or exported by /init)
// or aegis.boot_timing=1 appears on /proc/cmdline (orchestrator injects the
// flag for all VMs when the daemon sees the env).
func IsEnabled() bool {
	enabledOnce.Do(func() {
		enabled = bootargs.BootTimingEnabled()
	})
	return enabled
}

// RecordPhase records a named boot phase with nanosecond wall timestamp and,
// when enabled, emits both a human-readable line and the canonical machine
// line consumed by analysis tools:
//
//   BOOT_TIMING: phase=register_complete ts=1720123456795123456 duration_ms=6023
//
// duration_ms is computed relative to the first "main_entry" (or "main") seen.
// Safe and cheap to call when disabled (early return, no allocs in hot path).
func RecordPhase(name string) {
	if !IsEnabled() {
		return
	}
	ts := time.Now().UnixNano()

	phasesMu.Lock()
	phases[name] = ts
	first := int64(0)
	if t0, ok := phases["main_entry"]; ok && t0 > 0 {
		first = t0
	} else if t0, ok := phases["main"]; ok && t0 > 0 {
		first = t0
	}
	phasesMu.Unlock()

	durMS := int64(0)
	if first > 0 {
		durMS = (ts - first) / int64(time.Millisecond)
	}

	// Machine-readable (primary for console.log parsing + GetVMBootMetrics).
	fmt.Printf("BOOT_TIMING: phase=%s ts=%d duration_ms=%d\n", name, ts, durMS)

	// Human-readable companion (appears in fc-*-console.log and is useful
	// when just tailing during dev; mirrors the style of existing agent prints).
	fmt.Printf("=== BOOT PHASE %s (cumul %d ms)\n", name, durMS)
}

// GetBootReport returns a copy of all recorded phases (name -> unix nano ts).
// Useful for a guest to dump a final summary or for tests.
func GetBootReport() map[string]int64 {
	phasesMu.Lock()
	defer phasesMu.Unlock()
	cp := make(map[string]int64, len(phases))
	for k, v := range phases {
		cp[k] = v
	}
	return cp
}

// WriteComponentReadySentinel writes /tmp/aegis-component-ready containing the
// timestamp of successful hub registration (the moment the component is truly
// ready to handle chat / requests).
//
// This is the "true application readiness" signal (vs. just "process running"
// or "Firecracker socket ready"). The orchestrator / portal-bridge can poll
// for the file (via future vsock fs access or guest log) instead of guessing
// from StartVM return + sleep.
//
// Also emits a BOOT_TIMING line.
func WriteComponentReadySentinel() {
	if !IsEnabled() {
		return
	}
	ts := time.Now().UnixNano()
	content := fmt.Sprintf("ready_ts=%d\nstatus=component_ready_for_chat\n", ts)
	_ = os.WriteFile("/tmp/aegis-component-ready", []byte(content), 0644)

	fmt.Printf("BOOT_TIMING: phase=ready_sentinel_written ts=%d duration_ms=0\n", ts)
	fmt.Println("=== aegis-component-ready sentinel written (/tmp) — true ready signal for orchestrator/bridge polling ===")
}

// ParseBootTimings scans console log content (or any text containing the lines)
// for "BOOT_TIMING: ..." records and returns a map of phase -> duration (as
// time.Duration, computed from the duration_ms field or ts deltas).
//
// It tolerates mixed guest/host output and missing fields. Used by
// GetVMBootMetrics and the analysis script/CLI.
func ParseBootTimings(data string) map[string]time.Duration {
	out := make(map[string]time.Duration)
	if data == "" {
		return out
	}
	lines := splitLines(data)
	var base int64
	for _, line := range lines {
		if !contains(line, "BOOT_TIMING:") {
			continue
		}
		phase := extractKV(line, "phase=")
		tsStr := extractKV(line, "ts=")
		durStr := extractKV(line, "duration_ms=")
		if phase == "" {
			continue
		}
		if durStr != "" {
			if ms := parseInt64(durStr); ms > 0 {
				out["guest/"+phase] = time.Duration(ms) * time.Millisecond
			}
		} else if tsStr != "" {
			if ts := parseInt64(tsStr); ts > 0 {
				if base == 0 {
					base = ts
				}
				out["guest/"+phase] = time.Duration(ts-base) * time.Nanosecond
			}
		}
	}
	return out
}

// --- tiny helpers (avoid regexp / extra imports in this tiny pkg) ---

func splitLines(s string) []string {
	// handle \n and \r\n simply
	var res []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			res = append(res, s[start:i])
			start = i + 1
			if i+1 < len(s) && s[i+1] == '\r' {
				i++
				start = i + 1
			}
		}
	}
	if start < len(s) {
		res = append(res, s[start:])
	}
	return res
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) > 0 && index(s, sub) >= 0)
}

func index(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func extractKV(line, prefix string) string {
	i := index(line, prefix)
	if i < 0 {
		return ""
	}
	val := line[i+len(prefix):]
	// value ends at space or end
	for j := 0; j < len(val); j++ {
		if val[j] == ' ' || val[j] == '\t' || val[j] == '\n' {
			return val[:j]
		}
	}
	return val
}

func parseInt64(s string) int64 {
	var n int64
	var neg bool
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '-' && i == 0 {
			neg = true
			continue
		}
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int64(c-'0')
	}
	if neg {
		n = -n
	}
	return n
}
