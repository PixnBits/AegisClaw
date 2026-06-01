//go:build linux
// +build linux

package sandbox

import (
	"AegisClaw/internal/config"
)

func init() {
	newBackendFunc = func(cfg *config.Config) (Backend, error) {
		return NewFirecrackerBackend(cfg.StateDir), nil
	}
}
