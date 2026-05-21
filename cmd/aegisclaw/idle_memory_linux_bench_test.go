//go:build linux

package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
)

func readVmRSSKB() (uint64, error) {
	f, err := os.Open("/proc/self/status")
	if err != nil {
		return 0, err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := s.Text()
		if strings.HasPrefix(line, "VmRSS:") {
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			return strconv.ParseUint(fields[1], 10, 64)
		}
	}
	return 0, fmt.Errorf("VmRSS not found")
}

// BenchmarkProcVmRSS_DB03 reports this process resident set size (kB) as RSS_MB.
//
// The backlog target (~20 MB idle on reference Linux) applies to a minimal daemon
// after warm-up, not this benchmark harness. Use this metric when sampling a real
// `aegisclaw start` deployment; see docs/planning/daemon-test-backlog.md (DB-03).
func BenchmarkProcVmRSS_DB03(b *testing.B) {
	kb, err := readVmRSSKB()
	if err != nil {
		b.Fatal(err)
	}
	b.ReportMetric(float64(kb)/1024, "RSS_MB")
	for i := 0; i < b.N; i++ {
		if _, err := readVmRSSKB(); err != nil {
			b.Fatal(err)
		}
	}
}
