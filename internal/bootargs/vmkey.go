package bootargs

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"strings"

	"AegisClaw/internal/transport/hubclient"
)

// LoadDistributedVMKey reads the per-VM key from guest /etc/aegis/vmkey (preferred) or
// cmdline hex. The injected file is authoritative; cmdline hex is a legacy fallback only.
func LoadDistributedVMKey(component string) (ed25519.PrivateKey, ed25519.PublicKey, error) {
	if priv, pub, err := loadVMKeyFromGuestFile(component); err == nil {
		return priv, pub, nil
	}
	if hexStr := VMPrivateKeyHex(); hexStr != "" {
		if seed, err := decodeVMKeyHex(hexStr); err == nil {
			if priv, pub, err := keypairFromSeedBytes(component, seed); err == nil {
				return priv, pub, nil
			}
		}
	}
	return nil, nil, fmt.Errorf("%s: no distributed VM key found", component)
}

func decodeVMKeyHex(hexStr string) ([]byte, error) {
	return hex.DecodeString(strings.TrimSpace(hexStr))
}

func loadVMKeyFromGuestFile(component string) (ed25519.PrivateKey, ed25519.PublicKey, error) {
	path := VMPrivateKeyPath()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	privBytes, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(data)))
	if err != nil || len(privBytes) != ed25519.PrivateKeySize {
		return nil, nil, fmt.Errorf("%s: invalid guest vmkey file", component)
	}
	_ = os.WriteFile(path, []byte("shredded"), 0600) //nolint:gosec // intentional one-time shred
	_ = os.Remove(path)
	return keypairFromSeedBytes(component, privBytes)
}

func keypairFromSeedBytes(component string, privBytes []byte) (ed25519.PrivateKey, ed25519.PublicKey, error) {
	if len(privBytes) != ed25519.PrivateKeySize {
		return nil, nil, fmt.Errorf("%s: invalid VM key material", component)
	}
	// Copy before shredding temp buffers so returned priv is not aliased to zeroed memory.
	priv := make(ed25519.PrivateKey, ed25519.PrivateKeySize)
	copy(priv, privBytes)
	hubclient.ZeroPrivateKey(ed25519.PrivateKey(privBytes))
	pub := priv.Public().(ed25519.PublicKey)
	return priv, pub, nil
}
