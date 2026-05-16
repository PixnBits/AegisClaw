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
