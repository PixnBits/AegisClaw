package sandbox

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Replaced by tests. Defaults match Debian/Ubuntu: mkfs.ext4 is
// /sbin/mkfs.ext4 (or /usr/sbin) and is hidden when `sudo PATH=$PATH`
// drops the sbin directories.
var (
	sbinLookPath = exec.LookPath
	sbinStat     = os.Stat
	sbinDirs     = []string{"/usr/sbin", "/sbin"}
)

// FindSbinTool resolves name via PATH, then /usr/sbin and /sbin.
// A missing mkfs.ext4 reports that e2fsprogs needs to be installed.
func FindSbinTool(name string) (string, error) {
	if name == "" || strings.ContainsAny(name, `/\`) {
		return "", fmt.Errorf("invalid tool name %q", name)
	}
	if p, err := sbinLookPath(name); err == nil {
		return p, nil
	}
	for _, dir := range sbinDirs {
		candidate := filepath.Join(dir, name)
		info, err := sbinStat(candidate)
		if err != nil || info.IsDir() || info.Mode()&0111 == 0 {
			continue
		}
		return candidate, nil
	}
	return "", missingSbinToolError(name)
}

func missingSbinToolError(name string) error {
	switch name {
	case "mkfs.ext4":
		return fmt.Errorf("mkfs.ext4 not found: install e2fsprogs (apt-get install e2fsprogs)")
	case "mount", "umount", "losetup":
		return fmt.Errorf("%s not found: install util-linux (apt-get install util-linux)", name)
	default:
		return fmt.Errorf("%s not found in PATH, /usr/sbin, or /sbin", name)
	}
}
