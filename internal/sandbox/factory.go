package sandbox

import (
	"AegisClaw/internal/config"
)

// newBackendFunc is a function variable that gets assigned differently per platform.
var newBackendFunc func(*config.Config) (Backend, error)

// New creates a new sandbox backend for the current platform.
func New(cfg *config.Config) (Backend, error) {
	if newBackendFunc == nil {
		panic("newBackendFunc not initialized")
	}
	return newBackendFunc(cfg)
}
