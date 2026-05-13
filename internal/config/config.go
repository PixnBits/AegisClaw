// Package config provides platform and runtime configuration detection.
package config

import (
	"os"
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

func getKernelPath() string {
	if kernelPath := os.Getenv("AEGIS_KERNEL_PATH"); kernelPath != "" {
		return kernelPath
	}
	return "/opt/aegis/firecracker/vmlinux"
}

func getRootfsDir() string {
	if rootfsDir := os.Getenv("AEGIS_ROOTFS_DIR"); rootfsDir != "" {
		return rootfsDir
	}
	return "/opt/aegis/firecracker/rootfs"
}
