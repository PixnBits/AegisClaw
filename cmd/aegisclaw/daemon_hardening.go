package main

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"

	"go.uber.org/zap"
)

type AegisHubMonitor struct {
	logger                *zap.Logger
	maxFailsBeforeRestart int
	consecutiveFails      int
	stopOnce              sync.Once
}

func (m *AegisHubMonitor) Stop() {
	m.stopOnce.Do(func() {
		if m.logger != nil {
			m.logger.Info("AegisHub monitor stopped")
		}
	})
}

func dropCapabilities(logger *zap.Logger) error {
	if logger != nil {
		logger.Debug("capability drop not active in this build")
	}
	return nil
}

func createSecureSocket(socketPath string, logger *zap.Logger) (net.Listener, error) {
	socketDir := filepath.Dir(socketPath)
	if err := os.MkdirAll(socketDir, 0700); err != nil {
		return nil, fmt.Errorf("create socket directory: %w", err)
	}
	_ = os.Remove(socketPath)
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("listen on unix socket: %w", err)
	}
	if err := os.Chmod(socketPath, 0600); err != nil {
		_ = listener.Close()
		return nil, fmt.Errorf("set socket permissions: %w", err)
	}
	if logger != nil {
		logger.Info("created secure Unix socket", zap.String("path", socketPath))
	}
	return listener, nil
}
