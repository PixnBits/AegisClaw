package paths

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

const (
	AppDirName = ".aegis"

	UserDirPerm      os.FileMode = 0700
	UserSharedPerm   os.FileMode = 0750
	SensitiveDirPerm os.FileMode = 0700
	RuntimeDirPerm   os.FileMode = 0700

	// Socket permissions per 04-unix-socket-hardening.md
	SocketPermOwner os.FileMode = 0600 // strict owner-only (current default)
	SocketPermGroup os.FileMode = 0750 // owner + aegis group (preferred when group exists)

	RuntimeUIDEnv = "AEGIS_RUNTIME_UID"
	RuntimeGIDEnv = "AEGIS_RUNTIME_GID"

	AegisGroupName = "aegis" // dedicated non-root group for socket ownership (Task 1)
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

// RuntimeOwner is the user that should own the per-user runtime directory and
// socket. When a root daemon is launched via sudo, this is SUDO_USER so the
// unprivileged CLI can connect without making the socket world-writable.
type RuntimeOwner struct {
	UID int
	GID int
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
// Per 04-unix-socket-hardening: supports dedicated 'aegis' group (0750) when present;
// abstract socket (@aegis-daemon) available via DefaultAbstractSocketPath().
func DefaultSocketPath() (string, error) {
	if runtime.GOOS == "linux" {
		owner, err := RuntimeSocketOwner()
		if err != nil {
			return "", err
		}
		return filepath.Join("/run", "user", strconv.Itoa(owner.UID), "aegis", "daemon.sock"), nil
	}
	home, err := ResolveHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, AppDirName, "run", "daemon.sock"), nil
}

// DefaultAbstractSocketPath returns an abstract Unix socket name (no filesystem entry).
// Preferred for even tighter isolation (no path to race or mount). Use with net.Listen("unix", path).
func DefaultAbstractSocketPath() string {
	return "@aegis-daemon"
}

// RuntimeSocketOwner returns the intended owner of /run/user/$UID/aegis and
// daemon.sock. Access is owner-only; the GID is retained only so root can set a
// consistent ownership tuple without granting group access.
func RuntimeSocketOwner() (RuntimeOwner, error) {
	uid, gid := os.Getuid(), os.Getgid()
	if explicitUID := os.Getenv(RuntimeUIDEnv); explicitUID != "" {
		parsedUID, err := strconv.Atoi(explicitUID)
		if err != nil {
			return RuntimeOwner{}, fmt.Errorf("parse %s: %w", RuntimeUIDEnv, err)
		}
		uid = parsedUID
		if explicitGID := os.Getenv(RuntimeGIDEnv); explicitGID != "" {
			parsedGID, err := strconv.Atoi(explicitGID)
			if err != nil {
				return RuntimeOwner{}, fmt.Errorf("parse %s: %w", RuntimeGIDEnv, err)
			}
			gid = parsedGID
		} else if u, err := user.LookupId(explicitUID); err == nil {
			parsedGID, err := strconv.Atoi(u.Gid)
			if err != nil {
				return RuntimeOwner{}, fmt.Errorf("parse gid for uid %q: %w", explicitUID, err)
			}
			gid = parsedGID
		}
		return RuntimeOwner{UID: uid, GID: gid}, nil
	}
	if sudoUser := os.Getenv("SUDO_USER"); sudoUser != "" {
		u, err := user.Lookup(sudoUser)
		if err != nil {
			return RuntimeOwner{}, fmt.Errorf("lookup sudo user %q: %w", sudoUser, err)
		}
		parsedUID, err := strconv.Atoi(u.Uid)
		if err != nil {
			return RuntimeOwner{}, fmt.Errorf("parse uid for %q: %w", sudoUser, err)
		}
		parsedGID, err := strconv.Atoi(u.Gid)
		if err != nil {
			return RuntimeOwner{}, fmt.Errorf("parse gid for %q: %w", sudoUser, err)
		}
		uid, gid = parsedUID, parsedGID
	}
	return RuntimeOwner{UID: uid, GID: gid}, nil
}

// EnsureSecureDirectories creates missing directories and verifies existing
// high-sensitivity directories before privileged components use them. It never
// repairs insecure existing sensitive directories; use FixSecurePermissions for that.
func EnsureSecureDirectories(layout Layout) error {
	for _, d := range layoutDirs(layout) {
		if err := ensureDir(d.path, d.perm, false, d.strict); err != nil {
			return err
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
// symlink. On Linux, the parent must be a dedicated "aegis" directory so startup
// never repairs broad parents such as /tmp or /run/user/$UID.
// Supports /run/aegis/ for group-based layouts (future 04 enhancement).
func EnsureRuntimeDir(dir string) error {
	if dir == "" {
		return fmt.Errorf("runtime dir is required")
	}
	clean := filepath.Clean(dir)
	if runtime.GOOS == "linux" && clean == "/run" {
		return fmt.Errorf("runtime dir must be /run/user/$UID/aegis, not /run")
	}
	if err := ensureRuntimeDir(dir); err != nil {
		return err
	}
	return nil
}

// SetRuntimeSocketOwner applies the runtime owner and hardened permissions
// to the bound daemon socket per 04-unix-socket-hardening.md (Task 1).
// Prefers 0750 + 'aegis' group ownership when the group exists and running as root;
// falls back to strict 0600 owner-only.
func SetRuntimeSocketOwner(path string) error {
	owner, err := RuntimeSocketOwner()
	if err != nil {
		return err
	}
	if err := chownIfRoot(path, owner); err != nil {
		return err
	}

	// Dedicated non-root 'aegis' group support (Task 1)
	if gid, err := AegisGroupGID(); err == nil && os.Geteuid() == 0 {
		if chErr := os.Chown(path, owner.UID, gid); chErr == nil {
			return os.Chmod(path, SocketPermGroup)
		}
	}
	return os.Chmod(path, SocketPermOwner)
}

// AegisGroupGID returns the GID of the dedicated 'aegis' group if it exists.
// Used for 0750 group-owned socket permissions (non-root CLI + daemon group access).
func AegisGroupGID() (int, error) {
	g, err := user.LookupGroup(AegisGroupName)
	if err != nil {
		return -1, fmt.Errorf("lookup group %s: %w (create with: groupadd %s)", AegisGroupName, err, AegisGroupName)
	}
	gid, err := strconv.Atoi(g.Gid)
	if err != nil {
		return -1, fmt.Errorf("parse gid for group %s: %w", AegisGroupName, err)
	}
	return gid, nil
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
	for _, d := range layoutDirs(layout) {
		if err := ensureDir(d.path, d.perm, true, d.strict); err != nil {
			return err
		}
	}
	if err := fixRuntimeDir(filepath.Dir(layout.SocketPath)); err != nil {
		return err
	}
	return nil
}

func fixRuntimeDir(dir string) error {
	if dir == "" {
		return fmt.Errorf("runtime dir is required")
	}
	clean := filepath.Clean(dir)
	if runtime.GOOS == "linux" && clean == "/run" {
		return fmt.Errorf("runtime dir must be /run/user/$UID/aegis, not /run")
	}
	if err := ensureRuntimeDir(dir); err != nil {
		return err
	}
	return nil
}

type layoutDir struct {
	path   string
	perm   os.FileMode
	strict bool
}

func layoutDirs(layout Layout) []layoutDir {
	return []layoutDir{
		{layout.RootDir, UserDirPerm, false},
		{layout.ConfigDir, UserDirPerm, false},
		{layout.WorkspaceDir, UserDirPerm, false},
		{layout.CacheDir, UserDirPerm, false},
		{layout.LogsDir, UserSharedPerm, false},
		{layout.GitDir, UserSharedPerm, false},
		{layout.VMDir, UserSharedPerm, false},
		{layout.DataDir, UserDirPerm, false},
		{layout.StoreDir, SensitiveDirPerm, true},
		{layout.AuditDir, SensitiveDirPerm, true},
		{layout.RegistryDir, UserDirPerm, false},
		{layout.ProposalDir, UserDirPerm, false},
		{layout.SBOMDir, UserDirPerm, false},
		{layout.SecretsDir, SensitiveDirPerm, true},
	}
}

func ensureDir(path string, perm os.FileMode, repair, strict bool) error {
	if path == "" {
		return nil
	}
	fd, created, err := openOrCreateDirNoFollow(path, perm)
	if err != nil {
		return err
	}
	defer unix.Close(fd) //nolint:errcheck
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return fmt.Errorf("stat %s: %w", path, err)
	}
	got := os.FileMode(stat.Mode).Perm()
	if created || got != perm {
		if !created && strict && !repair {
			return fmt.Errorf("%s has mode %04o, want %04o", path, got, perm)
		}
		if (created || repair) && got != perm {
			if err := unix.Fchmod(fd, uint32(perm)); err != nil {
				return fmt.Errorf("chmod %s: %w", path, err)
			}
		}
	}
	return nil
}

func ensureRuntimeDir(path string) error {
	if runtime.GOOS == "linux" && filepath.Base(filepath.Clean(path)) != "aegis" {
		return fmt.Errorf("runtime dir must be a dedicated 'aegis' directory, got %s", path)
	}
	fd, created, err := openOrCreateDirNoFollow(path, RuntimeDirPerm)
	if err != nil {
		return err
	}
	defer unix.Close(fd) //nolint:errcheck
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return fmt.Errorf("stat runtime dir %s: %w", path, err)
	}
	if got := os.FileMode(stat.Mode).Perm(); created || got != RuntimeDirPerm {
		if err := unix.Fchmod(fd, uint32(RuntimeDirPerm)); err != nil {
			return fmt.Errorf("chmod %s: %w", path, err)
		}
	}
	owner, err := RuntimeSocketOwner()
	if err != nil {
		return err
	}
	if os.Geteuid() == 0 {
		if err := unix.Fchown(fd, owner.UID, owner.GID); err != nil {
			return fmt.Errorf("set runtime dir owner %s: %w", path, err)
		}
	}
	return nil
}

func openOrCreateDirNoFollow(path string, perm os.FileMode) (int, bool, error) {
	clean, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return -1, false, err
	}
	root, err := unix.Open(string(filepath.Separator), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, false, err
	}
	parent := root
	created := false
	rest := clean[len(filepath.VolumeName(clean)):]
	for _, part := range splitPath(rest) {
		next, err := unix.Openat(parent, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if err != nil && os.IsNotExist(err) {
			if mkdirErr := unix.Mkdirat(parent, part, uint32(perm)); mkdirErr != nil && !os.IsExist(mkdirErr) {
				unix.Close(parent) //nolint:errcheck
				return -1, false, fmt.Errorf("create %s: %w", clean, mkdirErr)
			}
			created = true
			next, err = unix.Openat(parent, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		}
		if err != nil {
			unix.Close(parent) //nolint:errcheck
			return -1, false, fmt.Errorf("open %s without following symlinks: %w", clean, err)
		}
		unix.Close(parent) //nolint:errcheck
		parent = next
	}
	return parent, created, nil
}

func splitPath(path string) []string {
	trimmed := filepath.Clean(path)
	trimmed = strings.TrimPrefix(trimmed, string(filepath.Separator))
	if trimmed == "." || trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, string(filepath.Separator))
}

func chownIfRoot(path string, owner RuntimeOwner) error {
	if os.Geteuid() != 0 {
		return nil
	}
	return os.Chown(path, owner.UID, owner.GID)
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
