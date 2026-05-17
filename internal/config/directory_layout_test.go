package config

import (
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
