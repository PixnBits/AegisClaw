package config

import (
	"os"
	"path/filepath"
	"testing"
)

func clearProfileEnv(t *testing.T) {
	t.Helper()
	keys := []string{
		"AEGIS_ENV_FILE",
		"AEGIS_COLLAB_TRACE",
		"AEGIS_DEFAULT_MODEL",
		"AEGIS_PM_MODEL",
		"AEGIS_ROOTFS_DIR",
		"AEGIS_KERNEL_PATH",
		"AEGIS_BOOT_TIMING",
		"AEGIS_DEBUG",
		"SUDO_USER",
	}
	for _, key := range keys {
		t.Setenv(key, "")
		os.Unsetenv(key)
	}
}

func TestParseProfileEnvLine(t *testing.T) {
	cases := []struct {
		in  string
		key string
		val string
		ok  bool
	}{
		{in: "", ok: false},
		{in: "   ", ok: false},
		{in: "# comment", ok: false},
		{in: "  # indented", ok: false},
		{in: "AEGIS_DEBUG=1", key: "AEGIS_DEBUG", val: "1", ok: true},
		{in: "AEGIS_DEBUG=\"1\"", key: "AEGIS_DEBUG", val: "1", ok: true},
		{in: "export AEGIS_PM_MODEL=\"qwen3.6:35b\"", key: "AEGIS_PM_MODEL", val: "qwen3.6:35b", ok: true},
		{in: "AEGIS_ROOTFS_DIR=\"/tmp/my rootfs\"", key: "AEGIS_ROOTFS_DIR", val: "/tmp/my rootfs", ok: true},
		{in: "AEGIS_KERNEL_PATH=/tmp/my kernel", key: "AEGIS_KERNEL_PATH", val: "/tmp/my kernel", ok: true},
		{in: "not a line", ok: false},
		{in: "=nokey", ok: false},
		{in: "AEGIS_DEBUG", ok: false},
		{in: "AEGIS_DEBUG=", key: "AEGIS_DEBUG", val: "", ok: true},
		{in: "AEGIS_DEBUG=\"\"", key: "AEGIS_DEBUG", val: "", ok: true},
	}
	for _, tc := range cases {
		key, val, ok := parseProfileEnvLine(tc.in)
		if ok != tc.ok || key != tc.key || val != tc.val {
			t.Errorf("parse %q: got (%q, %q, %v), want (%q, %q, %v)", tc.in, key, val, ok, tc.key, tc.val, tc.ok)
		}
	}
}

