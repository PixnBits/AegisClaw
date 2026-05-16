//go:build linux

package main

import (
	"os"

	"golang.org/x/sys/unix"
)

func disableTerminalEcho(fd int) (func(), error) {
	oldState, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		return func() {}, err
	}
	newState := *oldState
	newState.Lflag &^= unix.ECHO
	if err := unix.IoctlSetTermios(fd, unix.TCSETS, &newState); err != nil {
		return func() {}, err
	}
	return func() {
		_ = unix.IoctlSetTermios(fd, unix.TCSETS, oldState)
	}, nil
}

func openSecretFileNoFollow(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW, 0)
}
