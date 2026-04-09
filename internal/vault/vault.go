package vault

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"filippo.io/age"
	"go.uber.org/zap"
	"golang.org/x/crypto/hkdf"
)

// maxSecretBytes is an upper bound on a single secret's plaintext size.
// Secrets larger than this are rejected to prevent runaway memory on
// corrupt/tampered vault files.
const maxSecretBytes = 1 * 1024 * 1024 // 1 MiB

var secretNameRegex = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_\-]{0,127}$`)

// SecretEntry stores metadata about a secret (never the plaintext value).
type SecretEntry struct {
	Name      string    `json:"name"`
	SkillID   string    `json:"skill_id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Size      int       `json:"size"`
}

// Vault provides age-encrypted secret storage with kernel-only access.
// Secrets are encrypted at rest using an age identity derived from the kernel's
// Ed25519 private key. Only the kernel process can decrypt secrets.
type Vault struct {
	storeDir  string
	identity  *age.X25519Identity
	recipient *age.X25519Recipient
	logger    *zap.Logger
	mu        sync.RWMutex
	entries   map[string]*SecretEntry
}

// NewVault creates a Vault at the given directory using the kernel's Ed25519 key.
// The age X25519 encryption identity is derived deterministically from the
// Ed25519 private key via HKDF-SHA256, so no separate identity file is written
// to disk.  The vault can always be recovered from the Ed25519 key alone.
func NewVault(storeDir string, privateKey ed25519.PrivateKey, logger *zap.Logger) (*Vault, error) {
	if storeDir == "" {
		return nil, fmt.Errorf("store directory is required")
	}
	if len(privateKey) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("invalid Ed25519 private key size: expected %d, got %d", ed25519.PrivateKeySize, len(privateKey))
	}

	if err := os.MkdirAll(storeDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create vault directory %s: %w", storeDir, err)
	}

	identity, err := deriveAgeIdentity(privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to derive age identity: %w", err)
	}

	recipient := identity.Recipient()

	v := &Vault{
		storeDir:  storeDir,
		identity:  identity,
		recipient: recipient,
		logger:    logger,
		entries:   make(map[string]*SecretEntry),
	}

	// Load existing entries
	if err := v.loadEntries(); err != nil {
		return nil, fmt.Errorf("failed to load vault entries: %w", err)
	}

	logger.Info("vault initialized",
		zap.String("store_dir", storeDir),
		zap.Int("secrets", len(v.entries)),
	)

	return v, nil
}

// Add encrypts and stores a secret value. The plaintext is never stored on disk.
func (v *Vault) Add(name, skillID string, plaintext []byte) error {
	if err := validateSecretName(name); err != nil {
		return err
	}
	if skillID == "" {
		return fmt.Errorf("skill ID is required")
	}
	if len(plaintext) == 0 {
		return fmt.Errorf("secret value cannot be empty")
	}

	v.mu.Lock()
	defer v.mu.Unlock()

	// Encrypt with age
	encrypted, err := v.encrypt(plaintext)
	if err != nil {
		return fmt.Errorf("failed to encrypt secret %q: %w", name, err)
	}

	// Write encrypted file
	secretPath := v.secretPath(name)
	if err := os.WriteFile(secretPath, encrypted, 0600); err != nil {
		return fmt.Errorf("failed to write secret %q: %w", name, err)
	}

	// Update entry metadata
	now := time.Now().UTC()
	entry, exists := v.entries[name]
	if exists {
		entry.UpdatedAt = now
		entry.Size = len(plaintext)
		entry.SkillID = skillID
	} else {
		entry = &SecretEntry{
			Name:      name,
			SkillID:   skillID,
			CreatedAt: now,
			UpdatedAt: now,
			Size:      len(plaintext),
		}
		v.entries[name] = entry
	}

	if err := v.saveEntries(); err != nil {
		return fmt.Errorf("failed to save vault metadata: %w", err)
	}

	v.logger.Info("secret stored",
		zap.String("name", name),
		zap.String("skill_id", skillID),
		zap.Int("size", len(plaintext)),
	)

	return nil
}

// Get decrypts and returns a secret value. Only the kernel should call this.
func (v *Vault) Get(name string) ([]byte, error) {
	if err := validateSecretName(name); err != nil {
		return nil, err
	}

	v.mu.RLock()
	defer v.mu.RUnlock()

	_, exists := v.entries[name]
	if !exists {
		return nil, fmt.Errorf("secret %q not found", name)
	}

	secretPath := v.secretPath(name)
	encrypted, err := os.ReadFile(secretPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read secret %q: %w", name, err)
	}

	plaintext, err := v.decrypt(encrypted)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt secret %q: %w", name, err)
	}

	return plaintext, nil
}

// Delete removes a secret from the vault.
func (v *Vault) Delete(name string) error {
	if err := validateSecretName(name); err != nil {
		return err
	}

	v.mu.Lock()
	defer v.mu.Unlock()

	_, exists := v.entries[name]
	if !exists {
		return fmt.Errorf("secret %q not found", name)
	}

	secretPath := v.secretPath(name)
	if err := os.Remove(secretPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove secret %q: %w", name, err)
	}

	delete(v.entries, name)

	if err := v.saveEntries(); err != nil {
		return fmt.Errorf("failed to save vault metadata: %w", err)
	}

	v.logger.Info("secret deleted", zap.String("name", name))
	return nil
}

