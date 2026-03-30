package audit

import (
	"bufio"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// MerkleEntry is a single record in the tamper-evident audit chain.
// Each entry links to its predecessor via PrevHash, forming an append-only
// chain that detects any insertion, deletion, or modification of records.
type MerkleEntry struct {
	ID        string          `json:"id"`
	PrevHash  string          `json:"prev_hash"`
	Hash      string          `json:"hash"`
	Timestamp time.Time       `json:"timestamp"`
	Payload   json.RawMessage `json:"payload"`
	Signature []byte          `json:"signature"`
}

// computeHash calculates the SHA-256 hash over the chain-relevant fields.
// The hash covers: ID, PrevHash, Timestamp, and Payload. This is deterministic
// and does not include the Signature or Hash fields themselves.
func computeHash(id, prevHash string, ts time.Time, payload json.RawMessage) string {
	h := sha256.New()
	h.Write([]byte(id))
	h.Write([]byte(prevHash))
	h.Write([]byte(ts.Format(time.RFC3339Nano)))
	h.Write(payload)
	return hex.EncodeToString(h.Sum(nil))
}

// Verify checks that the entry's hash is correct for its contents.
func (e *MerkleEntry) Verify() error {
	expected := computeHash(e.ID, e.PrevHash, e.Timestamp, e.Payload)
	if e.Hash != expected {
		return fmt.Errorf("hash mismatch for entry %s: expected %s, got %s", e.ID, expected, e.Hash)
	}
	return nil
}

// VerifySignature checks the Ed25519 signature against the entry's hash.
func (e *MerkleEntry) VerifySignature(pubKey ed25519.PublicKey) error {
	hashBytes, err := hex.DecodeString(e.Hash)
	if err != nil {
		return fmt.Errorf("invalid hash hex in entry %s: %w", e.ID, err)
	}
	if !ed25519.Verify(pubKey, hashBytes, e.Signature) {
		return fmt.Errorf("invalid signature for entry %s", e.ID)
	}
	return nil
}

// MerkleLog is a tamper-evident append-only audit log backed by a JSONL file.
// Each entry's hash is chained to its predecessor, and every entry is signed
// with the kernel's Ed25519 key. Appends are fsynced to disk before returning.
type MerkleLog struct {
	file       *os.File
	privateKey ed25519.PrivateKey
	publicKey  ed25519.PublicKey
	lastHash   string
	entryCount uint64
	logger     *zap.Logger
	mu         sync.Mutex
}

// NewMerkleLog opens or creates a Merkle audit log at the given path.
// On open, it reads the existing chain to recover the last hash for continuation.
func NewMerkleLog(path string, privateKey ed25519.PrivateKey, publicKey ed25519.PublicKey, logger *zap.Logger) (*MerkleLog, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create audit directory %s: %w", dir, err)
	}

	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("failed to open audit log %s: %w", path, err)
	}

	ml := &MerkleLog{
		file:       file,
		privateKey: privateKey,
		publicKey:  publicKey,
		logger:     logger,
	}

	// Recover chain state from existing entries
	if err := ml.recoverChainState(); err != nil {
		file.Close()
		return nil, fmt.Errorf("failed to recover chain state from %s: %w", path, err)
	}

	logger.Info("merkle audit log opened",
		zap.String("path", path),
		zap.Uint64("entries", ml.entryCount),
		zap.String("last_hash", ml.lastHash),
	)

	return ml, nil
}

