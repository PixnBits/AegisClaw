//go:build linux

package main

import (
	"os"
	"unsafe"

	"go.uber.org/zap"
	"golang.org/x/sys/unix"
)

// dropCapabilities (linux implementation) — Phase 4 capability dropping for minimal TCB.
// (full body and docs identical to previous location in daemon_hardening.go)
func dropCapabilities(logger *zap.Logger) error {
	// Phase 4: Capability dropping for minimal TCB.
	// Per host-daemon.md "Minimal Privilege" requirement, we drop all
	// capabilities except the absolute minimum needed for Firecracker
	// VM lifecycle and Unix socket/VM directory operations.
	//
	// Retained:
	//   CAP_SYS_ADMIN  - REQUIRED for Firecracker jailer: the jailer binary
	//                    invokes chroot(2), unshare(2), and mount(2) to set up
	//                    the microVM's isolated rootfs and network namespace.
	//                    Dropping it would cause VM launch to fail immediately
	//                    (jailer cannot create the chroot jail or perform the
	//                    required unshare/mount for Firecracker's seccomp-isolated
	//                    execution environment). See testdata/cassettes/README.md.
	//   CAP_DAC_OVERRIDE - allows the daemon to access VM state directories
	//                      and the Unix socket path without granting broader
	//                      root privileges to other code paths.
	//
	// All others (CAP_NET_ADMIN, CAP_SYS_PTRACE, CAP_SETUID, etc.) are dropped.
	// This is done as early as possible (immediately after initRuntime) before
	// any VM launch or socket work.
	//
	// Complement: applyCapabilityBoundingSet (called right after) permanently
	// removes the dropped capabilities from the bounding set for defense-in-depth.

	// Step 1: prevent the process from gaining new privileges via exec.
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		if logger != nil {
			logger.Warn("Phase 4: PR_SET_NO_NEW_PRIVS failed (non-fatal)", zap.Error(err))
		}
		// continue; not fatal in all environments (e.g. some containers)
	}

	// Step 2: read current capabilities so we can log the before/after state.
	header := unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3}
	var data unix.CapUserData
	origEffective := uint32(0)
	if err := unix.Capget(&header, &data); err != nil {
		if logger != nil {
			logger.Debug("Phase 4: capget failed (running in limited environment: container/non-root)", zap.Error(err))
		}
		// still proceed with PR_SET_NO_NEW_PRIVS and attempt to set minimal caps
	} else {
		origEffective = data.Effective
	}

	// Step 3: set the minimal capability set.
	minCaps := uint32(1<<unix.CAP_SYS_ADMIN | 1<<unix.CAP_DAC_OVERRIDE)
	data.Effective = minCaps
	data.Permitted = minCaps
	data.Inheritable = 0

	if err := unix.Capset(&header, &data); err != nil {
		if logger != nil {
			logger.Warn("Phase 4: capset to minimal set failed (may be non-root)", zap.Error(err))
		}
		return nil
	}

	if logger != nil {
		logger.Info("Phase 4: capabilities dropped to minimal set",
			zap.Uint32("original_effective", origEffective),
			zap.Uint32("final_effective", minCaps),
			zap.String("kept", "CAP_SYS_ADMIN,CAP_DAC_OVERRIDE"),
			zap.String("dropped", "all others"))
	}
	return nil
}

// applyCapabilityBoundingSet permanently drops capabilities from the bounding set.
// Phase 4: This provides defense-in-depth so that even if the process or a child
// attempts to regain dropped capabilities (via exec or other means), it cannot.
// We keep only the two we intentionally allow: SYS_ADMIN and DAC_OVERRIDE.
func applyCapabilityBoundingSet(logger *zap.Logger) error {
	for c := 0; c <= unix.CAP_LAST_CAP; c++ {
		if c == unix.CAP_SYS_ADMIN || c == unix.CAP_DAC_OVERRIDE {
			continue
		}
		// PR_CAPBSET_DROP may fail in some containerized or limited environments;
		// treat as non-fatal but log for visibility.
		if err := unix.Prctl(unix.PR_CAPBSET_DROP, uintptr(c), 0, 0, 0); err != nil {
			if logger != nil {
				logger.Debug("Phase 4: PR_CAPBSET_DROP failed (may be container/non-root)",
					zap.Int("cap", c), zap.Error(err))
			}
		}
	}
	if logger != nil {
		logger.Info("Phase 4: capability bounding set applied (defense-in-depth)")
	}
	return nil
}

