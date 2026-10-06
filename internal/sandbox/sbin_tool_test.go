package sandbox

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func stubSbinTool(t *testing.T, look func(string) (string, error), stat func(string) (os.FileInfo, error), dirs []string) {
	t.Helper()
	prevLook, prevStat, prevDirs := sbinLookPath, sbinStat, sbinDirs
	t.Cleanup(func() {
		sbinLookPath, sbinStat, sbinDirs = prevLook, prevStat, prevDirs
	})
	if look != nil {
		sbinLookPath = look
	}
	if stat != nil {
		sbinStat = stat
	}
	if dirs != nil {
		sbinDirs = dirs
	}
}

type fakeFileInfo struct{ mode os.FileMode }

func (f fakeFileInfo) Name() string       { return "tool" }
func (f fakeFileInfo) Size() int64        { return 0 }
func (f fakeFileInfo) Mode() os.FileMode  { return f.mode }
func (f fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (f fakeFileInfo) IsDir() bool        { return f.mode.IsDir() }
func (f fakeFileInfo) Sys() any           { return nil }

func TestFindSbinToolUsesLookPath(t *testing.T) {
	stubSbinTool(t, func(name string) (string, error) {
		if name != "mkfs.ext4" {
			t.Fatalf("looked up %q", name)
		}
		return "/custom/bin/mkfs.ext4", nil
	}, func(string) (os.FileInfo, error) {
		t.Fatal("stat should not run when LookPath succeeds")
		return nil, nil
	}, []string{"/absent"})

	got, err := FindSbinTool("mkfs.ext4")
	if err != nil {
		t.Fatal(err)
	}
	if got != "/custom/bin/mkfs.ext4" {
		t.Fatalf("got %q", got)
	}
}

func TestFindSbinToolFallsBackUsrSbinThenSbin(t *testing.T) {
	lookMiss := func(string) (string, error) { return "", errors.New("not in PATH") }

	t.Run("usr-sbin", func(t *testing.T) {
		stubSbinTool(t, lookMiss, func(path string) (os.FileInfo, error) {
			if path == "/usr/sbin/mkfs.ext4" {
				return fakeFileInfo{mode: 0755}, nil
			}
			return nil, os.ErrNotExist
		}, nil)
		got, err := FindSbinTool("mkfs.ext4")
		if err != nil {
			t.Fatal(err)
		}
		if got != "/usr/sbin/mkfs.ext4" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("sbin", func(t *testing.T) {
		stubSbinTool(t, lookMiss, func(path string) (os.FileInfo, error) {
			if path == "/sbin/mount" {
				return fakeFileInfo{mode: 0755}, nil
			}
			return nil, os.ErrNotExist
		}, nil)
		got, err := FindSbinTool("mount")
		if err != nil {
			t.Fatal(err)
		}
		if got != "/sbin/mount" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("skip non-executable and directory", func(t *testing.T) {
		stubSbinTool(t, lookMiss, func(path string) (os.FileInfo, error) {
			switch path {
			case "/usr/sbin/mkfs.ext4":
				return fakeFileInfo{mode: 0644}, nil
			case "/opt/sbin/mkfs.ext4":
				return fakeFileInfo{mode: os.ModeDir | 0755}, nil
			case "/sbin/mkfs.ext4":
				return fakeFileInfo{mode: 0755}, nil
			default:
				return nil, os.ErrNotExist
			}
		}, []string{"/usr/sbin", "/opt/sbin", "/sbin"})
		got, err := FindSbinTool("mkfs.ext4")
		if err != nil {
			t.Fatal(err)
		}
		if got != "/sbin/mkfs.ext4" {
			t.Fatalf("got %q", got)
		}
	})
}

func TestFindSbinToolMkfsExt4Missing(t *testing.T) {
	stubSbinTool(t,
		func(string) (string, error) { return "", errors.New("not in PATH") },
		func(string) (os.FileInfo, error) { return nil, os.ErrNotExist },
		[]string{string(filepath.Separator) + "no-such-sbin"},
	)
	_, err := FindSbinTool("mkfs.ext4")
	if err == nil {
		t.Fatal("expected error")
	}
	const want = "mkfs.ext4 not found: install e2fsprogs (apt-get install e2fsprogs)"
	if err.Error() != want {
		t.Fatalf("got %q", err)
	}
}

func TestFindSbinToolMountMissingNamesUtilLinux(t *testing.T) {
	stubSbinTool(t,
		func(string) (string, error) { return "", errors.New("not in PATH") },
		func(string) (os.FileInfo, error) { return nil, os.ErrNotExist },
		[]string{"/no-such-sbin"},
	)
	_, err := FindSbinTool("mount")
	if err == nil || err.Error() != "mount not found: install util-linux (apt-get install util-linux)" {
		t.Fatalf("got %v", err)
	}
}

func TestFindSbinToolRejectsPath(t *testing.T) {
	stubSbinTool(t, func(string) (string, error) {
		t.Fatal("lookup should not run")
		return "", nil
	}, nil, nil)
	if _, err := FindSbinTool("../mkfs.ext4"); err == nil {
		t.Fatal("expected error")
	}
}

// Temp dir stands in for /sbin so the test does not need a host mkfs.ext4.
// Debian's /sbin/mkfs.ext4 is a symlink to mke2fs; stat must follow it.
func TestFindSbinToolTempDirSymlink(t *testing.T) {
	root := t.TempDir()
	usr := filepath.Join(root, "usr-sbin")
	sbn := filepath.Join(root, "sbin")
	if err := os.MkdirAll(usr, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(sbn, 0755); err != nil {
		t.Fatal(err)
	}
	// Earlier candidate exists but is not executable, so it must be skipped.
	if err := os.WriteFile(filepath.Join(usr, "mkfs.ext4"), []byte("nope"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sbn, "mke2fs"), []byte("#!/bin/true\n"), 0755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(sbn, "mkfs.ext4")
	if err := os.Symlink("mke2fs", link); err != nil {
		t.Fatal(err)
	}

	stubSbinTool(t,
		func(string) (string, error) { return "", errors.New("not in PATH") },
		os.Stat,
		[]string{usr, sbn},
	)
	got, err := FindSbinTool("mkfs.ext4")
	if err != nil {
		t.Fatal(err)
	}
	if got != link {
		t.Fatalf("got %q want %q", got, link)
	}
}
