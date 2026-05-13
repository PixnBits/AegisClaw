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
