package vault

import (
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"go.uber.org/zap/zaptest"
)

func testKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate test key: %v", err)
	}
	return priv
}

func TestNewVault_CreatesDir(t *testing.T) {
	dir := t.TempDir()
	storeDir := filepath.Join(dir, "secrets")
	logger := zaptest.NewLogger(t)

	v, err := NewVault(storeDir, testKey(t), logger)
	if err != nil {
		t.Fatalf("NewVault failed: %v", err)
	}

	if v == nil {
		t.Fatal("expected non-nil vault")
	}

	info, statErr := os.Stat(storeDir)
	if statErr != nil {
		t.Fatalf("store dir not created: %v", statErr)
	}
	if !info.IsDir() {
		t.Fatal("store dir is not a directory")
	}
}

func TestNewVault_EmptyDir(t *testing.T) {
	logger := zaptest.NewLogger(t)
	_, err := NewVault("", testKey(t), logger)
	if err == nil {
		t.Fatal("expected error for empty dir")
	}
}

func TestNewVault_InvalidKey(t *testing.T) {
	dir := t.TempDir()
	logger := zaptest.NewLogger(t)
	_, err := NewVault(dir, []byte("short"), logger)
	if err == nil {
		t.Fatal("expected error for invalid key")
	}
}

func TestNewVault_PersistentIdentity(t *testing.T) {
	dir := t.TempDir()
	storeDir := filepath.Join(dir, "secrets")
	logger := zaptest.NewLogger(t)
	key := testKey(t)

	// First init creates identity
	v1, err := NewVault(storeDir, key, logger)
	if err != nil {
		t.Fatalf("first NewVault failed: %v", err)
	}

	// Add a secret
	if err := v1.Add("testkey", "skill-1", []byte("hello")); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	// Second init reuses identity — must be able to decrypt
	v2, err := NewVault(storeDir, key, logger)
	if err != nil {
		t.Fatalf("second NewVault failed: %v", err)
	}

	data, err := v2.Get("testkey")
	if err != nil {
		t.Fatalf("Get after reopen failed: %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("expected %q, got %q", "hello", string(data))
	}
}

func TestVault_AddAndGet(t *testing.T) {
	dir := t.TempDir()
	logger := zaptest.NewLogger(t)

	v, err := NewVault(dir, testKey(t), logger)
	if err != nil {
		t.Fatalf("NewVault failed: %v", err)
	}

	if err := v.Add("dbpass", "skill-db", []byte("s3cret!")); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	got, err := v.Get("dbpass")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if string(got) != "s3cret!" {
		t.Fatalf("expected %q, got %q", "s3cret!", string(got))
	}
}

func TestVault_AddUpdate(t *testing.T) {
	dir := t.TempDir()
	logger := zaptest.NewLogger(t)

	v, err := NewVault(dir, testKey(t), logger)
	if err != nil {
		t.Fatalf("NewVault failed: %v", err)
	}

	if err := v.Add("token", "skill-a", []byte("v1")); err != nil {
		t.Fatalf("first Add failed: %v", err)
	}

	// Update same secret
	if err := v.Add("token", "skill-b", []byte("v2")); err != nil {
		t.Fatalf("update Add failed: %v", err)
	}

	got, err := v.Get("token")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if string(got) != "v2" {
		t.Fatalf("expected %q, got %q", "v2", string(got))
	}

	entry, ok := v.GetEntry("token")
	if !ok {
		t.Fatal("GetEntry returned false")
	}
	if entry.SkillID != "skill-b" {
		t.Fatalf("expected skill %q, got %q", "skill-b", entry.SkillID)
	}
}

func TestVault_Delete(t *testing.T) {
	dir := t.TempDir()
	logger := zaptest.NewLogger(t)

	v, err := NewVault(dir, testKey(t), logger)
	if err != nil {
		t.Fatalf("NewVault failed: %v", err)
	}

	if err := v.Add("ephemeral", "skill-e", []byte("temp")); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	if err := v.Delete("ephemeral"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	if v.Has("ephemeral") {
		t.Fatal("secret should be gone after delete")
	}

	_, err = v.Get("ephemeral")
	if err == nil {
		t.Fatal("Get should fail after delete")
	}
}

func TestVault_DeleteNotFound(t *testing.T) {
	dir := t.TempDir()
	logger := zaptest.NewLogger(t)

	v, err := NewVault(dir, testKey(t), logger)
	if err != nil {
		t.Fatalf("NewVault failed: %v", err)
	}

	err = v.Delete("nonexistent")
	if err == nil {
		t.Fatal("expected error deleting nonexistent secret")
	}
}

func TestVault_List(t *testing.T) {
	dir := t.TempDir()
	logger := zaptest.NewLogger(t)

	v, err := NewVault(dir, testKey(t), logger)
	if err != nil {
		t.Fatalf("NewVault failed: %v", err)
	}

	if entries := v.List(); len(entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(entries))
	}

	v.Add("a", "s1", []byte("data-a"))
	v.Add("b", "s1", []byte("data-b"))
	v.Add("c", "s2", []byte("data-c"))

	entries := v.List()
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
}

func TestVault_ListForSkill(t *testing.T) {
	dir := t.TempDir()
	logger := zaptest.NewLogger(t)

	v, err := NewVault(dir, testKey(t), logger)
	if err != nil {
		t.Fatalf("NewVault failed: %v", err)
	}

	v.Add("x", "skill-alpha", []byte("1"))
	v.Add("y", "skill-alpha", []byte("2"))
	v.Add("z", "skill-beta", []byte("3"))

	alpha := v.ListForSkill("skill-alpha")
	if len(alpha) != 2 {
		t.Fatalf("expected 2 for skill-alpha, got %d", len(alpha))
	}

	beta := v.ListForSkill("skill-beta")
	if len(beta) != 1 {
		t.Fatalf("expected 1 for skill-beta, got %d", len(beta))
	}

	none := v.ListForSkill("skill-gamma")
	if len(none) != 0 {
		t.Fatalf("expected 0 for skill-gamma, got %d", len(none))
	}
}

func TestVault_Has(t *testing.T) {
	dir := t.TempDir()
	logger := zaptest.NewLogger(t)

	v, err := NewVault(dir, testKey(t), logger)
	if err != nil {
		t.Fatalf("NewVault failed: %v", err)
	}

	if v.Has("missing") {
		t.Fatal("Has should be false for missing secret")
	}

	v.Add("present", "s1", []byte("val"))

	if !v.Has("present") {
		t.Fatal("Has should be true for added secret")
	}
}

func TestVault_GetEntry(t *testing.T) {
	dir := t.TempDir()
	logger := zaptest.NewLogger(t)

	v, err := NewVault(dir, testKey(t), logger)
	if err != nil {
		t.Fatalf("NewVault failed: %v", err)
	}

	v.Add("meta", "skill-m", []byte("data"))

	entry, ok := v.GetEntry("meta")
	if !ok {
		t.Fatal("GetEntry returned false")
	}
	if entry.Name != "meta" {
		t.Fatalf("expected name %q, got %q", "meta", entry.Name)
	}
	if entry.SkillID != "skill-m" {
		t.Fatalf("expected skill %q, got %q", "skill-m", entry.SkillID)
	}
	if entry.Size != 4 {
		t.Fatalf("expected size 4, got %d", entry.Size)
	}
	if entry.CreatedAt.IsZero() {
		t.Fatal("CreatedAt should not be zero")
	}

	_, ok = v.GetEntry("nope")
	if ok {
		t.Fatal("GetEntry should return false for missing")
	}
}

func TestVault_NameValidation(t *testing.T) {
	dir := t.TempDir()
	logger := zaptest.NewLogger(t)

	v, err := NewVault(dir, testKey(t), logger)
	if err != nil {
		t.Fatalf("NewVault failed: %v", err)
	}

	invalid := []string{
		"",
		"1startsWithDigit",
		"has spaces",
		"has.dot",
		"../traversal",
		"/absolute",
		"a/b",
	}

	for _, name := range invalid {
		if err := v.Add(name, "s1", []byte("val")); err == nil {
			t.Errorf("expected error for invalid name %q", name)
		}
	}

	// Valid names
	valid := []string{
		"a",
		"mySecret",
		"MY_SECRET",
		"api-key",
		"Token_123",
	}

	for _, name := range valid {
		if err := v.Add(name, "s1", []byte("val")); err != nil {
			t.Errorf("unexpected error for valid name %q: %v", name, err)
		}
	}
}

func TestVault_AddEmptyValue(t *testing.T) {
	dir := t.TempDir()
	logger := zaptest.NewLogger(t)

	v, err := NewVault(dir, testKey(t), logger)
	if err != nil {
		t.Fatalf("NewVault failed: %v", err)
	}

	err = v.Add("empty", "s1", []byte{})
	if err == nil {
		t.Fatal("expected error for empty value")
	}
}

func TestVault_AddEmptySkill(t *testing.T) {
	dir := t.TempDir()
	logger := zaptest.NewLogger(t)

	v, err := NewVault(dir, testKey(t), logger)
	if err != nil {
		t.Fatalf("NewVault failed: %v", err)
	}

	err = v.Add("noSkill", "", []byte("data"))
	if err == nil {
		t.Fatal("expected error for empty skill ID")
	}
}

func TestVault_EncryptDecryptRoundtrip(t *testing.T) {
	dir := t.TempDir()
	logger := zaptest.NewLogger(t)

	v, err := NewVault(dir, testKey(t), logger)
	if err != nil {
		t.Fatalf("NewVault failed: %v", err)
	}

	testData := []byte("The quick brown fox jumps over the lazy dog. 🦊")

	encrypted, err := v.encrypt(testData)
	if err != nil {
		t.Fatalf("encrypt failed: %v", err)
	}

	if string(encrypted) == string(testData) {
		t.Fatal("encrypted data should differ from plaintext")
	}

	decrypted, err := v.decrypt(encrypted)
	if err != nil {
		t.Fatalf("decrypt failed: %v", err)
	}

	if string(decrypted) != string(testData) {
		t.Fatalf("roundtrip mismatch: expected %q, got %q", string(testData), string(decrypted))
	}
}

func TestVault_LargeBinaryData(t *testing.T) {
	dir := t.TempDir()
	logger := zaptest.NewLogger(t)

	v, err := NewVault(dir, testKey(t), logger)
	if err != nil {
		t.Fatalf("NewVault failed: %v", err)
	}

	// 64KB of random data
	bigData := make([]byte, 65536)
	if _, err := rand.Read(bigData); err != nil {
		t.Fatalf("failed to generate random data: %v", err)
	}

	if err := v.Add("bigkey", "skill-big", bigData); err != nil {
		t.Fatalf("Add big data failed: %v", err)
	}

	got, err := v.Get("bigkey")
	if err != nil {
		t.Fatalf("Get big data failed: %v", err)
	}

	if len(got) != len(bigData) {
		t.Fatalf("size mismatch: expected %d, got %d", len(bigData), len(got))
	}

	for i := range bigData {
		if got[i] != bigData[i] {
			t.Fatalf("data mismatch at byte %d", i)
		}
	}
}

func TestVault_ConcurrentAccess(t *testing.T) {
	dir := t.TempDir()
	logger := zaptest.NewLogger(t)

	v, err := NewVault(dir, testKey(t), logger)
	if err != nil {
		t.Fatalf("NewVault failed: %v", err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 20)

	// Concurrent writes
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			name := "concurrent" + string(rune('A'+idx))
			if addErr := v.Add(name, "skill-c", []byte("data")); addErr != nil {
				errs <- addErr
			}
		}(i)
	}

	// Concurrent reads (after a brief moment)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = v.List()
		}()
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent error: %v", err)
	}
}

