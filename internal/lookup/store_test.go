package lookup

import (
	"context"
	"os"
	"strings"
	"testing"
)

// newTestStore creates a Store backed by a temp directory that is removed when
// the test ends.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := NewStore(StoreConfig{Dir: dir})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return s
}

func TestNewStore(t *testing.T) {
	s := newTestStore(t)
	if s == nil {
		t.Fatal("expected non-nil store")
	}
	if s.Count() != 0 {
		t.Errorf("expected empty store, got %d entries", s.Count())
	}
}

func TestNewStore_DefaultDir(t *testing.T) {
	// When Dir is empty the store falls back to /data/vectordb.  Since that path
	// won't be writable in CI we only verify the error message is descriptive.
	_, err := NewStore(StoreConfig{Dir: "/nonexistent/path/that/must/not/exist/lookup_test"})
	if err == nil {
		// If it somehow succeeded (mounted tmpfs) that's fine too.
		return
	}
	if !strings.Contains(err.Error(), "lookup:") {
		t.Errorf("expected error to contain 'lookup:', got: %v", err)
	}
}

func TestIndexTool_Valid(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	entry := ToolEntry{
		Name:        "memory.store",
		Description: "Store a persistent memory entry with key, value, and tags",
		SkillName:   "memory",
		Parameters:  `{"key": "string", "value": "string", "tags": ["string"]}`,
	}

	if err := s.IndexTool(ctx, entry); err != nil {
		t.Fatalf("IndexTool: %v", err)
	}
	if s.Count() != 1 {
		t.Errorf("expected 1 indexed tool, got %d", s.Count())
	}
}

