package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/PixnBits/AegisClaw/internal/api"
)

// systemStatsResponse holds real host-level resource metrics sampled from /proc.
type systemStatsResponse struct {
	HostRAMTotalMB int64   `json:"host_ram_total_mb"`
	HostRAMUsedMB  int64   `json:"host_ram_used_mb"`
	HostRAMPct     int     `json:"host_ram_pct"` // 0-100
	HostLoadAvg1   float64 `json:"host_load_avg_1"`
	HostLoadAvg5   float64 `json:"host_load_avg_5"`
	HostLoadAvg15  float64 `json:"host_load_avg_15"`
}

// makeSystemStatsHandler returns a handler that reports real host RAM and CPU
// load from /proc/meminfo and /proc/loadavg. No external dependencies needed.
func makeSystemStatsHandler() api.Handler {
	return func(_ context.Context, _ json.RawMessage) *api.Response {
		stats := readSystemStats()
		out, _ := json.Marshal(stats)
		return &api.Response{Success: true, Data: out}
	}
}

func readSystemStats() systemStatsResponse {
	var s systemStatsResponse

	// /proc/meminfo — single read, values are always fresh from the kernel.
	f, err := os.Open("/proc/meminfo")
	if err == nil {
		defer f.Close()
		var totalKB, availKB int64
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := scanner.Text()
			parts := strings.Fields(line)
			if len(parts) < 2 {
				continue
			}
			switch parts[0] {
			case "MemTotal:":
				totalKB, _ = strconv.ParseInt(parts[1], 10, 64)
			case "MemAvailable:":
				availKB, _ = strconv.ParseInt(parts[1], 10, 64)
			}
		}
		s.HostRAMTotalMB = totalKB / 1024
		s.HostRAMUsedMB = (totalKB - availKB) / 1024
		if totalKB > 0 {
			s.HostRAMPct = int(100 * (totalKB - availKB) / totalKB)
		}
	}

	// /proc/loadavg — kernel-maintained 1/5/15-minute exponential averages.
	if raw, err := os.ReadFile("/proc/loadavg"); err == nil {
		parts := strings.Fields(string(raw))
		if len(parts) >= 3 {
			s.HostLoadAvg1, _ = strconv.ParseFloat(parts[0], 64)
			s.HostLoadAvg5, _ = strconv.ParseFloat(parts[1], 64)
			s.HostLoadAvg15, _ = strconv.ParseFloat(parts[2], 64)
		}
	}

	return s
}

// fmtMB formats MB as "X.X GB" when >= 1024, otherwise "X MB".
func fmtMB(mb int64) string {
	if mb >= 1024 {
		return fmt.Sprintf("%.1f GB", float64(mb)/1024)
	}
	return fmt.Sprintf("%d MB", mb)
}

// procRSSKB reads the RSS (resident set size) of process pid from
// /proc/<pid>/status. Returns 0 if the process is gone or unreadable.
// Cost: one open+scan per call — negligible.
func procRSSKB(pid int) int64 {
	if pid <= 0 {
		return 0
	}
	f, err := os.Open(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return 0
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		parts := strings.Fields(scanner.Text())
		if len(parts) >= 2 && parts[0] == "VmRSS:" {
			kb, _ := strconv.ParseInt(parts[1], 10, 64)
			return kb
		}
	}
	return 0
}

// procSystemUptimeSeconds reads /proc/uptime once (caller caches if needed).
func procSystemUptimeSeconds() float64 {
	raw, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0
	}
	parts := strings.Fields(string(raw))
	if len(parts) == 0 {
		return 0
	}
	v, _ := strconv.ParseFloat(parts[0], 64)
	return v
}

// procCPUAvgPct returns the lifetime-average CPU utilisation for process pid
// as a percentage (0–100 × vcpus). It reads /proc/<pid>/stat once — no
// background goroutine needed. The percentage is averaged over the interval
// from when the process started until now (relative to system uptime).
//
// Formula:  (utime + stime + cutime + cstime) / (HZ × elapsed) × 100
// Assumes HZ = 100 (standard Linux tick rate).
func procCPUAvgPct(pid int, uptimeSeconds float64) float64 {
	if pid <= 0 || uptimeSeconds <= 0 {
		return 0
	}
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0
	}
	// Fields are space-separated; the 2nd field (comm) can contain spaces inside
	// parens, so find the closing ')' first.
	s := string(raw)
	closeIdx := strings.LastIndex(s, ")")
	if closeIdx < 0 {
		return 0
	}
	fields := strings.Fields(s[closeIdx+1:])
	// After ')': field index 0=state, 11=utime, 12=stime, 13=cutime, 14=cstime,
	// 19=starttime (all relative to the start-of-stat fields after the ')').
	// /proc/<pid>/stat field numbering (1-based, after comm closure):
	//   1=state 2=ppid … 12=utime 13=stime 14=cutime 15=cstime … 20=starttime
	if len(fields) < 20 {
		return 0
	}
	utime, _ := strconv.ParseFloat(fields[11], 64)
	stime, _ := strconv.ParseFloat(fields[12], 64)
	cutime, _ := strconv.ParseFloat(fields[13], 64)
	cstime, _ := strconv.ParseFloat(fields[14], 64)
	starttime, _ := strconv.ParseFloat(fields[19], 64)

	const hz = 100.0
	elapsed := uptimeSeconds - starttime/hz
	if elapsed <= 0 {
		return 0
	}
	totalCPU := utime + stime + cutime + cstime
	return totalCPU / hz / elapsed * 100
}
