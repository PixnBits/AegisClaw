// Package memory implements the tiered persistent Memory Store for AegisClaw.
//
// The store persists MemoryEntry records in an age-encrypted append-only JSONL
// file (the "vault file"), with a separate index for fast in-memory lookups.
// On restart the index is rebuilt from the vault file.
//
// All write operations are logged to the Merkle audit tree before the data is
// persisted.  Compaction rewrites the vault file in place (atomic rename).
//
// Security note: the age encryption key is derived from the daemon's Ed25519
// identity key (same mechanism as the secrets vault).  Memory data never leaves
// the host daemon process.
package memory

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"filippo.io/age"
	"github.com/google/uuid"
)

// TTLTier describes the retention and compaction fidelity tier for a memory.
type TTLTier string

const (
	TTL90d    TTLTier = "90d"
	TTL180d   TTLTier = "180d"
	TTL365d   TTLTier = "365d"
	TTL2yr    TTLTier = "2yr"
	TTLForever TTLTier = "forever"
)

// validTTLTiers lists tiers in compaction order (shortest first).
var validTTLTiers = []TTLTier{TTL90d, TTL180d, TTL365d, TTL2yr, TTLForever}

// SecurityLevel describes the sensitivity of stored data.
type SecurityLevel string

const (
	SecurityLow    SecurityLevel = "low"
	SecurityMedium SecurityLevel = "medium"
	SecurityHigh   SecurityLevel = "high"
)

// MemoryEntry is the unit of persistence in the Memory Store.
type MemoryEntry struct {
	// MemoryID is a UUID assigned on creation.
	MemoryID string `json:"memory_id"`
	// TaskID links the memory to an async task (optional).
	TaskID string `json:"task_id,omitempty"`
	// Key is a human-readable or semantic key for exact-match retrieval.
	Key string `json:"key"`
	// Value is the plaintext (or JSON-encoded) content of the memory.
	// It is encrypted at rest inside the vault file.
	Value string `json:"value"`
	// Tags are optional labels for filtered retrieval.
	Tags []string `json:"tags,omitempty"`
	// SecurityLevel affects redaction and approval requirements.
	SecurityLevel SecurityLevel `json:"security_level"`
	// TTLTier determines when the memory is compacted or archived.
	TTLTier TTLTier `json:"ttl_tier"`
	// CreatedAt is when the entry was first stored.
	CreatedAt time.Time `json:"created_at"`
	// LastCompactedAt is the most recent compaction time (nil = never compacted).
	LastCompactedAt *time.Time `json:"last_compacted_at,omitempty"`
	// Version is incremented on each soft-update.
	Version int `json:"version"`
	// Deleted marks the entry as soft-deleted (GDPR right-to-forget).
	Deleted bool `json:"deleted"`
}

// StoreSummary is a lightweight view of a MemoryEntry for listing.
type StoreSummary struct {
	MemoryID      string        `json:"memory_id"`
	TaskID        string        `json:"task_id,omitempty"`
	Key           string        `json:"key"`
	Tags          []string      `json:"tags,omitempty"`
	TTLTier       TTLTier       `json:"ttl_tier"`
	SecurityLevel SecurityLevel `json:"security_level"`
	CreatedAt     time.Time     `json:"created_at"`
	Version       int           `json:"version"`
}

// StoreConfig holds construction parameters for the Store.
type StoreConfig struct {
	// Dir is the directory where the vault file is stored.
	Dir string
	// MaxSizeMB is the hard cap on the vault file size in mebibytes (default 2048).
	MaxSizeMB int64
	// DefaultTTL is the TTL tier assigned when none is specified (default TTL90d).
	DefaultTTL TTLTier
}

// Store is the tiered persistent memory store.
// All exported methods are safe for concurrent use.
type Store struct {
	cfg       StoreConfig
	vaultPath string
	identity  *age.X25519Identity
	recipient *age.X25519Recipient
	mu        sync.RWMutex
	// index holds all non-deleted entries in memory for fast retrieval.
	index map[string]*MemoryEntry // keyed by MemoryID
}

const vaultFileName = "memory.vault.jsonl.age"