// applySeccompFilter installs a restrictive seccomp-bpf filter early in startup.
// Policy: default-deny. Only permit the syscalls required for the daemon's
// minimal TCB responsibilities (Firecracker VM lifecycle, Unix socket server,
// Merkle signing, watchdog).
func applySeccompFilter(logger *zap.Logger) error {
	strict := os.Getenv("AEGISCLAW_SECCOMP_STRICT") != "0"
	if !strict {
		if logger != nil {
			logger.Info("Phase 4: seccomp-bpf disabled via AEGISCLAW_SECCOMP_STRICT=0 (dev mode)")
		}
		return nil
	}

	// Real aggressive seccomp-bpf filter (Phase 4 completion).
	// We use a very restrictive allowlist. Only the syscalls strictly required
	// for VM lifecycle (Firecracker), Unix socket server, basic logging,
	// memory management, and the seccomp syscall itself are permitted.
	//
	// Dangerous syscalls (ptrace, mount, setns, unshare, execve, etc.) are
	// explicitly blocked with SECCOMP_RET_KILL or ERRNO.
	//
	// This is intentionally paranoid. If a required syscall is missing the
	// daemon will be killed; use AEGISCLAW_SECCOMP_STRICT=0 to disable during
	// development.

	// Minimal aggressive allowlist for a Firecracker + Unix-socket daemon.
	allowed := []int{
		unix.SYS_READ, unix.SYS_WRITE, unix.SYS_CLOSE,
		unix.SYS_OPENAT, unix.SYS_STATX, unix.SYS_FSTAT,
		unix.SYS_MMAP, unix.SYS_MUNMAP, unix.SYS_MPROTECT, unix.SYS_BRK,
		unix.SYS_CLONE, unix.SYS_WAIT4, unix.SYS_GETPID, unix.SYS_GETTID,
		unix.SYS_FUTEX, unix.SYS_NANOSLEEP, unix.SYS_CLOCK_GETTIME,
		unix.SYS_PRCTL, unix.SYS_GETRANDOM,
		unix.SYS_SOCKET, unix.SYS_BIND, unix.SYS_LISTEN, unix.SYS_ACCEPT,
		unix.SYS_CONNECT, unix.SYS_SENDTO, unix.SYS_RECVFROM,
		unix.SYS_SETSOCKOPT, unix.SYS_GETSOCKOPT,
		unix.SYS_IOCTL, unix.SYS_FCNTL, unix.SYS_FLOCK,
		unix.SYS_EPOLL_CREATE1, unix.SYS_EPOLL_CTL, unix.SYS_EPOLL_PWAIT,
		unix.SYS_EVENTFD2, unix.SYS_TIMERFD_CREATE, unix.SYS_TIMERFD_SETTIME,
	}

	// Build a simple filter program
	var filter []unix.SockFilter
	// Load syscall number
	filter = append(filter, unix.SockFilter{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: uint32(4)}) // offsetof(seccomp_data, nr)

	for _, nr := range allowed {
		// if (nr == syscall) accept
		filter = append(filter,
			unix.SockFilter{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: uint32(nr), Jt: 0, Jf: 1},
			unix.SockFilter{Code: unix.BPF_RET | unix.BPF_K, K: uint32(unix.SECCOMP_RET_ALLOW)},
		)
	}

	// Default: kill the process (very aggressive)
	filter = append(filter, unix.SockFilter{Code: unix.BPF_RET | unix.BPF_K, K: uint32(unix.SECCOMP_RET_KILL_PROCESS)})

	prog := unix.SockFprog{
		Len:    uint16(len(filter)),
		Filter: &filter[0],
	}

	// Use prctl to set the filter (unix.Seccomp may not be available in all versions)
	if err := unix.Prctl(unix.PR_SET_SECCOMP, unix.SECCOMP_MODE_FILTER, uintptr(unsafe.Pointer(&prog)), 0, 0); err != nil {
		if logger != nil {
			logger.Warn("Phase 4: failed to install aggressive seccomp filter", zap.Error(err))
		}
		return nil // non-fatal in some environments
	}

	if logger != nil {
		logger.Info("Phase 4: aggressive seccomp-bpf filter installed (paranoid allowlist)")
	}
	return nil
}

// setResourceLimits applies conservative rlimits to the daemon process.
// Phase 4: prevents unbounded memory or file descriptor consumption.
func setResourceLimits(logger *zap.Logger) error {
	// Conservative limits suitable for a minimal TCB daemon.
	limits := []struct {
		name string
		res  int
		soft uint64
		hard uint64
	}{
		{"RLIMIT_AS", unix.RLIMIT_AS, 256 * 1024 * 1024, 512 * 1024 * 1024}, // 256MB soft / 512MB hard
		{"RLIMIT_NOFILE", unix.RLIMIT_NOFILE, 1024, 2048},
	}

	for _, l := range limits {
		rlim := unix.Rlimit{Cur: l.soft, Max: l.hard}
		if err := unix.Setrlimit(l.res, &rlim); err != nil {
			if logger != nil {
				logger.Warn("Phase 4: failed to set rlimit", zap.String("limit", l.name), zap.Error(err))
			}
			continue
		}
		if logger != nil {
			logger.Debug("Phase 4: rlimit applied", zap.String("limit", l.name), zap.Uint64("soft", l.soft))
		}
	}
	return nil
}
