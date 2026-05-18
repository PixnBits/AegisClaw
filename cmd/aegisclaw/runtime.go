package main

import (
	"fmt"
	"net"
	"os"
	"path/filepath"

	"go.uber.org/zap"
)

// createSecureSocket creates a Unix domain socket with strict permissions.
// This is part of Phase 4.5 Unix Socket Hardening.
func createSecureSocket(socketPath string, logger *zap.Logger) (net.Listener, error) {
	socketDir := filepath.Dir(socketPath)

	// Ensure parent directory has strict permissions
	if err := os.MkdirAll(socketDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create socket directory: %w", err)
	}

	// Remove existing socket if present (common on restart)
	_ = os.Remove(socketPath)

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on unix socket: %w", err)
	}

	// Set strict permissions on the socket file itself
	if err := os.Chmod(socketPath, 0600); err != nil {
		listener.Close()
		return nil, fmt.Errorf("failed to set socket permissions: %w", err)
	}

	logger.Info("Created secure Unix socket", zap.String("path", socketPath))
	return listener, nil
}
