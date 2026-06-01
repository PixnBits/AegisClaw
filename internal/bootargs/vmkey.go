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

// LoadDistributedVMKey reads the per-VM key from cmdline hex or guest /etc/aegis/vmkey.
func LoadDistributedVMKey(component string) (ed25519.PrivateKey, ed25519.PublicKey, error) {
	if hexStr := VMPrivateKeyHex(); hexStr != "" {
		privBytes, err := hex.DecodeString(strings.TrimSpace(hexStr))
		if err != nil || len(privBytes) != ed25519.PrivateKeySize {
			return nil, nil, fmt.Errorf("%s: invalid aegis.vm_private_key_hex", component)
		}
		priv := ed25519.PrivateKey(privBytes)
		pub := priv.Public().(ed25519.PublicKey)
		return priv, pub, nil
	}

	path := VMPrivateKeyPath()
	if data, err := os.ReadFile(path); err == nil {
		privBytes, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(data)))
		if err == nil && len(privBytes) == ed25519.PrivateKeySize {
			_ = os.WriteFile(path, []byte("shredded"), 0600)
			_ = os.Remove(path)
			priv := ed25519.PrivateKey(privBytes)
			pub := priv.Public().(ed25519.PublicKey)
			hubclient.ZeroPrivateKey(ed25519.PrivateKey(privBytes))
			return priv, pub, nil
		}
	}

	return nil, nil, fmt.Errorf("%s: no distributed VM key found", component)
}
