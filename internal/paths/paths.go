package paths

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"

	"golang.org/x/sys/unix"
)

const (
	AppDirName = ".aegis"

	UserDirPerm      os.FileMode = 0700
	UserSharedPerm   os.FileMode = 0750
	SensitiveDirPerm os.FileMode = 0700
	RuntimeDirPerm   os.FileMode = 0750
)

// Layout contains the security-conscious filesystem layout from
// docs/specs/directory-layout.md.
type Layout struct {
	RootDir      string
	ConfigDir    string
	WorkspaceDir string
	CacheDir     string
	LogsDir      string
	GitDir       string
	VMDir        string
	DataDir      string
	StoreDir     string
	AuditDir     string
	RegistryDir  string
	ProposalDir  string
	SBOMDir      string
	SecretsDir   string
	SocketPath   string
}

// DefaultLayout returns the per-user default layout. Most data lives under
// ~/.aegis, while the privileged daemon socket is outside the home tree on
// Linux.
func DefaultLayout() (Layout, error) {
	home, err := ResolveHome()
	if err != nil {
		return Layout{}, err
	}
	root := filepath.Join(home, AppDirName)
	data := filepath.Join(root, "data")
	socket, err := DefaultSocketPath()
	if err != nil {
		return Layout{}, err
	}
	return Layout{
		RootDir:      root,
		ConfigDir:    filepath.Join(root, "config"),
		WorkspaceDir: filepath.Join(root, "workspace"),
		CacheDir:     filepath.Join(root, "cache"),
		LogsDir:      filepath.Join(root, "logs"),
		GitDir:       filepath.Join(root, "git"),
		VMDir:        filepath.Join(root, "vm"),
		DataDir:      data,
		StoreDir:     filepath.Join(data, "store"),
		AuditDir:     filepath.Join(data, "audit"),
		RegistryDir:  filepath.Join(data, "registry"),
		ProposalDir:  filepath.Join(data, "registry", "proposals"),
		SBOMDir:      filepath.Join(data, "sbom"),
		SecretsDir:   filepath.Join(root, "secrets"),
		SocketPath:   socket,
	}, nil
}

// ResolveHome mirrors config's sudo-aware home resolution without importing
// config, avoiding an import cycle.
func ResolveHome() (string, error) {
	if sudoUser := os.Getenv("SUDO_USER"); sudoUser != "" {
		resolvedUser, err := user.Lookup(sudoUser)
		if err == nil && resolvedUser.HomeDir != "" {
			return resolvedUser.HomeDir, nil
		}
	}
	return os.UserHomeDir()
}

// DefaultSocketPath returns the privileged daemon socket path. On Linux it is
// always outside ~/.aegis and placed in /run/user/$UID/aegis/ (tmpfs). Other
// platforms fall back to ~/.aegis/run/daemon.sock for compatibility.
func DefaultSocketPath() (string, error) {
	if runtime.GOOS == "linux" {
		uid := os.Getuid()
		if sudoUser := os.Getenv("SUDO_USER"); sudoUser != "" {
			if u, err := user.Lookup(sudoUser); err == nil {
				if parsed, parseErr := strconv.Atoi(u.Uid); parseErr == nil {
					uid = parsed
				}
			}
		}
		return filepath.Join("/run", "user", strconv.Itoa(uid), "aegis", "daemon.sock"), nil
	}
	home, err := ResolveHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, AppDirName, "run", "daemon.sock"), nil
}

// EnsureSecureDirectories creates all known directories and verifies high
// sensitivity directories before privileged components use them.
func EnsureSecureDirectories(layout Layout) error {
	dirs := []struct {
		path string
		perm os.FileMode
	}{
		{layout.RootDir, UserDirPerm},
		{layout.ConfigDir, UserDirPerm},
		{layout.WorkspaceDir, UserDirPerm},
		{layout.CacheDir, UserDirPerm},
		{layout.LogsDir, UserSharedPerm},
		{layout.GitDir, UserSharedPerm},
		{layout.VMDir, UserSharedPerm},
		{layout.DataDir, UserDirPerm},
		{layout.StoreDir, SensitiveDirPerm},
		{layout.AuditDir, SensitiveDirPerm},
		{layout.RegistryDir, UserDirPerm},
		{layout.ProposalDir, UserDirPerm},
		{layout.SBOMDir, UserDirPerm},
		{layout.SecretsDir, SensitiveDirPerm},
	}
	for _, d := range dirs {
		if d.path == "" {
			continue
		}
		if err := os.MkdirAll(d.path, d.perm); err != nil {
			return fmt.Errorf("create %s: %w", d.path, err)
		}
		if err := os.Chmod(d.path, d.perm); err != nil {
			return fmt.Errorf("chmod %s: %w", d.path, err)
		}
	}
	if err := EnsureRuntimeDir(filepath.Dir(layout.SocketPath)); err != nil {
		return err
	}
	for _, p := range []string{layout.SecretsDir, layout.StoreDir, layout.AuditDir} {
		if err := VerifySensitiveDir(p); err != nil {
			return err
		}
	}
	return nil
}

// EnsureRuntimeDir creates the daemon socket parent and verifies it is not a
// symlink. On Linux, this path must be outside ~/.aegis.
func EnsureRuntimeDir(dir string) error {
	if dir == "" {
		return fmt.Errorf("runtime dir is required")
	}
	clean := filepath.Clean(dir)
	if runtime.GOOS == "linux" && clean == "/run" {
		return fmt.Errorf("runtime dir must be /run/user/$UID/aegis, not /run")
	}
	if err := os.MkdirAll(dir, RuntimeDirPerm); err != nil {
		return fmt.Errorf("create runtime dir %s: %w", dir, err)
	}
	if err := os.Chmod(dir, RuntimeDirPerm); err != nil {
		return fmt.Errorf("chmod runtime dir %s: %w", dir, err)
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return fmt.Errorf("lstat runtime dir %s: %w", dir, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("runtime dir %s must not be a symlink", dir)
	}
	return nil
}

// VerifySensitiveDir enforces ownership, mode, and O_NOFOLLOW traversal for
// secrets/, data/store/, and data/audit/.
func VerifySensitiveDir(path string) error {
	if path == "" {
		return fmt.Errorf("sensitive path is required")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("lstat sensitive dir %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("sensitive dir %s must not be a symlink", path)
	}
	if !info.IsDir() {
		return fmt.Errorf("sensitive path %s is not a directory", path)
	}
	if info.Mode().Perm() != SensitiveDirPerm {
		return fmt.Errorf("sensitive dir %s has mode %04o, want %04o", path, info.Mode().Perm(), SensitiveDirPerm)
	}
	if err := verifyOwner(path, os.Geteuid()); err != nil {
		return err
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("open sensitive dir %s with O_NOFOLLOW: %w", path, err)
	}
	_ = unix.Close(fd)
	return nil
}

// FixSecurePermissions repairs common permission drift for known directories.
func FixSecurePermissions(layout Layout) error {
	return EnsureSecureDirectories(layout)
}

func verifyOwner(path string, uid int) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat %s: %w", path, err)
	}
	stat, ok := info.Sys().(*unix.Stat_t)
	if !ok {
		return nil
	}
	if int(stat.Uid) != uid {
		return fmt.Errorf("%s owner uid=%d, want %d", path, stat.Uid, uid)
	}
	return nil
}
