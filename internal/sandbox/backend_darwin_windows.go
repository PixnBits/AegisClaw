//go:build darwin || windows
// +build darwin windows

package sandbox

import (
	"AegisClaw/internal/config"
)

func init() {
	newBackendFunc = func(cfg *config.Config) (Backend, error) {
		return NewDockerBackend(cfg.StateDir), nil
	}
}

// PrewarmPooledRootfsCopies is a no-op stub on darwin/windows (real implementation lives in
// the linux-tagged guest_key_inject.go). The collaboration-model <1s pre-warm / pool logic
// only applies to Firecracker paths.
func PrewarmPooledRootfsCopies(stateDir, templateRootfs string, count int, prefix string) int {
	return 0
}
