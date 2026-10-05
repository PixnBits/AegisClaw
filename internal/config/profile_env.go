package config

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// profileEnvKeys is the only set LoadProfileEnv will apply. The file lives in
// the invoking user's home and the daemon reads it as root. Anything outside
// this list (PATH, LD_PRELOAD, arbitrary assignments) is ignored.
var profileEnvKeys = map[string]struct{}{
	"AEGIS_COLLAB_TRACE":  {},
	"AEGIS_DEFAULT_MODEL": {},
	"AEGIS_PM_MODEL":      {},
	"AEGIS_ROOTFS_DIR":    {},
	"AEGIS_KERNEL_PATH":   {},
	"AEGIS_BOOT_TIMING":   {},
	"AEGIS_DEBUG":         {},
}

var envKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// ProfileEnvPath is the file LoadProfileEnv reads.
// A non-empty AEGIS_ENV_FILE wins. Otherwise the path is
// <home>/.aegis/profile.env using the same SUDO_USER-first candidateHomes
// search as ResolveRootfsDir. sudo -n drops AEGIS_*, so the daemon must find
// the default path from SUDO_USER rather than from an exported AEGIS_ENV_FILE.
func ProfileEnvPath() string {
	if p := strings.TrimSpace(os.Getenv("AEGIS_ENV_FILE")); p != "" {
		return p
	}
	for _, home := range candidateHomes() {
		if home != "" {
			return filepath.Join(home, ".aegis", "profile.env")
		}
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".aegis", "profile.env")
}

// LoadProfileEnv applies allowlisted KEY=VALUE lines from ProfileEnvPath.
// Blank lines and comments (#) are ignored. A value may be wrapped in double
// quotes. A missing file is a no-op. A key that is already set to a non-empty
// value is left unchanged so an explicit environment beats the file.
// Empty values in the file are not applied. Returns the path consulted and
// the keys that were set.
func LoadProfileEnv() (string, []string, error) {
	path := ProfileEnvPath()
	return loadProfileEnvFile(path)
}

func loadProfileEnvFile(path string) (string, []string, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return path, nil, nil
		}
		return path, nil, err
	}
	defer f.Close()

	var applied []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		key, val, ok := parseProfileEnvLine(sc.Text())
		if !ok {
			continue
		}
		if _, allowed := profileEnvKeys[key]; !allowed {
			continue
		}
		if val == "" {
			continue
		}
		if cur, exists := os.LookupEnv(key); exists && cur != "" {
			continue
		}
		if err := os.Setenv(key, val); err != nil {
			return path, applied, err
		}
		applied = append(applied, key)
	}
	if err := sc.Err(); err != nil {
		return path, applied, err
	}
	return path, applied, nil
}

// parseProfileEnvLine returns key, value, ok. ok is false for blanks, comments,
// and lines that are not KEY=VALUE. An optional "export " prefix is accepted.
// One matching pair of surrounding double quotes is removed from the value.
func parseProfileEnvLine(line string) (string, string, bool) {
	s := strings.TrimSpace(line)
	if s == "" || strings.HasPrefix(s, "#") {
		return "", "", false
	}
	if strings.HasPrefix(s, "export ") {
		s = strings.TrimSpace(strings.TrimPrefix(s, "export "))
	}
	key, val, found := strings.Cut(s, "=")
	if !found {
		return "", "", false
	}
	key = strings.TrimSpace(key)
	if !envKeyPattern.MatchString(key) {
		return "", "", false
	}
	val = strings.TrimSpace(val)
	if len(val) >= 2 && val[0] == '"' && val[len(val)-1] == '"' {
		val = val[1 : len(val)-1]
	}
	return key, val, true
}
