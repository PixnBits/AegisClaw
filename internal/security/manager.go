// Package security provides cryptographic operations for the daemon.
package security

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
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
