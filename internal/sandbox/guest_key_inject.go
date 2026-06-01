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

	dst := filepath.Join(stateDir, vmID+".rootfs.img")
	_ = os.Remove(dst)
	if err := copyFile(templateRootfs, dst); err != nil {
		logrus.Warnf("VM %s: rootfs copy failed (%v), using shared image for key inject", vmID, err)
	} else {
		rootfsPath = dst
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
