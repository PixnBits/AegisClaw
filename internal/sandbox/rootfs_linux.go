//go:build linux
// +build linux

package sandbox

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sirupsen/logrus"
)

// EnsureBootableRootfsImage ensures a bootable raw .img exists for the given component.
// If only a .tar.gz from `make build-microvms` is present, it converts it on the fly
// (requires root for loop mounts). Reconverts when the tarball is newer than the raw image.
func EnsureBootableRootfsImage(rootfsDir, component string) (string, error) {
	if rootfsDir == "" || component == "" {
		return "", fmt.Errorf("sandbox: rootfsDir and component required")
	}

	rawPath := filepath.Join(rootfsDir, component+".img")
	tarPath := rawPath + ".tar.gz"

	if rawStat, err := os.Stat(rawPath); err == nil {
		if tarStat, tarErr := os.Stat(tarPath); tarErr == nil && tarStat.ModTime().After(rawStat.ModTime()) {
			logrus.Infof("EnsureBootableRootfsImage(%s): tarball newer than %s — reconverting", component, rawPath)
			_ = os.Remove(rawPath)
		} else {
			return rawPath, nil
		}
	}

	if _, err := os.Stat(tarPath); err != nil {
		return "", fmt.Errorf("neither raw %s nor tarball %s exists (run 'make build-microvms')", rawPath, tarPath)
	}

	// Collaboration model <1s target: on-demand conversion (truncate + mkfs + loop mount + tar) is a multi-second I/O hit
	// on the critical StartVM path (see orchestrator.StartVM, firecracker prepareVMRootfs, and implementation-plan/collaboration-model.md).
	// Prefer/require pre-built raw .img (the build script produces them). This path should only be hit for first-use after clean
	// or explicit rebuild. Consider failing hard in prod paths or pre-ensuring at daemon bootstrap for known roles (agent, memory, court-*).
	logrus.Infof("EnsureBootableRootfsImage: converting %s → %s (perf warning: this is slow; pre-build raw .img via make build-microvms)", tarPath, rawPath)

	size := "512M"
	switch component {
	case "builder", "store", "network-boundary", "web-portal":
		size = "1G"
	}

	if err := os.MkdirAll(filepath.Dir(rawPath), 0755); err != nil {
		return "", err
	}

	if _, err := exec.Command("truncate", "-s", size, rawPath).CombinedOutput(); err != nil {
		if _, err2 := exec.Command("dd", "if=/dev/zero", "of="+rawPath, "bs=1M", "count=1024", "status=none").CombinedOutput(); err2 != nil {
			return "", fmt.Errorf("failed to create image file: %v", err2)
		}
	}

	if out, err := exec.Command("mkfs.ext4", "-F", "-L", "rootfs", rawPath).CombinedOutput(); err != nil {
		os.Remove(rawPath)
		return "", fmt.Errorf("mkfs.ext4 failed on %s: %s", rawPath, strings.TrimSpace(string(out)))
	}

	mnt, err := os.MkdirTemp("", "aegis-rootfs-mnt-*")
	if err != nil {
		os.Remove(rawPath)
		return "", err
	}
	defer os.RemoveAll(mnt)

	if out, err := exec.Command("mount", "-o", "loop", rawPath, mnt).CombinedOutput(); err != nil {
		os.Remove(rawPath)
		return "", fmt.Errorf("loop mount failed for %s: %s", rawPath, strings.TrimSpace(string(out)))
	}

	if out, err := exec.Command("tar", "-xzf", tarPath, "-C", mnt).CombinedOutput(); err != nil {
		exec.Command("umount", mnt).Run()
		os.Remove(rawPath)
		return "", fmt.Errorf("tar extract failed: %s", strings.TrimSpace(string(out)))
	}

	if out, err := exec.Command("umount", mnt).CombinedOutput(); err != nil {
		logrus.Warnf("umount warning after extract for %s: %s", rawPath, strings.TrimSpace(string(out)))
	}

	logrus.Infof("Successfully created raw rootfs image: %s (from %s)", rawPath, tarPath)
	return rawPath, nil
}
