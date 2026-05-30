// Package config provides platform and runtime configuration detection.
package config

import (
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
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
	return ResolveKernelPath()
}

func getRootfsDir() string {
	return ResolveRootfsDir()
}

// ResolveKernelPath returns the Firecracker kernel path, re-reading the environment
// each call. Prefer explicit AEGIS_KERNEL_PATH, then the invoking user's
// ~/.aegis/firecracker/vmlinux (via SUDO_USER when running under sudo).
func ResolveKernelPath() string {
	if kernelPath := os.Getenv("AEGIS_KERNEL_PATH"); kernelPath != "" {
		return kernelPath
	}

	for _, home := range candidateHomes() {
		userKernel := filepath.Join(home, ".aegis/firecracker/vmlinux")
		if _, err := os.Stat(userKernel); err == nil {
			return userKernel
		}
	}

	return "/opt/aegis/firecracker/vmlinux"
}

// ResolveRootfsDir returns the microVM rootfs directory, re-reading the environment
// each call. This must be invoked at daemon startup (not only in init()) because
// the background daemon child may lose SUDO_USER while AEGIS_ROOTFS_DIR is unset,
// causing a stale fallback to /opt/aegis even when images were built under the
// developer's ~/.aegis/firecracker/rootfs (common when /opt is not writable).
func ResolveRootfsDir() string {
	if rootfsDir := os.Getenv("AEGIS_ROOTFS_DIR"); rootfsDir != "" {
		return rootfsDir
	}

	for _, home := range candidateHomes() {
		candidate := filepath.Join(home, ".aegis/firecracker/rootfs")
		if rootfsDirUsable(candidate) {
			return candidate
		}
	}

	return "/opt/aegis/firecracker/rootfs"
}

// candidateHomes returns home directories to search for user-built artifacts.
// SUDO_USER is listed first so we prefer the invoking developer's tree when
// the daemon runs as root via sudo (even if HOME=/root in the child process).
func candidateHomes() []string {
	seen := make(map[string]bool)
	var homes []string
	add := func(h string) {
		if h == "" || seen[h] {
			return
		}
		seen[h] = true
		homes = append(homes, h)
	}

	if sudoUser := os.Getenv("SUDO_USER"); sudoUser != "" {
		if u, err := user.Lookup(sudoUser); err == nil {
			add(u.HomeDir)
		}
	}
	add(getEffectiveHomeDir())
	return homes
}

// rootfsDirUsable reports whether dir exists and contains at least one built
// .img (preferred) or is an existing directory (tarball-only / mid-build).
func rootfsDirUsable(dir string) bool {
	st, err := os.Stat(dir)
	if err != nil || !st.IsDir() {
		return false
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".img") {
			return true
		}
	}
	// Directory exists (may hold tarballs awaiting conversion).
	return true
}
