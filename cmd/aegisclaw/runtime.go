package main

import (
	"fmt"

	"golang.org/x/sys/unix"
	"go.uber.org/zap"
)

// dropCapabilities removes all capabilities except the minimal set required.
// This should be called as early as possible after startup.
func dropCapabilities(logger *zap.Logger) error {
	// Minimal capabilities typically needed for Firecracker + jailer + socket operations.
	// Adjust based on your exact jailer configuration.
	keep := []unix.Cap{
		unix.CAP_SYS_ADMIN,   // Often needed for Firecracker namespaces/jailer
		unix.CAP_NET_ADMIN,   // Network namespace setup
		unix.CAP_SYS_CHROOT,  // Chroot for jailer
		unix.CAP_DAC_OVERRIDE, // File access in restricted environments
	}

	// Clear all capabilities first
	if err := unix.Prctl(unix.PR_CAPBSET_DROP, unix.CAP_SYS_ADMIN, 0, 0, 0); err != nil {
		// Non-fatal on some systems
	}

	// Set permitted + effective to the minimal set
	hdr := unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3}
	data := unix.CapUserData{
		Effective:   capMask(keep),
		Permitted:   capMask(keep),
		Inheritable: 0,
	}

	if err := unix.Capset(&hdr, &data); err != nil {
		return fmt.Errorf("failed to drop capabilities: %w", err)
	}

	logger.Info("Capabilities dropped to minimal set", zap.Any("kept", keep))
	return nil
}

func capMask(caps []unix.Cap) uint32 {
	var mask uint32
	for _, c := range caps {
		mask |= 1 << uint(c)
	}
	return mask
}
