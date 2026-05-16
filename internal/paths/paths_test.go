package paths

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestEnsureSecureDirectoriesCreatesExpectedModes(t *testing.T) {
	root := t.TempDir()
	layout := Layout{
		RootDir:      filepath.Join(root, ".aegis"),
		ConfigDir:    filepath.Join(root, ".aegis", "config"),
		WorkspaceDir: filepath.Join(root, ".aegis", "workspace"),
		CacheDir:     filepath.Join(root, ".aegis", "cache"),
		LogsDir:      filepath.Join(root, ".aegis", "logs"),
		GitDir:       filepath.Join(root, ".aegis", "git"),
		VMDir:        filepath.Join(root, ".aegis", "vm"),
		DataDir:      filepath.Join(root, ".aegis", "data"),
		StoreDir:     filepath.Join(root, ".aegis", "data", "store"),
		AuditDir:     filepath.Join(root, ".aegis", "data", "audit"),
		RegistryDir:  filepath.Join(root, ".aegis", "data", "registry"),
		ProposalDir:  filepath.Join(root, ".aegis", "data", "registry", "proposals"),
		SBOMDir:      filepath.Join(root, ".aegis", "data", "sbom"),
		SecretsDir:   filepath.Join(root, ".aegis", "secrets"),
		SocketPath:   filepath.Join(root, "run", "aegis", "daemon.sock"),
	}
	if err := EnsureSecureDirectories(layout); err != nil {
		t.Fatalf("EnsureSecureDirectories: %v", err)
	}
	for _, p := range []string{layout.SecretsDir, layout.StoreDir, layout.AuditDir} {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatalf("stat %s: %v", p, err)
		}
		if got := info.Mode().Perm(); got != SensitiveDirPerm {
			t.Fatalf("%s mode = %04o, want %04o", p, got, SensitiveDirPerm)
		}
		if err := VerifySensitiveDir(p); err != nil {
			t.Fatalf("VerifySensitiveDir(%s): %v", p, err)
		}
	}
}

func TestVerifySensitiveDirRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "secrets")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := VerifySensitiveDir(link); err == nil {
		t.Fatal("expected symlink sensitive dir to be rejected")
	}
}

func TestVerifySensitiveDirRejectsLoosePermissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "secrets")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := VerifySensitiveDir(dir); err == nil {
		t.Fatal("expected loose permissions to be rejected")
	}
}

