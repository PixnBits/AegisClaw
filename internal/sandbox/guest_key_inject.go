//go:build linux

package sandbox

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sirupsen/logrus"
)

// guestVMKeyPath is where the distributed key lives in the guest rootfs (avoid /run tmpfs overlay).
const guestVMKeyPath = "/etc/aegis/vmkey"

// injectVMKeyIntoRootfs writes the host .vmkey file into the guest image before Firecracker boots.
func injectVMKeyIntoRootfs(rootfsPath, hostKeyPath string) error {
	if rootfsPath == "" || hostKeyPath == "" {
		return fmt.Errorf("rootfs or key path empty")
	}
	if _, err := os.Stat(rootfsPath); err != nil {
		return fmt.Errorf("rootfs %s: %w", rootfsPath, err)
	}
	keyData, err := os.ReadFile(hostKeyPath)
	if err != nil {
		return fmt.Errorf("read host vmkey: %w", err)
	}

	mnt, err := os.MkdirTemp("", "aegis-vmkey-mnt-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(mnt)

	mount := exec.Command("mount", "-o", "loop,rw", rootfsPath, mnt)
	if out, err := mount.CombinedOutput(); err != nil {
		return fmt.Errorf("mount rootfs for key inject: %w (%s)", err, string(out))
	}
	defer func() {
		_ = exec.Command("umount", mnt).Run()
	}()

	guestDir := filepath.Join(mnt, "etc", "aegis")
	if err := os.MkdirAll(guestDir, 0700); err != nil {
		return fmt.Errorf("mkdir guest /etc/aegis: %w", err)
	}
	guestKey := filepath.Join(guestDir, "vmkey")
	if err := os.WriteFile(guestKey, keyData, 0600); err != nil {
		return fmt.Errorf("write guest vmkey: %w", err)
	}
	return nil
}

func needsPerVMRootfs(vmID string) bool {
	return strings.HasPrefix(vmID, "agent-") || strings.HasPrefix(vmID, "memory-")
}

// prepareVMRootfs returns a rootfs path for this VM. Paired agent/memory VMs get a
// private copy so injecting /run/aegis/vmkey does not clobber the shared template image.
func prepareVMRootfs(stateDir, vmID, templateRootfs, hostKeyPath string) string {
	rootfsPath := templateRootfs
	if !needsPerVMRootfs(vmID) || hostKeyPath == "" {
		return rootfsPath
	}

	// Collaboration <1s path: try to claim a pre-warmed pooled copy first (see PrewarmPooledRootfsCopies).
	// This avoids paying the full 512MB io.Copy on every per-session agent/memory launch.
	claimedPath, claimed := claimPooledRootfs(stateDir, vmID, templateRootfs)
	if claimed {
		rootfsPath = claimedPath
	} else {
		dst := filepath.Join(stateDir, vmID+".rootfs.img")
		_ = os.Remove(dst)
		if err := copyFile(templateRootfs, dst); err != nil {
			logrus.Warnf("VM %s: rootfs copy failed (%v), using shared image for key inject", vmID, err)
		} else {
			rootfsPath = dst
		}
	}

	if err := injectVMKeyIntoRootfs(rootfsPath, hostKeyPath); err != nil {
		logrus.Warnf("VM %s: could not inject /run/aegis/vmkey: %v", vmID, err)
	} else {
		logrus.Infof("VM %s: injected VM key into %s (%s)", vmID, rootfsPath, guestVMKeyPath)
	}
	return rootfsPath
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

// PrewarmPooledRootfsCopies pre-creates up to count private copies of the template rootfs
// for fast claim by hot per-VM paths (agent-*/memory-*). This moves the expensive 512MB
// io.Copy + potential FS work off the StartVM / chat session critical path.
//
// Call from orchestrator (e.g. New or a background goroutine after images are known)
// or daemon bootstrap. Copies are named <stateDir>/<prefix>-pooled-<n>.rootfs.img and
// claimed by rename in prepareVMRootfs when a matching agent-/memory- ID is started.
//
// Court/SDLC/PM roles using shared images (no needsPerVMRootfs) are unaffected and benefit
// from cmdline-only key distribution today.
//
// Part of <1s tactics for the collaboration model (see docs/implementation-plan/collaboration-model.md).
// Measurement: use AEGIS_BOOT_TIMING=1 + scripts/boot-metrics.sh (or aegis vm ...) around
// StartPairedAgentAndMemory and role ensures. Run under exact `make start` per AGENTS.md.
func PrewarmPooledRootfsCopies(stateDir, templateRootfs string, count int, prefix string) int {
	if count <= 0 || templateRootfs == "" || stateDir == "" {
		return 0
	}
	if _, err := os.Stat(templateRootfs); err != nil {
		logrus.Warnf("PrewarmPooledRootfsCopies: template missing %s: %v", templateRootfs, err)
		return 0
	}
	_ = os.MkdirAll(stateDir, 0700)

	created := 0
	for i := 0; i < count; i++ {
		dst := filepath.Join(stateDir, fmt.Sprintf("%s-pooled-%d.rootfs.img", prefix, i))
		if _, err := os.Stat(dst); err == nil {
			continue // already have one
		}
		if err := copyFile(templateRootfs, dst); err != nil {
			logrus.Warnf("Prewarm pooled copy %d failed: %v", i, err)
			continue
		}
		created++
	}
	if created > 0 {
		logrus.Infof("Prewarmed %d pooled rootfs copies for prefix=%s (from %s)", created, prefix, templateRootfs)
	}
	return created
}

// claimPooledRootfs attempts to atomically claim a pre-warmed pooled copy for this vmID
// by renaming a matching pooled file into the expected per-VM dst location.
// Returns the path to use (claimed or original template) and whether a claim happened.
func claimPooledRootfs(stateDir, vmID, templateRootfs string) (string, bool) {
	if !needsPerVMRootfs(vmID) {
		return templateRootfs, false
	}
	// Look for any pooled file for the common prefixes (agent or memory).
	// In real use the orchestrator passes a good prefix; here we infer from vmID.
	pfx := "agent"
	if strings.HasPrefix(vmID, "memory-") {
		pfx = "memory"
	}
	pattern := filepath.Join(stateDir, pfx+"-pooled-*.rootfs.img")
	matches, _ := filepath.Glob(pattern)
	for _, p := range matches {
		dst := filepath.Join(stateDir, vmID+".rootfs.img")
		// Best-effort exclusive claim via rename (atomic on same fs).
		if err := os.Rename(p, dst); err == nil {
			logrus.Infof("Claimed pooled rootfs %s -> %s for %s", p, dst, vmID)
			return dst, true
		}
		// If rename failed (race or permission), fall through and let normal copy happen.
	}
	return templateRootfs, false
}
