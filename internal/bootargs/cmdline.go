// Package bootargs reads orchestrator-injected kernel cmdline flags (aegis.*=).
package bootargs

import (
	"os"
	"strings"
)

func parseCmdlineKV(prefix string) string {
	data, err := os.ReadFile("/proc/cmdline")
	if err != nil {
		return ""
	}
	s := string(data)
	if i := strings.Index(s, prefix); i >= 0 {
		val := s[i+len(prefix):]
		if j := strings.IndexByte(val, ' '); j >= 0 {
			val = val[:j]
		}
		return val
	}
	return ""
}

// ComponentID returns the Hub registration id (e.g. agent-<session>, memory-<session>).
func ComponentID(defaultID string) string {
	if v := os.Getenv("AEGIS_COMPONENT_ID"); v != "" {
		return v
	}
	if v := parseCmdlineKV("aegis.component_id="); v != "" {
		return v
	}
	return defaultID
}

// PairedMemoryID is the 1:1 Memory VM peer for an Agent Runtime guest.
func PairedMemoryID() string {
	if v := os.Getenv("AEGIS_PAIRED_MEMORY_ID"); v != "" {
		return v
	}
	return parseCmdlineKV("aegis.paired_memory_id=")
}

// PairedAgentID is the 1:1 Agent Runtime peer for a Memory VM guest.
func PairedAgentID() string {
	if v := os.Getenv("AEGIS_PAIRED_AGENT_ID"); v != "" {
		return v
	}
	if v := parseCmdlineKV("aegis.paired_agent_id="); v != "" {
		return v
	}
	// Derive from memory-* component id (memory-{session} -> agent-{session}).
	if cid := ComponentID(""); strings.HasPrefix(cid, "memory-") {
		return strings.Replace(cid, "memory-", "agent-", 1)
	}
	return ""
}

// UseHubVsock reports whether this guest should reach AegisHub via vsock :9999.
func UseHubVsock() bool {
	if os.Getenv("AEGIS_HUB_VSOCK_PORT") != "" {
		return true
	}
	return parseCmdlineKV("aegis.hub_vsock=") == "1"
}

// VMPrivateKeyHex returns hex-encoded Ed25519 private key material from kernel cmdline.
func VMPrivateKeyHex() string {
	if v := os.Getenv("AEGIS_VM_PRIVATE_KEY_HEX"); v != "" {
		return v
	}
	return parseCmdlineKV("aegis.vm_private_key_hex=")
}

// VMPrivateKeyB64 returns the orchestrator-injected Ed25519 seed (base64) for Firecracker guests.
func VMPrivateKeyB64() string {
	if v := os.Getenv("AEGIS_VM_PRIVATE_KEY_B64"); v != "" {
		return v
	}
	return parseCmdlineKV("aegis.vm_private_key_b64=")
}

// VMPrivateKeyPath is the guest-local path to the distributed key file (if injected into rootfs).
func VMPrivateKeyPath() string {
	if v := os.Getenv("AEGIS_VM_PRIVATE_KEY_PATH"); v != "" {
		return v
	}
	if v := parseCmdlineKV("aegis.vm_private_key_path="); v != "" {
		return v
	}
	return "/etc/aegis/vmkey"
}