func TestEnsureSecureDirectoriesDoesNotRepairExistingLoosePermissions(t *testing.T) {
	root := t.TempDir()
	layout := testLayout(root)
	if err := EnsureSecureDirectories(layout); err != nil {
		t.Fatalf("initial ensure: %v", err)
	}
	if err := os.Chmod(layout.SecretsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := EnsureSecureDirectories(layout); err == nil {
		t.Fatal("expected insecure existing secrets dir to be refused, not repaired")
	}
	info, err := os.Stat(layout.SecretsDir)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0755 {
		t.Fatalf("EnsureSecureDirectories repaired mode to %04o; want unchanged 0755", got)
	}
}

func TestFixSecurePermissionsRepairsLoosePermissions(t *testing.T) {
	root := t.TempDir()
	layout := testLayout(root)
	if err := EnsureSecureDirectories(layout); err != nil {
		t.Fatalf("initial ensure: %v", err)
	}
	if err := os.Chmod(layout.SecretsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := FixSecurePermissions(layout); err != nil {
		t.Fatalf("FixSecurePermissions: %v", err)
	}
	info, err := os.Stat(layout.SecretsDir)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != SensitiveDirPerm {
		t.Fatalf("mode = %04o, want %04o", got, SensitiveDirPerm)
	}
}

func TestEnsureRuntimeDirRejectsSymlinkBeforeChmod(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "runtime")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := EnsureRuntimeDir(link); err == nil {
		t.Fatal("expected runtime symlink to be rejected")
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0700 {
		t.Fatalf("target mode changed through symlink to %04o", got)
	}
}

func TestEnsureSecureDirectoriesRejectsSymlinkedParentBeforeMkdirAll(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "linked-parent")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	layout := testLayout(root)
	layout.RootDir = filepath.Join(link, ".aegis")
	layout.ConfigDir = filepath.Join(layout.RootDir, "config")
	layout.WorkspaceDir = filepath.Join(layout.RootDir, "workspace")
	layout.CacheDir = filepath.Join(layout.RootDir, "cache")
	layout.LogsDir = filepath.Join(layout.RootDir, "logs")
	layout.GitDir = filepath.Join(layout.RootDir, "git")
	layout.VMDir = filepath.Join(layout.RootDir, "vm")
	layout.DataDir = filepath.Join(layout.RootDir, "data")
	layout.StoreDir = filepath.Join(layout.DataDir, "store")
	layout.AuditDir = filepath.Join(layout.DataDir, "audit")
	layout.RegistryDir = filepath.Join(layout.DataDir, "registry")
	layout.ProposalDir = filepath.Join(layout.RegistryDir, "proposals")
	layout.SBOMDir = filepath.Join(layout.DataDir, "sbom")
	layout.SecretsDir = filepath.Join(layout.RootDir, "secrets")

	if err := EnsureSecureDirectories(layout); err == nil {
		t.Fatal("expected symlinked parent to be rejected")
	}
	if _, err := os.Stat(filepath.Join(target, ".aegis")); !os.IsNotExist(err) {
		t.Fatalf("MkdirAll followed symlinked parent; stat err=%v", err)
	}
}

func TestSetRuntimeSocketOwnerUsesOwnerOnlyMode(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "daemon.sock")
	if err := os.WriteFile(socket, []byte{}, 0666); err != nil {
		t.Fatal(err)
	}
	if err := SetRuntimeSocketOwner(socket); err != nil {
		t.Fatalf("SetRuntimeSocketOwner: %v", err)
	}
	info, err := os.Stat(socket)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf("socket mode = %04o, want 0600", got)
	}
}

func TestDefaultSocketPathLinuxNotUnderHome(t *testing.T) {
	path, err := DefaultSocketPath()
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "linux" {
		t.Skip("Linux-specific socket placement")
	}
	if !strings.HasPrefix(path, "/run/user/") {
		t.Fatalf("socket path = %q, want /run/user/$UID/aegis/daemon.sock", path)
	}
	home, err := ResolveHome()
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(path, filepath.Join(home, AppDirName)) {
		t.Fatalf("socket path must not live under ~/.aegis: %s", path)
	}
}

func TestEnsureRuntimeDirRejectsBareRun(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux-specific runtime dir policy")
	}
	if err := EnsureRuntimeDir("/run"); err == nil {
		t.Fatal("expected /run runtime dir to be rejected")
	}
}

func testLayout(root string) Layout {
	return Layout{
		RootDir:      filepath.Join(root, ".aegis"),
		ConfigDir:    filepath.Join(root, ".aegis", "config"),
		WorkspaceDir: filepath.Join(root, ".aegis", "workspace"),
		CacheDir:     filepath.Join(root, ".aegis", "cache"),
		LogsDir:      filepath.Join(root, ".aegis", "logs"),
		GitDir:       filepath.Join(root, ".aegis", "git"),
		VMDir:        filepath.Join(root, ".aegis", "vm"),
		DataDir:      filepath.Join(root, ".aegis", "data"),
		StoreDir:     filepath.Join(root, ".aegis", "data", "store"),
		AuditDir:     filepath.Join(root, ".aegis", "data", "audit"),
		RegistryDir:  filepath.Join(root, ".aegis", "data", "registry"),
		ProposalDir:  filepath.Join(root, ".aegis", "data", "registry", "proposals"),
		SBOMDir:      filepath.Join(root, ".aegis", "data", "sbom"),
		SecretsDir:   filepath.Join(root, ".aegis", "secrets"),
		SocketPath:   filepath.Join(root, "run", "aegis", "daemon.sock"),
	}
}
