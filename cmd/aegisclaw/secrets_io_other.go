//go:build !linux

package main

import (
	"errors"
	"os"
)

func disableTerminalEcho(_ int) (func(), error) {
	return func() {}, errors.ErrUnsupported
}

func openSecretFileNoFollow(path string) (*os.File, error) {
	return os.Open(path)
}
