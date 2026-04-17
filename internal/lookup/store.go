// Package lookup implements the dynamic semantic tool-lookup skill for AegisClaw.
//
// It maintains a chromem-go vector collection named "skills" that is persisted at
// the path provided in Config.Dir.  Every new skill created by the builder is
// automatically indexed via IndexTool.  At query time LookupTools returns the
// most relevant tool blocks already wrapped in Gemma 4 native control tokens so
// the agent can consume them immediately without additional formatting.
//
// Embedding model
// ───────────────
// The package ships a pure-Go hash-based embedding function (384 dimensions,
// matching all-MiniLM-L6-v2's output size) that requires no CGO or external
// binaries.  It is designed to be a drop-in replacement: swap EmbeddingFunc in
// StoreConfig for a real ONNX/onnxruntime-go wrapper when the model weights are
// available.
//
// Security model
// ──────────────
// The agent never touches the chromem-go database directly.  All reads and
// writes go through this package's exported methods, which are the sole path
// into the vector store.  No network access is required.
package lookup

import (
	"context"
	"fmt"
	"hash/fnv"
	"math"
	"os"
	"strings"
	"sync"

	chromem "github.com/philippgille/chromem-go"
	"go.uber.org/zap"
)

const (
	// collectionName is the chromem-go collection used for skill/tool records.
	collectionName = "skills"

	// embeddingDims matches all-MiniLM-L6-v2's output dimension.
	embeddingDims = 384

	// defaultMaxResults is the upper bound returned when the caller passes 0.
	defaultMaxResults = 6
)

// ToolEntry holds the metadata for a single tool that is to be indexed.
type ToolEntry struct {
	// Name is the qualified tool name, e.g. "memory.store" or "proposal.create_draft".
	Name string
	// Description is the human-readable description used for semantic matching.
	Description string
	// SkillName is the owning skill (may be empty for built-in daemon tools).
	SkillName string
	// Parameters is a free-form string describing the tool's input parameters.
	// It is stored as metadata alongside the tool block and included in the
	// Gemma 4 output so the agent knows how to call the tool.
	Parameters string
}

// FormattedTool is the result of a LookupTools query.  The Block field is ready
// to be inserted verbatim into a Gemma 4 prompt.
type FormattedTool struct {
	Name  string
	Block string // Gemma 4 <|tool|> … <|/tool|> block
}

// StoreConfig holds construction parameters for the lookup Store.
type StoreConfig struct {
	// Dir is the directory where the persistent chromem-go database is stored.
	// Defaults to /data/vectordb (the microVM-style default) but can be
	// overridden for the host-daemon use case.
	Dir string

	// EmbeddingFunc is an optional custom embedding function.  When nil, the
	// built-in hash-based embedding is used.
	EmbeddingFunc chromem.EmbeddingFunc

	// Logger is used for structured operational logging.  When nil, a no-op
	// logger is used.
	Logger *zap.Logger
}

// Store is the persistent semantic lookup store.
// All exported methods are safe for concurrent use.
type Store struct {
	cfg        StoreConfig
	db         *chromem.DB
	collection *chromem.Collection
	embedFn    chromem.EmbeddingFunc
	logger     *zap.Logger
	mu         sync.RWMutex
}

// NewStore opens (or creates) the lookup Store at cfg.Dir.
// The chromem-go database is loaded from disk on the first call; subsequent
// calls using the same directory are safe (the DB is keyed by document ID).
func NewStore(cfg StoreConfig) (*Store, error) {
	if cfg.Dir == "" {
		cfg.Dir = "/data/vectordb"
	}
	if err := os.MkdirAll(cfg.Dir, 0700); err != nil {
		return nil, fmt.Errorf("lookup: create dir %s: %w", cfg.Dir, err)
	}

	logger := cfg.Logger
	if logger == nil {
		logger = zap.NewNop()
	}

	embedFn := cfg.EmbeddingFunc
	if embedFn == nil {
		embedFn = hashEmbeddingFunc
	}

	db, err := chromem.NewPersistentDB(cfg.Dir, false)
	if err != nil {
		return nil, fmt.Errorf("lookup: open persistent DB at %s: %w", cfg.Dir, err)
	}

	col, err := db.GetOrCreateCollection(collectionName, nil, embedFn)
	if err != nil {
		return nil, fmt.Errorf("lookup: get or create collection %q: %w", collectionName, err)
	}

	s := &Store{
		cfg:        cfg,
		db:         db,
		collection: col,
		embedFn:    embedFn,
		logger:     logger,
	}

	logger.Info("lookup store ready",
		zap.String("dir", cfg.Dir),
		zap.Int("indexed", col.Count()),
	)
	return s, nil
}

