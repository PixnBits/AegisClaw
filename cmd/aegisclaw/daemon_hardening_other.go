//go:build !linux

package main

import "go.uber.org/zap"

// Stubs for non-Linux platforms (macOS/Windows Docker Sandbox path).
// Hardening (caps, seccomp, prctl, rlimits via the Linux-specific syscalls) is
// a no-op here; the Docker backend provides its own containment. The functions
// exist so that start.go and the TCB smoke tests compile and run everywhere.

func dropCapabilities(logger *zap.Logger) error {
	if logger != nil {
		logger.Debug("Phase 4: capability dropping skipped on non-Linux (Docker sandbox path)")
	}
	return nil
}

func applyCapabilityBoundingSet(logger *zap.Logger) error {
	if logger != nil {
		logger.Debug("Phase 4: capability bounding set skipped on non-Linux")
	}
	return nil
}

func applySeccompFilter(logger *zap.Logger) error {
	if logger != nil {
		logger.Debug("Phase 4: seccomp-bpf skipped on non-Linux (no SECCOMP_MODE_FILTER)")
	}
	return nil
}

func setResourceLimits(logger *zap.Logger) error {
	if logger != nil {
		logger.Debug("Phase 4: resource limits (rlimit) skipped on non-Linux")
	}
	return nil
}
