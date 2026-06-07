//go:build linux

package sandbox

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
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
			logrus.Infof("VM %s: no pooled copy was available; fell back to full copy (pre-warm may still be running or this is first use)", vmID)
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

// copyFileFast attempts a CoW/reflink copy first (near-instant on XFS, btrfs, and other
// filesystems supporting --reflink=auto). This is the key optimization so that pre-warming
// pooled rootfs copies for agent-/memory- does not require external long sleeps (e.g. 300s+)
// before the pools are usable for claim in the <1s hot path.
//
// Falls back to the normal io.Copy if reflink is not supported by the FS or the cp binary.
// After a successful copy we also attempt to chown the pooled file to the effective
// invoking user (SUDO_USER) so that normal-user client commands and `ls` can see the
// artifacts without root.
func copyFileFast(src, dst string) error {
	// Prefer reflink for speed — this directly addresses the root cause of needing
	// multi-minute external waits for PrewarmPooledRootfsCopies to produce claimable files.
	if err := exec.Command("cp", "--reflink=auto", src, dst).Run(); err == nil {
		return nil
	}
	// Fallback for filesystems without reflink support (or older cp).
	return copyFile(src, dst)
}

// effectiveOwner returns the uid/gid of the original invoking user when the daemon
// is running under sudo (SUDO_USER). Falls back to the current process uid/gid.
// Used so that pooled rootfs copies (created as root) remain visible and stat-able
// by the normal user running ./bin/aegis vm ... and similar client tools.
func effectiveOwner() (int, int) {
	if su := os.Getenv("SUDO_USER"); su != "" {
		if u, err := user.Lookup(su); err == nil {
			if uid, err := strconv.Atoi(u.Uid); err == nil {
				if gid, err := strconv.Atoi(u.Gid); err == nil {
					return uid, gid
				}
			}
		}
	}
	return os.Getuid(), os.Getgid()
}

// PrewarmPooledRootfsCopies pre-creates up to count private copies of the template rootfs
// for fast claim by hot per-VM paths (agent-*/memory-*). This moves the expensive 512MB
// copy off the StartVM / chat session critical path (claim does atomic rename instead).
//
// Uses copyFileFast (reflink=auto when the backing FS supports CoW) so that pre-warming
// completes in seconds or less instead of requiring external multi-minute sleeps before
// pooled files are visible and claimable. After copy we chown to the SUDO_USER effective
// owner for visibility from normal-user client tools.
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

	uid, gid := effectiveOwner()

	created := 0
	for i := 0; i < count; i++ {
		dst := filepath.Join(stateDir, fmt.Sprintf("%s-pooled-%d.rootfs.img", prefix, i))
		if _, err := os.Stat(dst); err == nil {
			continue // already have one
		}
		if err := copyFileFast(templateRootfs, dst); err != nil {
			logrus.Warnf("Prewarm pooled copy %d failed: %v", i, err)
			continue
		}
		// Make the pooled file visible to the original user (not just root).
		// Best-effort; the file is still 0600-ish from create but we want readability for inspection.
		_ = os.Chown(dst, uid, gid)
		_ = os.Chmod(dst, 0644)
		created++
	}
	if created > 0 {
		logrus.Infof("Prewarmed %d pooled rootfs copies for prefix=%s (from %s) using fast path", created, prefix, templateRootfs)
	}
	// Always report current pooled inventory so operators see progress without external long sleeps.
	pattern := filepath.Join(stateDir, prefix+"-pooled-*.rootfs.img")
	if matches, _ := filepath.Glob(pattern); len(matches) > 0 {
		logrus.Infof("Pooled copies now available for %s: %d (claim will use rename, not copy)", prefix, len(matches))
	}
	return created
}

// claimPooledRootfs attempts to atomically claim a pre-warmed pooled copy for this vmID
// by renaming a matching pooled file into the expected per-VM dst location.
// Returns the path to use (claimed or original template) and whether a claim happened.
// A successful claim is what delivers the low "host" phase time (~100-200ms) instead of
// paying the full rootfs copy cost on every agent/memory start.
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