// IndexTool adds or updates a tool record in the vector collection.
// If a document with the same ID already exists it is deleted and re-added so
// the embedding stays in sync with the latest description.
func (s *Store) IndexTool(ctx context.Context, entry ToolEntry) error {
	if entry.Name == "" {
		return fmt.Errorf("lookup: tool name is required")
	}
	if entry.Description == "" {
		return fmt.Errorf("lookup: tool description is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Build the indexable content: name + description + parameters.
	content := buildIndexContent(entry)

	// Delete any existing record with the same ID to allow re-indexing.
	if existing, err := s.collection.GetByID(ctx, entry.Name); err == nil && existing.ID != "" {
		if delErr := s.collection.Delete(ctx, nil, nil, entry.Name); delErr != nil {
			s.logger.Warn("lookup: failed to delete existing tool before re-index",
				zap.String("tool", entry.Name),
				zap.Error(delErr),
			)
		}
	}

	metadata := map[string]string{
		"skill_name":  entry.SkillName,
		"description": entry.Description,
		"parameters":  entry.Parameters,
		"tool_block":  formatGemma4Block(entry),
	}

	doc := chromem.Document{
		ID:       entry.Name,
		Content:  content,
		Metadata: metadata,
	}

	if err := s.collection.AddDocument(ctx, doc); err != nil {
		return fmt.Errorf("lookup: index tool %q: %w", entry.Name, err)
	}

	s.logger.Info("lookup: tool indexed",
		zap.String("tool", entry.Name),
		zap.String("skill", entry.SkillName),
	)
	return nil
}

// LookupTools returns the most relevant tools for the given query, formatted
// as Gemma 4 native control-token blocks.  maxResults ≤ 0 is treated as the
// default (6).  If fewer tools are indexed than maxResults the method returns
// all of them.
func (s *Store) LookupTools(ctx context.Context, query string, maxResults int) ([]FormattedTool, error) {
	if strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("lookup: query must not be empty")
	}
	if maxResults <= 0 {
		maxResults = defaultMaxResults
	}

	s.mu.RLock()
	total := s.collection.Count()
	s.mu.RUnlock()

	if total == 0 {
		return nil, nil
	}
	if maxResults > total {
		maxResults = total
	}

	s.mu.RLock()
	results, err := s.collection.Query(ctx, query, maxResults, nil, nil)
	s.mu.RUnlock()

	if err != nil {
		return nil, fmt.Errorf("lookup: query %q: %w", query, err)
	}

	out := make([]FormattedTool, 0, len(results))
	for _, r := range results {
		block, ok := r.Metadata["tool_block"]
		if !ok || block == "" {
			// Fallback: rebuild from metadata.
			block = formatGemma4BlockFromMetadata(r.ID, r.Metadata)
		}
		out = append(out, FormattedTool{
			Name:  r.ID,
			Block: block,
		})
	}

	s.logger.Info("lookup: query completed",
		zap.String("query", query),
		zap.Int("results", len(out)),
	)
	return out, nil
}

// Count returns the number of indexed tool entries.
func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.collection.Count()
}

// ──────────────────────────────────────────────────────────────────────────────
// Gemma 4 formatting
// ──────────────────────────────────────────────────────────────────────────────