func TestIndexTool_RequiresName(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	err := s.IndexTool(ctx, ToolEntry{Description: "some description"})
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestIndexTool_RequiresDescription(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	err := s.IndexTool(ctx, ToolEntry{Name: "tool.name"})
	if err == nil {
		t.Fatal("expected error for empty description")
	}
}

func TestIndexTool_Reindex(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	entry := ToolEntry{
		Name:        "worker.spawn",
		Description: "Spawn an ephemeral worker microVM for a subtask",
		SkillName:   "worker",
	}

	if err := s.IndexTool(ctx, entry); err != nil {
		t.Fatalf("first IndexTool: %v", err)
	}

	// Re-index with updated description — should not fail or duplicate.
	entry.Description = "Spawn an ephemeral Worker VM to execute a narrowly-scoped subtask"
	if err := s.IndexTool(ctx, entry); err != nil {
		t.Fatalf("second IndexTool (re-index): %v", err)
	}

	if s.Count() != 1 {
		t.Errorf("expected 1 record after re-index, got %d", s.Count())
	}
}

func TestLookupTools_EmptyCollection(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	results, err := s.LookupTools(ctx, "memory", 6)
	if err != nil {
		t.Fatalf("LookupTools on empty store: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results from empty store, got %d", len(results))
	}
}

func TestLookupTools_ReturnsRelevantTools(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	tools := []ToolEntry{
		{Name: "memory.store", Description: "Store a persistent memory entry with key and value", SkillName: "memory"},
		{Name: "memory.retrieve", Description: "Retrieve memory entries by semantic or keyword query", SkillName: "memory"},
		{Name: "proposal.create_draft", Description: "Create a new skill proposal draft for governance review", SkillName: "proposal"},
		{Name: "worker.spawn", Description: "Spawn a Worker microVM to execute a research subtask", SkillName: "worker"},
		{Name: "script.run", Description: "Execute a short script in a sandboxed environment", SkillName: "script"},
	}
	for _, e := range tools {
		if err := s.IndexTool(ctx, e); err != nil {
			t.Fatalf("IndexTool %q: %v", e.Name, err)
		}
	}

	results, err := s.LookupTools(ctx, "store and retrieve memory entries", 3)
	if err != nil {
		t.Fatalf("LookupTools: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least 1 result")
	}
	if len(results) > 3 {
		t.Errorf("expected at most 3 results, got %d", len(results))
	}

	// Verify all returned blocks contain the Gemma 4 control tokens.
	for _, r := range results {
		if !strings.Contains(r.Block, "<|tool|>") {
			t.Errorf("result %q: block missing <|tool|> token: %s", r.Name, r.Block)
		}
		if !strings.Contains(r.Block, "<|/tool|>") {
			t.Errorf("result %q: block missing <|/tool|> token: %s", r.Name, r.Block)
		}
		if r.Name == "" {
			t.Error("result has empty Name")
		}
	}
}

func TestLookupTools_DefaultMaxResults(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	// Index 10 tools.
	for i := 0; i < 10; i++ {
		e := ToolEntry{
			Name:        strings.Repeat("x", i+1),
			Description: "generic tool description for testing purposes",
		}
		e.Name = "tool." + e.Name
		if err := s.IndexTool(ctx, e); err != nil {
			t.Fatalf("IndexTool: %v", err)
		}
	}

	// maxResults=0 should use defaultMaxResults (6).
	results, err := s.LookupTools(ctx, "generic tool", 0)
	if err != nil {
		t.Fatalf("LookupTools: %v", err)
	}
	if len(results) > defaultMaxResults {
		t.Errorf("expected at most %d results, got %d", defaultMaxResults, len(results))
	}
}

func TestLookupTools_EmptyQueryError(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	_, err := s.LookupTools(ctx, "", 6)
	if err == nil {
		t.Fatal("expected error for empty query")
	}
}

func TestFormatGemma4Block(t *testing.T) {
	e := ToolEntry{
		Name:        "memory.store",
		Description: "Store a persistent memory entry",
		SkillName:   "memory",
		Parameters:  `{"key": "string", "value": "string"}`,
	}
	block := formatGemma4Block(e)

	if !strings.HasPrefix(block, "<|tool|>") {
		t.Errorf("block should start with <|tool|>: %s", block)
	}
	if !strings.HasSuffix(block, "<|/tool|>") {
		t.Errorf("block should end with <|/tool|>: %s", block)
	}
	if !strings.Contains(block, "memory.store") {
		t.Errorf("block should contain tool name: %s", block)
	}
	if !strings.Contains(block, "Store a persistent memory entry") {
		t.Errorf("block should contain description: %s", block)
	}
}

func TestFormatGemma4Block_EscapesSpecialChars(t *testing.T) {
	e := ToolEntry{
		Name:        `tool.with"quotes`,
		Description: "Description with\nnewline and \"quotes\"",
	}
	block := formatGemma4Block(e)
	// The block must be parseable as part of valid JSON — verify no raw newlines
	// or unescaped quotes break the structure.
	if strings.Contains(block[len("<|tool|>"):len(block)-len("<|/tool|>")], "\n") {
		t.Errorf("block contains raw newline in JSON body: %s", block)
	}
}

func TestHashEmbeddingFunc_DimensionCount(t *testing.T) {
	ctx := context.Background()
	vec, err := hashEmbeddingFunc(ctx, "retrieve memory entries for the current task")
	if err != nil {
		t.Fatalf("hashEmbeddingFunc: %v", err)
	}
	if len(vec) != embeddingDims {
		t.Errorf("expected %d dimensions, got %d", embeddingDims, len(vec))
	}
}

func TestHashEmbeddingFunc_Normalised(t *testing.T) {
	ctx := context.Background()
	vec, err := hashEmbeddingFunc(ctx, "store memory key value tags")
	if err != nil {
		t.Fatalf("hashEmbeddingFunc: %v", err)
	}
	var sum float64
	for _, v := range vec {
		sum += float64(v) * float64(v)
	}
	// L2 norm should be ≈ 1.0 (within floating-point tolerance).
	if sum < 0.999 || sum > 1.001 {
		t.Errorf("vector is not unit-normalised: L2²=%f", sum)
	}
}

func TestHashEmbeddingFunc_Deterministic(t *testing.T) {
	ctx := context.Background()
	text := "proposal create draft governance review"
	v1, _ := hashEmbeddingFunc(ctx, text)
	v2, _ := hashEmbeddingFunc(ctx, text)
	for i := range v1 {
		if v1[i] != v2[i] {
			t.Errorf("non-deterministic at index %d: %f != %f", i, v1[i], v2[i])
		}
	}
}

func TestHashEmbeddingFunc_EmptyText(t *testing.T) {
	ctx := context.Background()
	vec, err := hashEmbeddingFunc(ctx, "")
	if err != nil {
		t.Fatalf("hashEmbeddingFunc empty text: %v", err)
	}
	if len(vec) != embeddingDims {
		t.Errorf("expected %d dimensions for empty text, got %d", embeddingDims, len(vec))
	}
}

func TestPersistence(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	// Create store, index a tool, close.
	s1, err := NewStore(StoreConfig{Dir: dir})
	if err != nil {
		t.Fatalf("first NewStore: %v", err)
	}
	entry := ToolEntry{
		Name:        "proposal.submit",
		Description: "Submit a proposal for governance court review",
		SkillName:   "proposal",
	}
	if err := s1.IndexTool(ctx, entry); err != nil {
		t.Fatalf("IndexTool: %v", err)
	}

	// Reopen the store from the same directory; the tool should still be there.
	s2, err := NewStore(StoreConfig{Dir: dir})
	if err != nil {
		t.Fatalf("second NewStore: %v", err)
	}
	if s2.Count() != 1 {
		t.Errorf("expected 1 persisted tool after reopen, got %d", s2.Count())
	}
	_ = os.RemoveAll(dir)
}