// List returns metadata for all stored secrets.
func (v *Vault) List() []*SecretEntry {
	v.mu.RLock()
	defer v.mu.RUnlock()

	entries := make([]*SecretEntry, 0, len(v.entries))
	for _, e := range v.entries {
		entries = append(entries, e)
	}
	return entries
}

// ListForSkill returns metadata for secrets associated with a specific skill.
func (v *Vault) ListForSkill(skillID string) []*SecretEntry {
	v.mu.RLock()
	defer v.mu.RUnlock()

	var entries []*SecretEntry
	for _, e := range v.entries {
		if e.SkillID == skillID {
			entries = append(entries, e)
		}
	}
	return entries
}

// Has checks if a secret exists.
func (v *Vault) Has(name string) bool {
	v.mu.RLock()
	defer v.mu.RUnlock()
	_, exists := v.entries[name]
	return exists
}

// GetEntry returns the metadata entry for a secret (no decryption).
func (v *Vault) GetEntry(name string) (*SecretEntry, bool) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	e, ok := v.entries[name]
	return e, ok
}

// encrypt encrypts plaintext using the vault's age recipient.
func (v *Vault) encrypt(plaintext []byte) ([]byte, error) {
	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, v.recipient)
	if err != nil {
		return nil, fmt.Errorf("age encrypt init: %w", err)
	}
	if _, err := w.Write(plaintext); err != nil {
		return nil, fmt.Errorf("age encrypt write: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("age encrypt close: %w", err)
	}
	return buf.Bytes(), nil
}

// decrypt decrypts age-encrypted ciphertext using the vault's identity.
func (v *Vault) decrypt(ciphertext []byte) ([]byte, error) {
	r, err := age.Decrypt(bytes.NewReader(ciphertext), v.identity)
	if err != nil {
		return nil, fmt.Errorf("age decrypt: %w", err)
	}
	plaintext, err := io.ReadAll(io.LimitReader(r, maxSecretBytes))
	if err != nil {
		return nil, fmt.Errorf("age decrypt read: %w", err)
	}
	// If we read exactly maxSecretBytes, the secret may have been silently
	// truncated by LimitReader — treat this as a corrupt vault file.
	if int64(len(plaintext)) == maxSecretBytes {
		return nil, fmt.Errorf("secret exceeds maximum size (%d bytes); vault file may be corrupt", maxSecretBytes)
	}
	return plaintext, nil
}

// secretPath returns the file path for an encrypted secret.
func (v *Vault) secretPath(name string) string {
	return filepath.Join(v.storeDir, name+".age")
}

// loadEntries reads the metadata index from disk.
func (v *Vault) loadEntries() error {
	indexPath := filepath.Join(v.storeDir, "index.json")
	data, err := os.ReadFile(indexPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to read vault index: %w", err)
	}

	var entries []*SecretEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return fmt.Errorf("failed to parse vault index: %w", err)
	}

	for _, e := range entries {
		v.entries[e.Name] = e
	}
	return nil
}

// saveEntries writes the metadata index to disk.
func (v *Vault) saveEntries() error {
	entries := make([]*SecretEntry, 0, len(v.entries))
	for _, e := range v.entries {
		entries = append(entries, e)
	}

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal vault index: %w", err)
	}

	indexPath := filepath.Join(v.storeDir, "index.json")
	return os.WriteFile(indexPath, data, 0600)
}

// deriveAgeIdentity derives a deterministic age X25519 encryption identity from
// an Ed25519 private key using HKDF-SHA256.  Binding the vault key to the
// kernel's existing Ed25519 key means no separate identity file is required:
// vault contents are always recoverable as long as the Ed25519 key is intact.
// The info label domain-separates this derivation and encodes a version so
// future key-derivation changes can be detected.
func deriveAgeIdentity(privateKey ed25519.PrivateKey) (*age.X25519Identity, error) {
	r := hkdf.New(sha256.New, privateKey.Seed(), nil, []byte("aegisclaw-vault-age-identity-v1"))
	scalar := make([]byte, 32)
	if _, err := io.ReadFull(r, scalar); err != nil {
		return nil, fmt.Errorf("hkdf derive age scalar: %w", err)
	}
	encoded := bech32Encode("age-secret-key-", scalar)
	// age.ParseX25519Identity requires the uppercase form (AGE-SECRET-KEY-1...).
	return age.ParseX25519Identity(strings.ToUpper(encoded))
}

// validateSecretName checks that a secret name is valid.
func validateSecretName(name string) error {
	if name == "" {
		return fmt.Errorf("secret name is required")
	}
	if !secretNameRegex.MatchString(name) {
		return fmt.Errorf("invalid secret name %q: must match %s", name, secretNameRegex.String())
	}
	if strings.Contains(name, "..") || strings.Contains(name, "/") || strings.Contains(name, "\\") {
		return fmt.Errorf("invalid secret name %q: contains path separator or traversal", name)
	}
	return nil
}
