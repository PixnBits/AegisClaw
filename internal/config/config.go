// Package config provides platform and runtime configuration detection.
package config

import (
	"os"
	"os/user"
	"path/filepath"
	"runtime"
)

type Platform string

const (
	Linux   Platform = "linux"
	Darwin  Platform = "darwin"
	Windows Platform = "windows"
)

type SandboxType string

const (
	Firecracker SandboxType = "firecracker"
	Docker      SandboxType = "docker"
)

// Config holds platform and sandbox configuration.
type Config struct {
	Platform    Platform
	SandboxType SandboxType
	StateDir    string
	KernelPath  string
	RootfsDir   string
}

// New creates a new configuration based on the current platform.
func New() *Config {
	var platform Platform
	switch runtime.GOOS {
	case "linux":
		platform = Linux
	case "darwin":
		platform = Darwin
	case "windows":
		platform = Windows
	default:
		platform = Platform(runtime.GOOS)
	}

	sandboxType := getSandboxType(platform)

	return &Config{
		Platform:    platform,
		SandboxType: sandboxType,
		StateDir:    getStateDir(),
		KernelPath:  getKernelPath(),
		RootfsDir:   getRootfsDir(),
	}
}

func getSandboxType(platform Platform) SandboxType {
	if platform == Linux {
		return Firecracker
	}
	return Docker
}

func getStateDir() string {
	if stateDir := os.Getenv("AEGIS_STATE_DIR"); stateDir != "" {
		return stateDir
	}
	home, _ := os.UserHomeDir()
	return home + "/.aegis/state"
}

// getEffectiveHomeDir returns the home directory of the original invoking user
// when running under sudo (via SUDO_USER), falling back to the current process
// user. This lets the daemon (which usually runs as root) automatically find
// user-built kernels and images in the developer's own ~/.aegis directory.
func getEffectiveHomeDir() string {
	if sudoUser := os.Getenv("SUDO_USER"); sudoUser != "" {
		if u, err := user.Lookup(sudoUser); err == nil && u.HomeDir != "" {
			return u.HomeDir
		}
	}
	home, _ := os.UserHomeDir()
	return home
}

func getKernelPath() string {
	if kernelPath := os.Getenv("AEGIS_KERNEL_PATH"); kernelPath != "" {
		return kernelPath
	}

	// Prefer a user-writable minimal kernel location (recommended).
	// When the daemon runs as root via sudo, we still want to find the kernel
	// the normal user built/downloaded into their own home.
	home := getEffectiveHomeDir()
	userKernel := filepath.Join(home, ".aegis/firecracker/vmlinux")
	if _, err := os.Stat(userKernel); err == nil {
		return userKernel
	}

	// Fallback to system location
	return "/opt/aegis/firecracker/vmlinux"
}

func getRootfsDir() string {
	if rootfsDir := os.Getenv("AEGIS_ROOTFS_DIR"); rootfsDir != "" {
		return rootfsDir
	}

	// Prefer the effective (original) user's home for images.
	// The build script already defaults to putting images here.
	home := getEffectiveHomeDir()
	userRootfs := filepath.Join(home, ".aegis/firecracker/rootfs")
	// Only use it if something actually exists there (otherwise fall back to /opt).
	if _, err := os.Stat(userRootfs); err == nil {
		return userRootfs
	}

	return "/opt/aegis/firecracker/rootfs"
}
