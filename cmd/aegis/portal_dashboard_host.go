package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// hostCollectOverviewBundle gathers dashboard stats on the host (orchestrator + /proc).
// Used by /api/host/dashboard-stats and host /events SSE (guest bridge is often unavailable).
func hostCollectOverviewBundle() map[string]interface{} {
	workers := portalWorkerList()
	sandboxes := portalSandboxList()
	hostStats := readHostSystemStats()

	runningVMCount := 0
	var runningVMVCPUs, runningVMMemoryMB int64
	runningVMs := make([]interface{}, 0)
	for _, raw := range sandboxes {
		m, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		status := strings.ToLower(fmt.Sprintf("%v", m["status"]))
		if status != "running" && status != "" {
			continue
		}
		runningVMCount++
		if v, ok := m["vcpus"].(float64); ok {
			runningVMVCPUs += int64(v)
		}
		if v, ok := m["memory_mb"].(float64); ok {
			runningVMMemoryMB += int64(v)
		}
		m["name"] = m["id"]
		m["state"] = m["status"]
		runningVMs = append(runningVMs, m)
	}

	timerCount := 0
	if timers, err := sendToComponentViaHub("store", "timer.list", nil); err == nil {
		timerCount = countItems(timers)
	}

	sessions, _ := callStoreSessionsAction("sessions.list", nil)
	sessCount := countItems(sessions)

	hostRAMTotalMB := int64(toFloat64(hostStats["host_ram_total_mb"]))
	hostRAMUsedMB := int64(toFloat64(hostStats["host_ram_used_mb"]))
	hostRAMPct := int(toFloat64(hostStats["host_ram_pct"]))
	hostLoadAvg1 := toFloat64(hostStats["host_load_avg_1"])

	return map[string]interface{}{
		"worker_count":         len(workers),
		"approval_count":       0,
		"session_count":        sessCount,
		"timer_count":          timerCount,
		"memory_count":         0,
		"running_vm_count":     runningVMCount,
		"running_vm_vcpus":   runningVMVCPUs,
		"running_vm_memory_mb": runningVMMemoryMB,
		"running_vm_rss_mb":    0,
		"host_ram_label":       fmtHostRAMLabel(hostRAMUsedMB, hostRAMTotalMB),
		"host_ram_pct":         hostRAMPct,
		"host_load_label":      fmt.Sprintf("%.2f", hostLoadAvg1),
		"workers":              workers,
		"approvals":            []interface{}{},
		"running_vms":          runningVMs,
		"sessions":             sessions,
		"tool_events":          []interface{}{},
		"thought_events":       []interface{}{},
		"ts":                   time.Now().UTC().Format(time.RFC3339),
	}
}

func readHostSystemStats() map[string]interface{} {
	stats := map[string]interface{}{
		"host_ram_total_mb": 0.0,
		"host_ram_used_mb":  0.0,
		"host_ram_pct":      0.0,
		"host_load_avg_1":   0.0,
		"running_vms":       0.0,
	}
	if orchestrator != nil {
		if vms, err := orchestrator.ListVMs(context.Background()); err == nil {
			stats["running_vms"] = float64(len(vms))
		}
	}
	if data, err := os.ReadFile("/proc/meminfo"); err == nil {
		var totalKB, availKB float64
		sc := bufio.NewScanner(strings.NewReader(string(data)))
		for sc.Scan() {
			line := sc.Text()
			if strings.HasPrefix(line, "MemTotal:") {
				totalKB = parseProcMemKB(line)
			} else if strings.HasPrefix(line, "MemAvailable:") {
				availKB = parseProcMemKB(line)
			}
		}
		if totalKB > 0 {
			totalMB := totalKB / 1024
			usedMB := (totalKB - availKB) / 1024
			if usedMB < 0 {
				usedMB = 0
			}
			pct := (usedMB / totalMB) * 100
			stats["host_ram_total_mb"] = totalMB
			stats["host_ram_used_mb"] = usedMB
			stats["host_ram_pct"] = pct
		}
	}
	if data, err := os.ReadFile("/proc/loadavg"); err == nil {
		fields := strings.Fields(string(data))
		if len(fields) > 0 {
			if v, err := strconv.ParseFloat(fields[0], 64); err == nil {
				stats["host_load_avg_1"] = v
			}
		}
	}
	return stats
}

func parseProcMemKB(line string) float64 {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return 0
	}
	v, _ := strconv.ParseFloat(fields[1], 64)
	return v
}

func fmtHostRAMLabel(usedMB, totalMB int64) string {
	if totalMB <= 0 {
		return "— / —"
	}
	return fmt.Sprintf("%d MB / %d MB", usedMB, totalMB)
}

func countItems(v interface{}) int {
	if v == nil {
		return 0
	}
	if s, ok := v.([]interface{}); ok {
		return len(s)
	}
	return 0
}

func toFloat64(v interface{}) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	default:
		return 0
	}
}

func handleHostDashboardStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET required", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(hostCollectOverviewBundle()) //nolint:errcheck
}

func handleHostSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	ctx := r.Context()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	writeSSE := func(v interface{}) bool {
		b, err := json.Marshal(v)
		if err != nil {
			return false
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}

	writeSSE(map[string]interface{}{"type": "heartbeat", "ts": time.Now().UTC().Format(time.RFC3339)})

	var lastFingerprint string
	tick := 0

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tick++
			bundle := hostCollectOverviewBundle()
			fpBytes, _ := json.Marshal(bundle)
			fp := string(fpBytes)
			if fp != lastFingerprint {
				lastFingerprint = fp
				update := map[string]interface{}{
					"type":              "update",
					"active_workers":    bundle["workers"],
					"pending_approvals": bundle["approvals"],
					"tool_events":       bundle["tool_events"],
					"thought_events":    bundle["thought_events"],
					"sessions":          bundle["sessions"],
					"overview":          bundle,
					"ts":                bundle["ts"],
				}
				if !writeSSE(update) {
					return
				}
			} else if tick%15 == 0 {
				if !writeSSE(map[string]interface{}{"type": "heartbeat", "ts": time.Now().UTC().Format(time.RFC3339)}) {
					return
				}
			}
		}
	}
}