func TestLoadProfileEnvOverrideUnsetCommentsAndQuotes(t *testing.T) {
	clearProfileEnv(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "profile.env")
	body := "" +
		"# profiling settings\n" +
		"\n" +
		"AEGIS_COLLAB_TRACE=1\n" +
		"AEGIS_DEFAULT_MODEL=\"from-file\"\n" +
		"export AEGIS_PM_MODEL=\"qwen3.6:35b\"\n" +
		"AEGIS_ROOTFS_DIR=\"/tmp/arm rootfs\"\n" +
		"AEGIS_DEBUG=\n" +
		"PATH=/should-not-apply\n" +
		"LD_PRELOAD=/evil.so\n" +
		"NOT_AEGIS=1\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AEGIS_ENV_FILE", path)
	t.Setenv("AEGIS_DEFAULT_MODEL", "keep-me")
	// Empty is treated as unset and may be filled from the file.
	t.Setenv("AEGIS_PM_MODEL", "")
	os.Unsetenv("AEGIS_PM_MODEL")
	os.Unsetenv("AEGIS_COLLAB_TRACE")
	os.Unsetenv("AEGIS_ROOTFS_DIR")
	os.Unsetenv("AEGIS_DEBUG")
	pathBefore := os.Getenv("PATH")
	preloadBefore := os.Getenv("LD_PRELOAD")

	gotPath, applied, err := LoadProfileEnv()
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != path {
		t.Fatalf("path %q, want %q", gotPath, path)
	}
	if os.Getenv("AEGIS_DEFAULT_MODEL") != "keep-me" {
		t.Fatalf("explicit model overridden: %q", os.Getenv("AEGIS_DEFAULT_MODEL"))
	}
	if os.Getenv("AEGIS_PM_MODEL") != "qwen3.6:35b" {
		t.Fatalf("unset PM model not loaded: %q", os.Getenv("AEGIS_PM_MODEL"))
	}
	if os.Getenv("AEGIS_COLLAB_TRACE") != "1" {
		t.Fatalf("trace = %q", os.Getenv("AEGIS_COLLAB_TRACE"))
	}
	if os.Getenv("AEGIS_ROOTFS_DIR") != "/tmp/arm rootfs" {
		t.Fatalf("rootfs = %q", os.Getenv("AEGIS_ROOTFS_DIR"))
	}
	if _, ok := os.LookupEnv("AEGIS_DEBUG"); ok {
		t.Fatalf("empty file value set AEGIS_DEBUG=%q", os.Getenv("AEGIS_DEBUG"))
	}
	if os.Getenv("PATH") != pathBefore {
		t.Fatalf("PATH changed from %q to %q", pathBefore, os.Getenv("PATH"))
	}
	if os.Getenv("LD_PRELOAD") != preloadBefore {
		t.Fatalf("LD_PRELOAD changed from %q to %q", preloadBefore, os.Getenv("LD_PRELOAD"))
	}
	if os.Getenv("NOT_AEGIS") != "" {
		t.Fatalf("non-allowlisted key applied: %q", os.Getenv("NOT_AEGIS"))
	}
	wantApplied := []string{"AEGIS_COLLAB_TRACE", "AEGIS_PM_MODEL", "AEGIS_ROOTFS_DIR"}
	if len(applied) != len(wantApplied) {
		t.Fatalf("applied %v, want %v", applied, wantApplied)
	}
	for i := range wantApplied {
		if applied[i] != wantApplied[i] {
			t.Fatalf("applied %v, want %v", applied, wantApplied)
		}
	}
}

func TestLoadProfileEnvMissingFileIsNoop(t *testing.T) {
	clearProfileEnv(t)
	missing := filepath.Join(t.TempDir(), "no-such-profile.env")
	t.Setenv("AEGIS_ENV_FILE", missing)
	t.Setenv("AEGIS_COLLAB_TRACE", "already")

	path, applied, err := LoadProfileEnv()
	if err != nil {
		t.Fatal(err)
	}
	if path != missing {
		t.Fatalf("path %q", path)
	}
	if applied != nil {
		t.Fatalf("applied %v, want nil", applied)
	}
	if os.Getenv("AEGIS_COLLAB_TRACE") != "already" {
		t.Fatalf("trace changed: %q", os.Getenv("AEGIS_COLLAB_TRACE"))
	}
}

func TestLoadProfileEnvDefaultPathUsesHome(t *testing.T) {
	clearProfileEnv(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	os.Unsetenv("AEGIS_ENV_FILE")
	os.Unsetenv("SUDO_USER")
	os.Unsetenv("AEGIS_BOOT_TIMING")

	dir := filepath.Join(home, ".aegis")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "profile.env")
	if err := os.WriteFile(path, []byte("AEGIS_BOOT_TIMING=1\n# ignored\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := ProfileEnvPath(); got != path {
		t.Fatalf("ProfileEnvPath %q, want %q", got, path)
	}
	if _, _, err := LoadProfileEnv(); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("AEGIS_BOOT_TIMING") != "1" {
		t.Fatalf("boot timing %q", os.Getenv("AEGIS_BOOT_TIMING"))
	}
}

func TestProfileEnvPathPrefersExplicitFile(t *testing.T) {
	clearProfileEnv(t)
	t.Setenv("AEGIS_ENV_FILE", "/tmp/explicit-profile.env")
	t.Setenv("HOME", t.TempDir())
	if got := ProfileEnvPath(); got != "/tmp/explicit-profile.env" {
		t.Fatalf("path %q", got)
	}
}