// formatGemma4Block produces a Gemma 4 native tool block for the given entry.
// Format reference: https://ai.google.dev/gemma/docs/gemma-tooluse
//
// Example output:
//
//	<|tool|>{"name":"memory.store","description":"…","parameters":"…"}<|/tool|>
func formatGemma4Block(e ToolEntry) string {
	var sb strings.Builder
	sb.WriteString("<|tool|>")
	sb.WriteString(`{"name":`)
	sb.WriteString(jsonQuote(e.Name))
	sb.WriteString(`,"description":`)
	sb.WriteString(jsonQuote(e.Description))
	if e.Parameters != "" {
		sb.WriteString(`,"parameters":`)
		sb.WriteString(jsonQuote(e.Parameters))
	}
	if e.SkillName != "" {
		sb.WriteString(`,"skill":`)
		sb.WriteString(jsonQuote(e.SkillName))
	}
	sb.WriteString("}")
	sb.WriteString("<|/tool|>")
	return sb.String()
}

// formatGemma4BlockFromMetadata rebuilds a Gemma 4 block from stored metadata
// when the pre-formatted block is absent (e.g. legacy records).
func formatGemma4BlockFromMetadata(name string, meta map[string]string) string {
	return formatGemma4Block(ToolEntry{
		Name:        name,
		Description: meta["description"],
		SkillName:   meta["skill_name"],
		Parameters:  meta["parameters"],
	})
}

// jsonQuote returns a JSON-safe double-quoted string with special characters escaped.
func jsonQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\r", `\r`)
	s = strings.ReplaceAll(s, "\t", `\t`)
	return `"` + s + `"`
}

// ──────────────────────────────────────────────────────────────────────────────
// Embedding function
// ──────────────────────────────────────────────────────────────────────────────

// hashEmbeddingFunc is a pure-Go deterministic embedding function that produces
// 384-dimensional normalised float32 vectors.  It uses word-level and character
// bi-gram FNV-32 hashes to fill the vector, giving reasonable semantic proximity
// for near-duplicate phrases without requiring any model weights or CGO.
//
// This function is intentionally a drop-in placeholder: replace it with a real
// ONNX/all-MiniLM-L6-v2 implementation when model weights are available.
func hashEmbeddingFunc(_ context.Context, text string) ([]float32, error) {
	vec := make([]float32, embeddingDims)

	text = strings.ToLower(strings.TrimSpace(text))
	words := strings.Fields(text)

	// Word-level contributions.
	for _, word := range words {
		h := fnvHash(word)
		idx := int(h) % embeddingDims
		if idx < 0 {
			idx = -idx
		}
		vec[idx] += 1.0
	}

	// Character bi-gram contributions for sub-word similarity.
	runes := []rune(text)
	for i := 0; i+1 < len(runes); i++ {
		bigram := string(runes[i : i+2])
		h := fnvHash(bigram)
		idx := int(h) % embeddingDims
		if idx < 0 {
			idx = -idx
		}
		vec[idx] += 0.5
	}

	// L2-normalise so chromem-go's cosine similarity (dot product) is valid.
	normalise(vec)
	return vec, nil
}

// fnvHash returns an int64 hash of s using FNV-32a.
func fnvHash(s string) int64 {
	h := fnv.New32a()
	h.Write([]byte(s))  //nolint:errcheck
	return int64(h.Sum32())
}

// normalise scales vec to unit length in-place.
func normalise(vec []float32) {
	var sum float64
	for _, v := range vec {
		sum += float64(v) * float64(v)
	}
	if sum == 0 {
		return
	}
	scale := float32(1.0 / math.Sqrt(sum))
	for i := range vec {
		vec[i] *= scale
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Helpers
// ──────────────────────────────────────────────────────────────────────────────

// buildIndexContent concatenates the tool's name, description, and parameters
// into a single string that is embedded and stored as the chromem-go document
// content.  Richer content improves recall.
func buildIndexContent(e ToolEntry) string {
	parts := []string{e.Name, e.Description}
	if e.Parameters != "" {
		parts = append(parts, e.Parameters)
	}
	if e.SkillName != "" {
		parts = append(parts, "skill:"+e.SkillName)
	}
	return strings.Join(parts, " ")
}
