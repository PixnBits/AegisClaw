package provision

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"go.uber.org/zap"
)

// AssetConfig holds paths to assets that must exist before sandboxes can run.
type AssetConfig struct {
	KernelPath string // vmlinux binary (e.g. /var/lib/aegisclaw/vmlinux)
	RootfsPath string // rootfs template (e.g. /var/lib/aegisclaw/rootfs-templates/alpine.ext4)
}

// EnsureAssets verifies that the Firecracker kernel and rootfs template exist,
// provisioning any that are missing. Downloads the vmlinux kernel from
// Firecracker's quickstart artifacts and builds an Alpine rootfs with the
// guest-agent binary as init. Must be called as root.
func EnsureAssets(ctx context.Context, cfg AssetConfig, logger *zap.Logger) error {
	for _, p := range []string{cfg.KernelPath, cfg.RootfsPath} {
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			return fmt.Errorf("create directory %s: %w", filepath.Dir(p), err)
		}
	}
	if err := ensureKernel(ctx, cfg.KernelPath, logger); err != nil {
		return err
	}
	return ensureRootfs(ctx, cfg.RootfsPath, logger)
}

func ensureKernel(ctx context.Context, kernelPath string, logger *zap.Logger) error {
	if _, err := os.Stat(kernelPath); err == nil {
		return nil
	}

	arch := runtime.GOARCH
	switch arch {
	case "amd64":
		arch = "x86_64"
	case "arm64":
		arch = "aarch64"
	}

	// Use the Firecracker CI kernel which has CONFIG_VIRTIO_VSOCKETS=y built-in.
	// The quickstart kernel only has vsock as a module which doesn't work for
	// guests running without a module-loading init system.
	url := fmt.Sprintf(
		"https://s3.amazonaws.com/spec.ccfc.min/firecracker-ci/v1.11/%s/vmlinux-5.10.225",
		arch,
	)

	fmt.Printf("  Downloading vmlinux kernel for %s...\n", arch)
	logger.Info("downloading vmlinux kernel", zap.String("url", url), zap.String("dest", kernelPath))

	if err := downloadFile(ctx, url, kernelPath); err != nil {
		return fmt.Errorf("download vmlinux from %s: %w", url, err)
	}
	if err := os.Chmod(kernelPath, 0644); err != nil {
		return err
	}

	fmt.Println("  vmlinux kernel ready.")
	return nil
}

func ensureRootfs(ctx context.Context, rootfsPath string, logger *zap.Logger) error {
	if _, err := os.Stat(rootfsPath); err == nil {
		return nil
	}

	for _, tool := range []string{"dd", "mkfs.ext4", "mount", "umount"} {
		if _, err := exec.LookPath(tool); err != nil {
			return fmt.Errorf("rootfs build requires %q — install e2fsprogs", tool)
		}
	}

	agentPath, err := findGuestAgent()
	if err != nil {
		return fmt.Errorf(
			"guest-agent binary not found next to the aegisclaw binary or in the working directory\n" +
				"Build it first:\n  go build -o guest-agent ./cmd/guest-agent",
		)
	}
	logger.Info("found guest-agent", zap.String("path", agentPath))

	fmt.Println("  Building rootfs template (Alpine + guest-agent)...")
	if err := buildRootfs(ctx, rootfsPath, agentPath, logger); err != nil {
		return fmt.Errorf("build rootfs: %w", err)
	}

	fmt.Println("  rootfs template ready.")
	return nil
}

func findGuestAgent() (string, error) {
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), "guest-agent")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	if cwd, err := os.Getwd(); err == nil {
		candidate := filepath.Join(cwd, "guest-agent")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("not found")
}

const (
	rootfsSizeMB  = 256
	alpineVersion = "3.21"
)

