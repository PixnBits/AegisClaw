package kernel

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"go.uber.org/zap"
)

// Kernel is the immutable core of AegisClaw.
// All operations are routed through the kernel for signing and audit logging.
// Security: Singleton pattern ensures a single point of authority per process.
// Ed25519 keys provide cryptographic integrity for every action.
type Kernel struct {
	privateKey   ed25519.PrivateKey
	publicKey    ed25519.PublicKey
	logger       *zap.Logger
	controlPlane *ControlPlane
	auditFile    *os.File
	auditMu      sync.Mutex
}

var (
	instance *Kernel
	initErr  error
	once     sync.Once
)

// GetInstance returns the singleton Kernel instance, initializing on first call.
// The auditDir parameter specifies where the append-only audit JSONL is written.
// Security: Thread-safe initialization via sync.Once prevents race conditions.
func GetInstance(logger *zap.Logger, auditDir string) (*Kernel, error) {
	once.Do(func() {
		instance, initErr = newKernel(logger, auditDir)
	})
	if initErr != nil {
		return nil, initErr
	}
	return instance, nil
}

// ResetInstance tears down the singleton for testing purposes only.
func ResetInstance() {
	if instance != nil {
		instance.Shutdown()
	}
	instance = nil
	initErr = nil
	once = sync.Once{}
}

func newKernel(logger *zap.Logger, auditDir string) (*Kernel, error) {
	keyDir, err := defaultKeyDir()
	if err != nil {
		return nil, fmt.Errorf("failed to determine key directory: %w", err)
	}

	if err := os.MkdirAll(keyDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create key directory %s: %w", keyDir, err)
	}

	privateKey, publicKey, err := loadOrGenerateKey(logger, keyDir)
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(auditDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create audit directory %s: %w", auditDir, err)
	}

	auditPath := filepath.Join(auditDir, "kernel.jsonl")
	auditFile, err := os.OpenFile(auditPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return nil, fmt.Errorf("failed to open audit log %s: %w", auditPath, err)
	}

	k := &Kernel{
		privateKey: privateKey,
		publicKey:  publicKey,
		logger:     logger,
		auditFile:  auditFile,
	}

	k.controlPlane = NewControlPlane(k, logger)

	logger.Info("kernel initialized",
		zap.String("public_key", fmt.Sprintf("%x", publicKey)),
		zap.String("audit_log", auditPath),
	)

	return k, nil
}

// loadOrGenerateKey loads an existing Ed25519 key or generates a new one.
// Security: Key stored with 0600 permissions. Generated using crypto/rand.
func loadOrGenerateKey(logger *zap.Logger, keyDir string) (ed25519.PrivateKey, ed25519.PublicKey, error) {
	keyPath := filepath.Join(keyDir, "kernel.key")

	keyData, err := os.ReadFile(keyPath)
	if os.IsNotExist(err) {
		logger.Info("generating new Ed25519 kernel keypair", zap.String("path", keyPath))
		pub, priv, genErr := ed25519.GenerateKey(rand.Reader)
		if genErr != nil {
			return nil, nil, fmt.Errorf("failed to generate Ed25519 key: %w", genErr)
		}
		if writeErr := os.WriteFile(keyPath, priv, 0600); writeErr != nil {
			return nil, nil, fmt.Errorf("failed to write kernel key: %w", writeErr)
		}
		logger.Info("kernel keypair generated and stored")
		return priv, pub, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read kernel key: %w", err)
	}
	if len(keyData) != ed25519.PrivateKeySize {
		return nil, nil, fmt.Errorf("invalid kernel key size: expected %d bytes, got %d", ed25519.PrivateKeySize, len(keyData))
	}

	priv := ed25519.PrivateKey(keyData)
	pub := priv.Public().(ed25519.PublicKey)
	logger.Info("kernel keypair loaded from storage")
	return priv, pub, nil
}

// SignAndLog signs an action with the kernel's Ed25519 key and appends it
// to the append-only audit log. This is the mandatory entry point for all
// kernel operations — nothing proceeds without a signed audit record.
// Security: Every action is cryptographically signed and fsynced to disk
// before the method returns, ensuring a complete tamper-evident audit trail.
func (k *Kernel) SignAndLog(action Action) (*SignedAction, error) {
	if err := action.Validate(); err != nil {
		return nil, fmt.Errorf("invalid action: %w", err)
	}

	data, err := action.Marshal()
	if err != nil {
		return nil, fmt.Errorf("failed to marshal action %s: %w", action.ID, err)
	}

	signature := ed25519.Sign(k.privateKey, data)

	signed := &SignedAction{
		Action:    action,
		Signature: signature,
	}

	entry, err := json.Marshal(signed)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal signed action %s: %w", action.ID, err)
	}
	entry = append(entry, '\n')

	k.auditMu.Lock()
	_, writeErr := k.auditFile.Write(entry)
	syncErr := k.auditFile.Sync()
	k.auditMu.Unlock()

	if writeErr != nil {
		return nil, fmt.Errorf("failed to write audit entry %s: %w", action.ID, writeErr)
	}
	if syncErr != nil {
		return nil, fmt.Errorf("failed to sync audit log for %s: %w", action.ID, syncErr)
	}

	k.logger.Info("action signed and logged",
		zap.String("action_id", action.ID),
		zap.String("type", string(action.Type)),
		zap.String("source", action.Source),
	)

	return signed, nil
}

// Sign signs arbitrary data with the kernel's Ed25519 private key.
func (k *Kernel) Sign(data []byte) []byte {
	return ed25519.Sign(k.privateKey, data)
}

// Verify checks an Ed25519 signature against data using the kernel's public key.
func (k *Kernel) Verify(data []byte, signature []byte) bool {
	return ed25519.Verify(k.publicKey, data, signature)
}

// PublicKey returns the kernel's Ed25519 public key.
func (k *Kernel) PublicKey() ed25519.PublicKey {
	return k.publicKey
}

// ControlPlane returns the kernel's control plane for VM communication.
func (k *Kernel) ControlPlane() *ControlPlane {
	return k.controlPlane
}

// Shutdown gracefully shuts down the kernel, closing all resources.
func (k *Kernel) Shutdown() {
	if k.controlPlane != nil {
		k.controlPlane.Shutdown()
	}
	if k.auditFile != nil {
		k.auditMu.Lock()
		k.auditFile.Close()
		k.auditMu.Unlock()
	}
	k.logger.Info("kernel shut down")
}

func defaultKeyDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(home, ".config", "aegisclaw"), nil
}
