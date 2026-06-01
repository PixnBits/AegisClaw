//go:build !linux

package main

import "os"

// isControlSocketReady falls back to a simple filesystem stat on non-Linux platforms.
func isControlSocketReady(addr string) bool {
	_, err := os.Stat(addr)
	return err == nil
}
