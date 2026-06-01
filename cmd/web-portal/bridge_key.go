package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"os"
	"strings"
)

// portalBridgeKey returns an Ed25519 keypair for Hub/portal-bridge traffic.
// Firecracker guests often block for a long time on crypto/rand before the kernel
// entropy pool is ready; we therefore prefer a distributed VM key file when
// present and fall back to a deterministic seed (dev/guest-only bridge identity).
func portalBridgeKey() (ed25519.PublicKey, ed25519.PrivateKey) {
	if priv, pub, ok := loadVMKeyFromPath(os.Getenv("AEGIS_VM_PRIVATE_KEY_PATH")); ok {
		return pub, priv
	}
	if path := vmPrivateKeyPathFromCmdline(); path != "" {
		if priv, pub, ok := loadVMKeyFromPath(path); ok {
			return pub, priv
		}
	}
	for _, path := range []string{"/run/aegis/vmkey", "/tmp/aegis/vmkey"} {
		if priv, pub, ok := loadVMKeyFromPath(path); ok {
			return pub, priv
		}
	}
	seed := sha256.Sum256([]byte("aegis-web-portal-guest-bridge-v1"))
	priv := ed25519.NewKeyFromSeed(seed[:])
	pub := priv.Public().(ed25519.PublicKey)
	return pub, priv
}

func loadVMKeyFromPath(path string) (ed25519.PrivateKey, ed25519.PublicKey, bool) {
	if path == "" {
		return nil, nil, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, false
	}
	privBytes, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(data)))
	if err != nil || len(privBytes) != ed25519.PrivateKeySize {
		return nil, nil, false
	}
	priv := ed25519.PrivateKey(privBytes)
	return priv, priv.Public().(ed25519.PublicKey), true
}

func vmPrivateKeyPathFromCmdline() string {
	data, err := os.ReadFile("/proc/cmdline")
	if err != nil {
		return ""
	}
	for _, kv := range strings.Fields(string(data)) {
		if strings.HasPrefix(kv, "aegis.vm_private_key_path=") {
			return strings.TrimPrefix(kv, "aegis.vm_private_key_path=")
		}
	}
	return ""
}