// Append creates a new Merkle entry with the given payload, chains it to the
// previous entry, signs it, and writes it atomically to the log file.
// Returns the entry ID and hash for reference.
func (ml *MerkleLog) Append(payload json.RawMessage) (string, string, error) {
	ml.mu.Lock()
	defer ml.mu.Unlock()

	// Check if already closed
	if ml.file == nil {
		return "", "", fmt.Errorf("audit log is closed")
	}

	id := uuid.New().String()
	ts := time.Now().UTC()
	hash := computeHash(id, ml.lastHash, ts, payload)

	hashBytes, err := hex.DecodeString(hash)
	if err != nil {
		return "", "", fmt.Errorf("internal error: invalid computed hash: %w", err)
	}
	signature := ed25519.Sign(ml.privateKey, hashBytes)

	entry := MerkleEntry{
		ID:        id,
		PrevHash:  ml.lastHash,
		Hash:      hash,
		Timestamp: ts,
		Payload:   payload,
		Signature: signature,
	}

	line, err := json.Marshal(entry)
	if err != nil {
		return "", "", fmt.Errorf("failed to marshal merkle entry: %w", err)
	}
	line = append(line, '\n')

	if _, err := ml.file.Write(line); err != nil {
		return "", "", fmt.Errorf("failed to write merkle entry: %w", err)
	}
	if err := ml.file.Sync(); err != nil {
		return "", "", fmt.Errorf("failed to sync merkle log: %w", err)
	}

	ml.lastHash = hash
	ml.entryCount++

	return id, hash, nil
}

// LastHash returns the hash of the most recent entry (chain head).
func (ml *MerkleLog) LastHash() string {
	ml.mu.Lock()
	defer ml.mu.Unlock()
	return ml.lastHash
}

// EntryCount returns the number of entries in the log.
func (ml *MerkleLog) EntryCount() uint64 {
	ml.mu.Lock()
	defer ml.mu.Unlock()
	return ml.entryCount
}

// Close closes the underlying file.
func (ml *MerkleLog) Close() error {
	ml.mu.Lock()
	defer ml.mu.Unlock()
	if ml.file == nil {
		return nil // already closed
	}
	err := ml.file.Close()
	ml.file = nil
	return err
}

// Path returns the file path of the audit log.
func (ml *MerkleLog) Path() string {
	return ml.file.Name()
}

// ReadEntries reads all entries from the given audit log path.
func ReadEntries(path string) ([]MerkleEntry, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open audit log: %w", err)
	}
	defer file.Close()

	var entries []MerkleEntry
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var entry MerkleEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			return entries, fmt.Errorf("failed to parse entry %d: %w", len(entries)+1, err)
		}
		entries = append(entries, entry)
	}

	if err := scanner.Err(); err != nil {
		return entries, fmt.Errorf("scanner error: %w", err)
	}

	return entries, nil
}

// VerifyChain reads the entire log and verifies:
// 1. Each entry's hash is correct for its contents.
// 2. Each entry's PrevHash matches the previous entry's Hash.
// 3. Each entry's signature is valid against the given public key.
// Returns the number of verified entries and any error encountered.
func VerifyChain(path string, pubKey ed25519.PublicKey) (uint64, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("failed to open audit log for verification: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	// Support entries up to 1MB (generous for JSON payloads)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	var count uint64
	var prevHash string

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var entry MerkleEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			return count, fmt.Errorf("failed to parse entry %d: %w", count+1, err)
		}

		// Verify hash integrity
		if err := entry.Verify(); err != nil {
			return count, fmt.Errorf("entry %d: %w", count+1, err)
		}

		// Verify chain linkage
		if entry.PrevHash != prevHash {
			return count, fmt.Errorf("chain break at entry %d (%s): expected prev_hash %q, got %q",
				count+1, entry.ID, prevHash, entry.PrevHash)
		}

		// Verify signature
		if err := entry.VerifySignature(pubKey); err != nil {
			return count, fmt.Errorf("entry %d: %w", count+1, err)
		}

		prevHash = entry.Hash
		count++
	}

	if err := scanner.Err(); err != nil {
		return count, fmt.Errorf("scanner error at entry %d: %w", count+1, err)
	}

	return count, nil
}

// recoverChainState reads the existing log file to find the last hash
// and entry count so that new appends continue the chain correctly.
func (ml *MerkleLog) recoverChainState() error {
	if _, err := ml.file.Seek(0, 0); err != nil {
		return fmt.Errorf("failed to seek to start: %w", err)
	}

	scanner := bufio.NewScanner(ml.file)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var entry MerkleEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			return fmt.Errorf("failed to parse entry %d: %w", ml.entryCount+1, err)
		}

		ml.lastHash = entry.Hash
		ml.entryCount++
	}

	return scanner.Err()
}