func TestVault_GetNonexistent(t *testing.T) {
	dir := t.TempDir()
	logger := zaptest.NewLogger(t)

	v, err := NewVault(dir, testKey(t), logger)
	if err != nil {
		t.Fatalf("NewVault failed: %v", err)
	}

	_, err = v.Get("ghost")
	if err == nil {
		t.Fatal("expected error for nonexistent secret")
	}
}

// TestVault_IndexPersistence verifies that entries survive vault close/reopen.
func TestVault_IndexPersistence(t *testing.T) {
	dir := t.TempDir()
	logger := zaptest.NewLogger(t)
	key := testKey(t)

	v1, err := NewVault(dir, key, logger)
	if err != nil {
		t.Fatalf("NewVault failed: %v", err)
	}

	v1.Add("persist1", "s1", []byte("data1"))
	v1.Add("persist2", "s2", []byte("data2"))
	v1.Add("persist3", "s1", []byte("data3"))

	// Reopen
	v2, err := NewVault(dir, key, logger)
	if err != nil {
		t.Fatalf("reopen NewVault failed: %v", err)
	}

	entries := v2.List()
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries after reopen, got %d", len(entries))
	}

	s1 := v2.ListForSkill("s1")
	if len(s1) != 2 {
		t.Fatalf("expected 2 for s1, got %d", len(s1))
	}
}
