package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/spf13/viper"
	"go.uber.org/zap"
)

// resetViper clears global viper state so Load tests do not leak defaults or
// config file paths between cases (DB-08 / legacy migration regression).
func resetViper(t *testing.T) {
	t.Helper()
	viper.Reset()
	t.Cleanup(func() { viper.Reset() })
}

// TestLoadPreservesExplicitDaemonSocketPath verifies there is no implicit
// migration that rewrites user-supplied absolute paths in config.yaml.
func TestLoadPreservesExplicitDaemonSocketPath(t *testing.T) {
	resetViper(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SUDO_USER", "")

	customSocket := filepath.Join(home, ".aegis", "run", "daemon.sock")
	configDir := filepath.Join(home, ".aegis", "config")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(configDir, "config.yaml")
	yaml := "daemon:\n  socket_path: " + customSocket + "\n"
	if err := os.WriteFile(configPath, []byte(yaml), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(zap.NewNop())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Daemon.SocketPath != customSocket {
		t.Fatalf("Daemon.SocketPath: got %q, want %q (expected no silent migration away from explicit path)", cfg.Daemon.SocketPath, customSocket)
	}
}

// TestLoadFreshInstallMatchesDefaultConfig verifies first-time Load writes and
// reads secure defaults aligned with DefaultConfig() for the same HOME.
func TestLoadFreshInstallMatchesDefaultConfig(t *testing.T) {
	resetViper(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SUDO_USER", "")

	cfg, err := Load(zap.NewNop())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := DefaultConfig()
	if cfg.Daemon.SocketPath != want.Daemon.SocketPath {
		t.Fatalf("Daemon.SocketPath: got %q, want %q", cfg.Daemon.SocketPath, want.Daemon.SocketPath)
	}
	if cfg.Audit.Dir != want.Audit.Dir {
		t.Fatalf("Audit.Dir: got %q, want %q", cfg.Audit.Dir, want.Audit.Dir)
	}
	if cfg.Vault.Dir != want.Vault.Dir {
		t.Fatalf("Vault.Dir: got %q, want %q", cfg.Vault.Dir, want.Vault.Dir)
	}
	if runtime.GOOS == "linux" && filepath.HasPrefix(cfg.Daemon.SocketPath, filepath.Join(home, ".aegis")+string(filepath.Separator)) {
		t.Fatalf("default socket must not live under ~/.aegis on Linux: %s", cfg.Daemon.SocketPath)
	}
}