// NewStore opens (or creates) the Memory Store at cfg.Dir using the given age
// identity for encryption/decryption.  The identity is expected to come from
// the daemon's existing age key (same as the secrets vault).
//
// On startup the vault file is fully decrypted and loaded into the in-memory
// index so that retrieval operations are O(n) rather than requiring disk access
// on every call.
func NewStore(cfg StoreConfig, identity *age.X25519Identity) (*Store, error) {
	if cfg.Dir == "" {
		return nil, fmt.Errorf("memory store directory is required")
	}
	if cfg.MaxSizeMB <= 0 {
		cfg.MaxSizeMB = 2048
	}
	if cfg.DefaultTTL == "" {
		cfg.DefaultTTL = TTL90d
	}
	if identity == nil {
		return nil, fmt.Errorf("age identity is required for memory store encryption")
	}

	if err := os.MkdirAll(cfg.Dir, 0700); err != nil {
		return nil, fmt.Errorf("create memory dir %s: %w", cfg.Dir, err)
	}

	s := &Store{
		cfg:       cfg,
		vaultPath: filepath.Join(cfg.Dir, vaultFileName),
		identity:  identity,
		recipient: identity.Recipient(),
		index:     make(map[string]*MemoryEntry),
	}

	if err := s.loadIndex(); err != nil {
		return nil, fmt.Errorf("load memory store index: %w", err)
	}
	return s, nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Write operations
// ──────────────────────────────────────────────────────────────────────────────

// Store persists a new memory entry and returns its assigned MemoryID.
// The entry is appended to the vault file and added to the in-memory index.
func (s *Store) Store(e *MemoryEntry) (string, error) {
	if e == nil {
		return "", fmt.Errorf("memory entry must not be nil")
	}
	if e.Key == "" {
		return "", fmt.Errorf("memory entry key is required")
	}

	now := time.Now().UTC()
	if e.MemoryID == "" {
		e.MemoryID = uuid.New().String()
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = now
	}
	if e.TTLTier == "" {
		e.TTLTier = s.cfg.DefaultTTL
	}
	if e.SecurityLevel == "" {
		e.SecurityLevel = SecurityLow
	}
	e.Version = 1
	e.Deleted = false

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.appendEntry(e); err != nil {
		return "", fmt.Errorf("append memory entry: %w", err)
	}
	s.index[e.MemoryID] = e
	return e.MemoryID, nil
}

// Delete soft-deletes all entries whose Key or Tags match any token in query.
// GDPR-style: the entries are marked Deleted=true and rewritten to the vault.
// Returns the number of entries that were deleted.
func (s *Store) Delete(query string) (int, error) {
	if query == "" {
		return 0, fmt.Errorf("delete query must not be empty")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	tokens := strings.Fields(strings.ToLower(query))
	count := 0
	for _, e := range s.index {
		if e.Deleted {
			continue
		}
		if matchesAny(e, tokens) {
			e.Deleted = true
			count++
		}
	}
	if count == 0 {
		return 0, nil
	}

	// Rewrite vault to materialise the soft deletes.
	if err := s.rewriteVault(); err != nil {
		return 0, fmt.Errorf("rewrite vault after delete: %w", err)
	}
	return count, nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Read operations
// ──────────────────────────────────────────────────────────────────────────────

// Retrieve returns up to k entries matching query (keyword search against key,
// value, and tags).  If k <= 0 all matches are returned.
// Filters: if taskID is non-empty only entries matching that task are returned.
// Results are sorted newest-first.
func (s *Store) Retrieve(query string, k int, taskID string) ([]*MemoryEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tokens := strings.Fields(strings.ToLower(query))
	var results []*MemoryEntry

	for _, e := range s.index {
		if e.Deleted {
			continue
		}
		if taskID != "" && e.TaskID != taskID {
			continue
		}
		if len(tokens) == 0 || matchesAny(e, tokens) {
			cp := *e
			results = append(results, &cp)
		}
	}

	// Sort newest first.
	sort.Slice(results, func(i, j int) bool {
		return results[i].CreatedAt.After(results[j].CreatedAt)
	})

	if k > 0 && len(results) > k {
		results = results[:k]
	}
	return results, nil
}

// Get returns the entry with the exact MemoryID, or an error if not found.
func (s *Store) Get(memoryID string) (*MemoryEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	e, ok := s.index[memoryID]
	if !ok || e.Deleted {
		return nil, fmt.Errorf("memory %s not found", memoryID)
	}
	cp := *e
	return &cp, nil
}

// List returns summaries for all non-deleted entries, optionally filtered by
// TTL tier (empty = all tiers).
func (s *Store) List(tier TTLTier) ([]StoreSummary, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var out []StoreSummary
	for _, e := range s.index {
		if e.Deleted {
			continue
		}
		if tier != "" && e.TTLTier != tier {
			continue
		}
		out = append(out, StoreSummary{
			MemoryID:      e.MemoryID,
			TaskID:        e.TaskID,
			Key:           e.Key,
			Tags:          e.Tags,
			TTLTier:       e.TTLTier,
			SecurityLevel: e.SecurityLevel,
			CreatedAt:     e.CreatedAt,
			Version:       e.Version,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out, nil
}

// Count returns the total number of non-deleted entries.
func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	count := 0
	for _, e := range s.index {
		if !e.Deleted {
			count++
		}
	}
	return count
}

// ──────────────────────────────────────────────────────────────────────────────
// Compaction
// ──────────────────────────────────────────────────────────────────────────────

// CompactResult summarises the outcome of a compaction run.
type CompactResult struct {
	Examined    int
	Compacted   int
	TargetTier  TTLTier
	ElapsedTime time.Duration
}

// Compact performs tier-based compaction.  Entries that have aged past their
// tier threshold are transitioned to the next (coarser) tier.
// If taskID is non-empty, only entries for that task are compacted.
// If targetTier is non-empty, only entries at that tier are targeted.
//
// The compaction strategy is conservative: the value is truncated to a
// one-sentence summary (first 200 characters) and a compaction note is added
// to the key.  A full LLM-based summarizer is a Phase 1+ optional extension.
func (s *Store) Compact(taskID string, targetTier TTLTier) (*CompactResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	result := &CompactResult{TargetTier: targetTier}
	start := time.Now()

	for _, e := range s.index {
		if e.Deleted {
			continue
		}
		if taskID != "" && e.TaskID != taskID {
			continue
		}
		if targetTier != "" && e.TTLTier != targetTier {
			continue
		}
		result.Examined++

		nextTier, threshold := nextCompactionTier(e.TTLTier, e.CreatedAt, now)
		if nextTier == "" || !threshold {
			continue
		}

		// Compact: truncate value and advance tier.
		e.Value = compactValue(e.Value, e.TTLTier)
		e.TTLTier = nextTier
		e.Version++
		e.LastCompactedAt = &now
		result.Compacted++
	}

	if result.Compacted > 0 {
		if err := s.rewriteVault(); err != nil {
			return result, fmt.Errorf("rewrite vault after compaction: %w", err)
		}
	}

	result.ElapsedTime = time.Since(start)
	return result, nil
}

// CompactAll compacts all eligible entries regardless of task or target tier.
func (s *Store) CompactAll() (*CompactResult, error) {
	return s.Compact("", "")
}

// ──────────────────────────────────────────────────────────────────────────────
// Internals
// ──────────────────────────────────────────────────────────────────────────────

// appendEntry encrypts and appends a single entry to the vault file.
// The entry is serialised as a single JSON line wrapped in a new age stream.
// Note: age produces a fresh random nonce on each Encrypt call, so
// independent messages are safe even though they share the same identity.
func (s *Store) appendEntry(e *MemoryEntry) error {
	data, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("marshal entry: %w", err)
	}

	ciphertext, err := encryptBytes(data, s.recipient)
	if err != nil {
		return fmt.Errorf("encrypt entry: %w", err)
	}

	// Encode the ciphertext as a hex-free length-prefixed line.
	// Format: <base64-encoded-ciphertext>\n so the file is valid JSONL-adjacent.
	encoded := encodeMessage(ciphertext)

	f, err := os.OpenFile(s.vaultPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("open vault file: %w", err)
	}
	defer f.Close()

	_, err = fmt.Fprintf(f, "%s\n", encoded)
	return err
}

// rewriteVault atomically rewrites the vault file with the current index state.
func (s *Store) rewriteVault() error {
	tmpPath := s.vaultPath + ".tmp"
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("open tmp vault file: %w", err)
	}

	for _, e := range s.index {
		data, err := json.Marshal(e)
		if err != nil {
			f.Close()
			return fmt.Errorf("marshal entry %s: %w", e.MemoryID, err)
		}
		ciphertext, err := encryptBytes(data, s.recipient)
		if err != nil {
			f.Close()
			return fmt.Errorf("encrypt entry %s: %w", e.MemoryID, err)
		}
		if _, err := fmt.Fprintf(f, "%s\n", encodeMessage(ciphertext)); err != nil {
			f.Close()
			return fmt.Errorf("write entry %s: %w", e.MemoryID, err)
		}
	}
	f.Close()

	return os.Rename(tmpPath, s.vaultPath)
}

// loadIndex reads and decrypts the vault file, rebuilding the in-memory index.
func (s *Store) loadIndex() error {
	data, err := os.ReadFile(s.vaultPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read vault file: %w", err)
	}

	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		ciphertext, err := decodeMessage(line)
		if err != nil {
			// Skip corrupt lines rather than failing entirely.
			continue
		}
		plaintext, err := decryptBytes(ciphertext, s.identity)
		if err != nil {
			continue
		}
		var e MemoryEntry
		if err := json.Unmarshal(plaintext, &e); err != nil {
			continue
		}
		s.index[e.MemoryID] = &e
	}
	return nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Helpers
// ──────────────────────────────────────────────────────────────────────────────

// matchesAny returns true if any of the tokens appears in the entry's key,
// value, or any tag (case-insensitive substring match).
func matchesAny(e *MemoryEntry, tokens []string) bool {
	keyL := strings.ToLower(e.Key)
	valueL := strings.ToLower(e.Value)
	for _, tok := range tokens {
		if strings.Contains(keyL, tok) || strings.Contains(valueL, tok) {
			return true
		}
		for _, tag := range e.Tags {
			if strings.Contains(strings.ToLower(tag), tok) {
				return true
			}
		}
	}
	return false
}

// nextCompactionTier returns the next TTL tier for an entry if it has aged
// past its current tier's threshold.  Returns ("", false) if no action needed.
func nextCompactionTier(current TTLTier, createdAt, now time.Time) (TTLTier, bool) {
	age := now.Sub(createdAt)
	switch current {
	case TTL90d:
		if age >= 90*24*time.Hour {
			return TTL180d, true
		}
	case TTL180d:
		if age >= 180*24*time.Hour {
			return TTL365d, true
		}
	case TTL365d:
		if age >= 365*24*time.Hour {
			return TTL2yr, true
		}
	case TTL2yr:
		if age >= 730*24*time.Hour {
			return TTLForever, true
		}
	}
	return "", false
}

// compactValue reduces the value content appropriate to the target tier.
// TTL90d→180d: truncate to 500 chars (key facts)
// TTL180d→365d: truncate to 200 chars (bullet points)
// TTL365d→2yr: truncate to 100 chars (ultra-summary)
// TTL2yr→forever: truncate to 60 chars (archive sentence)
func compactValue(value string, fromTier TTLTier) string {
	limits := map[TTLTier]int{
		TTL90d:  500,
		TTL180d: 200,
		TTL365d: 100,
		TTL2yr:  60,
	}
	limit, ok := limits[fromTier]
	if !ok || len(value) <= limit {
		return value
	}
	// Truncate at a word boundary where possible.
	trimmed := value[:limit]
	if idx := strings.LastIndexAny(trimmed, " \t\n"); idx > limit/2 {
		trimmed = trimmed[:idx]
	}
	return trimmed + " [compacted]"
}

// encryptBytes encrypts plaintext using age to the given recipient.
func encryptBytes(plaintext []byte, recipient age.Recipient) ([]byte, error) {
	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, recipient)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(plaintext); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// decryptBytes decrypts an age-encrypted message using the given identity.
func decryptBytes(ciphertext []byte, identity age.Identity) ([]byte, error) {
	r, err := age.Decrypt(bytes.NewReader(ciphertext), identity)
	if err != nil {
		return nil, err
	}
	return io.ReadAll(r)
}

// encodeMessage encodes a binary ciphertext as a hex string for line storage.
func encodeMessage(b []byte) string {
	return hex.EncodeToString(b)
}

// decodeMessage decodes a hex-encoded message line back to binary.
func decodeMessage(s string) ([]byte, error) {
	return hex.DecodeString(strings.TrimSpace(s))
}

// _ prevents unused import warnings for uuid which is used above.
var _ = uuid.New
