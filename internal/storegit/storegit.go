package storegit

import (
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

const (
	// Transport is the git-remote-* helper name (git-remote-hub).
	Transport = "hub"
	// WellKnownSocketName is used when the helper has no AEGIS_HUB_SOCKET
	// (T7's git(1) push/clone inherit the test process env, not Store's).
	WellKnownSocketName = "aegis-store-git.sock"
	SiblingSocketName   = "git.sock"
)

// RemoteURL is a git(1)-speakable Hub/vsock URL. Tenant is part of the
// remote so tenant-a and tenant-b get distinct remotes for the same repo name.
func RemoteURL(tenant, repo string) string {
	return "hub::vsock/" + tenant + "/" + repo
}

// ParseURL extracts tenant and repo from a hub::vsock/... remote or the
// address passed to git-remote-hub (vsock/<tenant>/<repo>).
func ParseURL(s string) (tenant, repo string, ok bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", "", false
	}
	s = strings.TrimPrefix(s, "hub::")
	s = strings.TrimPrefix(s, "vsock/")
	s = strings.Trim(s, "/")
	parts := strings.Split(s, "/")
	if len(parts) < 2 {
		return "", "", false
	}
	tenant, repo = parts[0], parts[len(parts)-1]
	if !ValidName(tenant) || !ValidName(repo) {
		return "", "", false
	}
	return tenant, repo, true
}

// ValidName rejects path traversal and empty git names.
func ValidName(s string) bool {
	if s == "" || s == "." || s == ".." {
		return false
	}
	if strings.ContainsAny(s, `/\:`) || strings.Contains(s, "..") {
		return false
	}
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == '.' {
			continue
		}
		return false
	}
	return true
}

// HubPrivateGitSocket is the Store git unix socket. Only Hub may know this
// path. The helper must not read this env or dial these well-known names.
func HubPrivateGitSocket() string {
	return strings.TrimSpace(os.Getenv("AEGIS_STORE_GIT_SOCKET"))
}

// PublicGitSockets are well-known paths that must not accept (T13).
func PublicGitSockets(hubSock string) []string {
	var out []string
	if hubSock == "" {
		hubSock = os.Getenv("AEGIS_HUB_SOCKET")
	}
	if hubSock != "" {
		out = append(out, filepath.Join(filepath.Dir(hubSock), SiblingSocketName))
	}
	out = append(out, filepath.Join(os.TempDir(), WellKnownSocketName))
	return out
}

// BarePath is the tenant-prefix bare remote on Store disk (cmd.Dir).
func BarePath(tenant, repo string) string {
	return filepath.Join("repos", tenant, repo)
}
