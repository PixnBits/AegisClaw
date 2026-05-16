package main

import (
	"path/filepath"
	"testing"

	"github.com/PixnBits/AegisClaw/internal/config"
	aegispaths "github.com/PixnBits/AegisClaw/internal/paths"
)

func TestDoctorHasFixPermissionsFlag(t *testing.T) {
	if doctorCmd.Flags().Lookup("fix-permissions") == nil {
		t.Fatal("doctor must expose --fix-permissions for directory-layout repair")
	}
}

func TestLayoutFromConfigDoesNotInferRootFromVaultDir(t *testing.T) {
	defaultLayout, err := aegispaths.DefaultLayout()
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	cfg.Vault.Dir = filepath.Join(t.TempDir(), "custom-secrets")
	cfg.Audit.Dir = filepath.Join(t.TempDir(), "custom-audit")

	layout := layoutFromConfig(&cfg)

	if layout.RootDir != defaultLayout.RootDir {
		t.Fatalf("root dir = %q, want default %q", layout.RootDir, defaultLayout.RootDir)
	}
	if layout.StoreDir != defaultLayout.StoreDir {
		t.Fatalf("store dir = %q, want default %q", layout.StoreDir, defaultLayout.StoreDir)
	}
}
