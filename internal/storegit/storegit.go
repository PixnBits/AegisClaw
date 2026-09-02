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

// SocketCandidates lists unix sockets the helper should try, in order.
// The clone URL never contains these paths.
func SocketCandidates(hubSock string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		if p == "" {
			return
		}
		p = filepath.Clean(p)
		if seen[p] {
			return
		}
		seen[p] = true
		out = append(out, p)
	}
	add(os.Getenv("AEGIS_STORE_GIT_SOCKET"))
	if hubSock == "" {
		hubSock = os.Getenv("AEGIS_HUB_SOCKET")
	}
	if hubSock != "" {
		add(filepath.Join(filepath.Dir(hubSock), SiblingSocketName))
	}
	add(filepath.Join(os.TempDir(), WellKnownSocketName))
	return out
}

// BarePath is the tenant-prefix bare remote on Store disk (cmd.Dir).
func BarePath(tenant, repo string) string {
	return filepath.Join("repos", tenant, repo)
}
