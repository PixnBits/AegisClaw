package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	aegispaths "github.com/PixnBits/AegisClaw/internal/paths"
)

func TestDefaultConfigUsesAegisRootLayout(t *testing.T) {
	t.Setenv("SUDO_USER", "")
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg := DefaultConfig()
	root := filepath.Join(home, aegispaths.AppDirName)

	for name, path := range map[string]string{
		"audit.dir":          cfg.Audit.Dir,
		"vault.dir":          cfg.Vault.Dir,
		"workspace.dir":      cfg.Workspace.Dir,
		"proposal.store_dir": cfg.Proposal.StoreDir,
		"memory.dir":         cfg.Memory.Dir,
		"eventbus.dir":       cfg.EventBus.Dir,
		"worker.dir":         cfg.Worker.Dir,
		"lookup.dir":         cfg.Lookup.Dir,
	} {
		if !strings.HasPrefix(path, root+string(filepath.Separator)) {
			t.Fatalf("%s = %q, want under %q", name, path, root)
		}
	}

	if runtime.GOOS == "linux" && strings.HasPrefix(cfg.Daemon.SocketPath, root) {
		t.Fatalf("daemon socket must not be under ~/.aegis: %s", cfg.Daemon.SocketPath)
	}
}

func TestNormalizeConfigPathsMigratesOldRunSocket(t *testing.T) {
	defaults := DefaultConfig()
	cfg := defaults
	cfg.Daemon.SocketPath = "/run/aegisclaw.sock"

	normalizeConfigPaths(&cfg, defaults, nil)

	if cfg.Daemon.SocketPath == "/run/aegisclaw.sock" {
		t.Fatal("old /run/aegisclaw.sock default was not migrated")
	}
	if cfg.Daemon.SocketPath != defaults.Daemon.SocketPath {
		t.Fatalf("socket path = %q, want %q", cfg.Daemon.SocketPath, defaults.Daemon.SocketPath)
	}
}

func TestNormalizeConfigPathsMigratesOldDefaultDirs(t *testing.T) {
	t.Setenv("SUDO_USER", "")
	home := t.TempDir()
	t.Setenv("HOME", home)

	defaults := DefaultConfig()
	oldData := filepath.Join(home, ".local", "share", "aegisclaw")
	oldConfig := filepath.Join(home, ".config", "aegisclaw")
	oldWorkspace := filepath.Join(home, ".aegisclaw")
	cfg := defaults
	cfg.Audit.Dir = filepath.Join(oldData, "audit")
	cfg.Sandbox.StateDir = filepath.Join(oldData, "sandboxes")
	cfg.Sandbox.RegistryPath = filepath.Join(oldData, "registry.json")
	cfg.Proposal.StoreDir = filepath.Join(oldData, "proposals")
	cfg.Court.PersonaDir = filepath.Join(oldConfig, "personas")
	cfg.Vault.Dir = filepath.Join(oldConfig, "secrets")
	cfg.Workspace.Dir = filepath.Join(oldWorkspace, "workspace")

	normalizeConfigPaths(&cfg, defaults, nil)

	for name, gotWant := range map[string][2]string{
		"audit.dir":          {cfg.Audit.Dir, defaults.Audit.Dir},
		"sandbox.state_dir":  {cfg.Sandbox.StateDir, defaults.Sandbox.StateDir},
		"sandbox.registry":   {cfg.Sandbox.RegistryPath, defaults.Sandbox.RegistryPath},
		"proposal.store_dir": {cfg.Proposal.StoreDir, defaults.Proposal.StoreDir},
		"court.persona_dir":  {cfg.Court.PersonaDir, defaults.Court.PersonaDir},
		"vault.dir":          {cfg.Vault.Dir, defaults.Vault.Dir},
		"workspace.dir":      {cfg.Workspace.Dir, defaults.Workspace.Dir},
	} {
		if gotWant[0] != gotWant[1] {
			t.Fatalf("%s = %q, want %q", name, gotWant[0], gotWant[1])
		}
	}
}

func TestNormalizeConfigPathsPreservesReadableLegacyData(t *testing.T) {
	t.Setenv("SUDO_USER", "")
	home := t.TempDir()
	t.Setenv("HOME", home)

	defaults := DefaultConfig()
	oldData := filepath.Join(home, ".local", "share", "aegisclaw")
	oldAudit := filepath.Join(oldData, "audit")
	if err := os.MkdirAll(oldAudit, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldAudit, "kernel.merkle.jsonl"), []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := defaults
	cfg.Audit.Dir = oldAudit

	normalizeConfigPaths(&cfg, defaults, nil)

	if cfg.Audit.Dir != oldAudit {
		t.Fatalf("readable legacy audit path was not preserved: got %q want %q", cfg.Audit.Dir, oldAudit)
	}
}

func TestNormalizeConfigPathsMigratesEmptyLegacyDir(t *testing.T) {
	t.Setenv("SUDO_USER", "")
	home := t.TempDir()
	t.Setenv("HOME", home)

	defaults := DefaultConfig()
	oldAudit := filepath.Join(home, ".local", "share", "aegisclaw", "audit")
	if err := os.MkdirAll(oldAudit, 0700); err != nil {
		t.Fatal(err)
	}
	cfg := defaults
	cfg.Audit.Dir = oldAudit

	normalizeConfigPaths(&cfg, defaults, nil)

	if cfg.Audit.Dir != defaults.Audit.Dir {
		t.Fatalf("empty legacy audit path = %q, want secure default %q", cfg.Audit.Dir, defaults.Audit.Dir)
	}
}

func TestNormalizeConfigPathsMigratesLegacySymlink(t *testing.T) {
	t.Setenv("SUDO_USER", "")
	home := t.TempDir()
	t.Setenv("HOME", home)

	defaults := DefaultConfig()
	oldAudit := filepath.Join(home, ".local", "share", "aegisclaw", "audit")
	if err := os.MkdirAll(filepath.Dir(oldAudit), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), oldAudit); err != nil {
		t.Fatal(err)
	}
	cfg := defaults
	cfg.Audit.Dir = oldAudit

	normalizeConfigPaths(&cfg, defaults, nil)

	if cfg.Audit.Dir != defaults.Audit.Dir {
		t.Fatalf("symlink legacy audit path = %q, want secure default %q", cfg.Audit.Dir, defaults.Audit.Dir)
	}
}