func buildRootfs(ctx context.Context, outputPath, guestAgentPath string, logger *zap.Logger) error {
	workdir, err := os.MkdirTemp("", "aegisclaw-rootfs-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(workdir)

	mountpoint := filepath.Join(workdir, "mnt")
	if err := os.MkdirAll(mountpoint, 0755); err != nil {
		return err
	}

	imgPath := filepath.Join(workdir, "rootfs.ext4")

	// Create ext4 image.
	if err := sh(ctx, "dd", "if=/dev/zero", "of="+imgPath,
		"bs=1M", fmt.Sprintf("count=%d", rootfsSizeMB), "status=none"); err != nil {
		return fmt.Errorf("create image: %w", err)
	}
	if err := sh(ctx, "mkfs.ext4", "-F", "-q", "-L", "aegisclaw", imgPath); err != nil {
		return fmt.Errorf("mkfs.ext4: %w", err)
	}

	// Mount.
	if err := sh(ctx, "mount", "-o", "loop", imgPath, mountpoint); err != nil {
		return fmt.Errorf("mount: %w", err)
	}
	unmounted := false
	unmount := func() {
		if !unmounted {
			sh(ctx, "umount", mountpoint)
			unmounted = true
		}
	}
	defer unmount()

	// Download and extract Alpine minirootfs.
	arch := "x86_64"
	if runtime.GOARCH == "arm64" {
		arch = "aarch64"
	}
	alpineURL := fmt.Sprintf(
		"https://dl-cdn.alpinelinux.org/alpine/v%s/releases/%s/alpine-minirootfs-%s.0-%s.tar.gz",
		alpineVersion, arch, alpineVersion, arch,
	)
	tarball := filepath.Join(workdir, "alpine.tar.gz")
	fmt.Printf("    Downloading Alpine v%s minirootfs...\n", alpineVersion)
	if err := downloadFile(ctx, alpineURL, tarball); err != nil {
		return fmt.Errorf("download Alpine minirootfs: %w", err)
	}
	if err := sh(ctx, "tar", "xzf", tarball, "-C", mountpoint); err != nil {
		return fmt.Errorf("extract Alpine: %w", err)
	}

	// Create essential directories.
	for _, d := range []string{
		"dev", "proc", "sys", "tmp", "run", "workspace",
		"sbin", "etc", "run/secrets",
	} {
		os.MkdirAll(filepath.Join(mountpoint, d), 0755)
	}

	// Install guest-agent as init.
	fmt.Println("    Installing guest-agent as init...")
	destAgent := filepath.Join(mountpoint, "sbin", "guest-agent")
	if err := copyFile(guestAgentPath, destAgent, 0755); err != nil {
		return fmt.Errorf("install guest-agent: %w", err)
	}
	if err := os.Symlink("/sbin/guest-agent", filepath.Join(mountpoint, "init")); err != nil {
		return fmt.Errorf("symlink init: %w", err)
	}

	// Configure basic system files.
	os.WriteFile(filepath.Join(mountpoint, "etc/resolv.conf"), []byte("nameserver 10.0.0.1\n"), 0644)
	os.WriteFile(filepath.Join(mountpoint, "etc/hostname"), []byte("aegisclaw-sandbox\n"), 0644)
	os.WriteFile(filepath.Join(mountpoint, "etc/passwd"),
		[]byte("root:x:0:0:root:/workspace:/bin/sh\nnobody:x:65534:65534:nobody:/:/sbin/nologin\n"), 0644)
	os.WriteFile(filepath.Join(mountpoint, "etc/group"),
		[]byte("root:x:0:\nnobody:x:65534:\n"), 0644)

	// Minimize image size.
	for _, d := range []string{"var/cache", "usr/share/doc", "usr/share/man", "usr/share/info"} {
		os.RemoveAll(filepath.Join(mountpoint, d))
	}

	// Permissions.
	os.Chmod(mountpoint, 0755)
	os.Chmod(filepath.Join(mountpoint, "tmp"), 01777)
	os.Chmod(filepath.Join(mountpoint, "run/secrets"), 0700)

	// Unmount before shrinking.
	unmount()

	// Shrink (best-effort).
	sh(ctx, "e2fsck", "-f", "-y", imgPath)
	sh(ctx, "resize2fs", "-M", imgPath)

	// Install to final location.
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return err
	}
	return copyFile(imgPath, outputPath, 0444)
}

// maxDownloadBytes caps individual asset downloads to prevent runaway
// transfers from a compromised or misbehaving server.
//   - vmlinux kernel: typically 15–20 MiB
//   - Alpine minirootfs: typically 3–5 MiB
//   - We use a generous 512 MiB ceiling so legitimate oversized kernels still work.
const maxDownloadBytes = 512 * 1024 * 1024

func downloadFile(ctx context.Context, url, destPath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}

	f, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := io.Copy(f, io.LimitReader(resp.Body, maxDownloadBytes)); err != nil {
		os.Remove(destPath)
		return err
	}

	// Detect silent truncation: if Content-Length is set and we got fewer bytes,
	// the download may be incomplete.  This guards against a misbehaving server
	// returning a truncated response without an error.
	if cl := resp.ContentLength; cl > 0 {
		if fi, statErr := f.Stat(); statErr == nil && fi.Size() < cl {
			os.Remove(destPath)
			return fmt.Errorf("download truncated: got %d bytes, expected %d from %s", fi.Size(), cl, url)
		}
	}
	return nil
}

func copyFile(src, dst string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

func sh(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Run()
}
