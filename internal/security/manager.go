// Package security provides cryptographic operations for the daemon.
package security

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// KeyPair holds a public/private key pair.
type KeyPair struct {
	PublicKey  ed25519.PublicKey
	PrivateKey ed25519.PrivateKey
}

// Manager handles cryptographic operations for the daemon.
type Manager struct {
	keyFile string
	keyPair *KeyPair
	vms     *vmKeyRegistry // per-VM public keys (privates never stored)
}

// NewManager creates a new security manager.
func NewManager(stateDir string) *Manager {
	return &Manager{
		keyFile: filepath.Join(stateDir, "daemon.key"),
	}
}

// Load loads or creates the daemon's keypair.
func (m *Manager) Load() error {
	if _, err := os.Stat(m.keyFile); err == nil {
		// Load existing key
		keyData, err := os.ReadFile(m.keyFile)
		if err != nil {
			return fmt.Errorf("failed to read key file: %w", err)
		}
		privKey := ed25519.PrivateKey(keyData)
		m.keyPair = &KeyPair{
			PublicKey:  privKey.Public().(ed25519.PublicKey),
			PrivateKey: privKey,
		}
		return nil
	}

	// Create new key
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("failed to generate keypair: %w", err)
	}

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(m.keyFile), 0700); err != nil {
		return fmt.Errorf("failed to create state directory: %w", err)
	}

	// Write key file (mode 0600 for security)
	if err := os.WriteFile(m.keyFile, priv, 0600); err != nil {
		return fmt.Errorf("failed to write key file: %w", err)
	}

	m.keyPair = &KeyPair{
		PublicKey:  pub,
		PrivateKey: priv,
	}

	return nil
}

// GetKeyPair returns the daemon's keypair.
func (m *Manager) GetKeyPair() *KeyPair {
	return m.keyPair
}

// Sign signs a message with the daemon's private key.
func (m *Manager) Sign(message []byte) (string, error) {
	if m.keyPair == nil {
		return "", fmt.Errorf("keypair not loaded")
	}
	sig := ed25519.Sign(m.keyPair.PrivateKey, message)
	return base64.StdEncoding.EncodeToString(sig), nil
}

// Verify verifies a signature.
func (m *Manager) Verify(publicKey ed25519.PublicKey, message []byte, signature string) error {
	sigBytes, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		return fmt.Errorf("invalid signature encoding: %w", err)
	}
	if !ed25519.Verify(publicKey, message, sigBytes) {
		return fmt.Errorf("signature verification failed")
	}
	return nil
}

// PublicKeyString returns the public key as a base64-encoded string.
func PublicKeyString(pubKey ed25519.PublicKey) string {
	return base64.StdEncoding.EncodeToString(pubKey)
}

// --- Per-VM key management (Host Daemon TCB responsibility) ---

// vmKeyRegistry holds public keys for registered VMs/sandboxes so that
// AegisHub and other components can verify signed messages.
// Private keys are NEVER stored here after generation (see GenerateVMKeyPair).
type vmKeyRegistry struct {
	mu     sync.RWMutex
	pubKeys map[string]ed25519.PublicKey
}

func newVMKeyRegistry() *vmKeyRegistry {
	return &vmKeyRegistry{
		pubKeys: make(map[string]ed25519.PublicKey),
	}
}

// GenerateVMKeyPair creates a new Ed25519 keypair intended for a single microVM or
// sandbox. The *private* key must be injected into that VM's environment by the
// caller (Orchestrator + sandbox backend) and MUST NOT be retained or logged by
// the Host Daemon afterward. This is a core TCB requirement from host-daemon.md.
func (m *Manager) GenerateVMKeyPair() (*KeyPair, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate VM keypair: %w", err)
	}
	// Note: we deliberately do *not* store the private key anywhere in the Manager.
	return &KeyPair{
		PublicKey:  pub,
		PrivateKey: priv,
	}, nil
}

// RegisterVM registers the public key of a successfully started VM. The private
// key must have already been handed off to the VM and zeroized in the daemon.
func (m *Manager) RegisterVM(vmID string, pub ed25519.PublicKey) {
	if m.vms == nil {
		m.vms = newVMKeyRegistry()
	}
	m.vms.mu.Lock()
	defer m.vms.mu.Unlock()
	m.vms.pubKeys[vmID] = pub
}

// GetVMPublicKey returns the registered public key for a VM (for signature verification).
func (m *Manager) GetVMPublicKey(vmID string) (ed25519.PublicKey, bool) {
	if m.vms == nil {
		return nil, false
	}
	m.vms.mu.RLock()
	defer m.vms.mu.RUnlock()
	p, ok := m.vms.pubKeys[vmID]
	return p, ok
}

// ListRegisteredVMs returns the IDs of currently registered VMs (for debugging/audit).
func (m *Manager) ListRegisteredVMs() []string {
	if m.vms == nil {
		return nil
	}
	m.vms.mu.RLock()
	defer m.vms.mu.RUnlock()
	ids := make([]string, 0, len(m.vms.pubKeys))
	for id := range m.vms.pubKeys {
		ids = append(ids, id)
	}
	return ids
}
