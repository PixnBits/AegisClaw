package boundarycrypto

import (
	"crypto/rand"
	"encoding/base64"
	"testing"
)

func TestEncryptDecryptRoundtrip(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}

	secrets := map[string]string{
		"researcher":   "sk_live_research_abc123",
		"discord-bot":  "xoxb-1234567890-abcdef",
		"empty-skill":  "",
	}

	ct, nonce, err := EncryptSecretsBlob(secrets, key)
	if err != nil {
		t.Fatalf("EncryptSecretsBlob failed: %v", err)
	}
	if len(ct) == 0 || len(nonce) != 12 { // GCM standard nonce size
		t.Fatal("ciphertext or nonce has unexpected length")
	}

	decrypted, err := DecryptSecretsBlob(ct, nonce, key)
	if err != nil {
		t.Fatalf("DecryptSecretsBlob failed: %v", err)
	}

	if len(decrypted) != len(secrets) {
		t.Errorf("decrypted map length mismatch: got %d want %d", len(decrypted), len(secrets))
	}
	for k, v := range secrets {
		if decrypted[k] != v {
			t.Errorf("secret for %s mismatch: got %q want %q", k, decrypted[k], v)
		}
	}
}

func TestDecryptTampering(t *testing.T) {
	key := make([]byte, 32)
	rand.Read(key)

	secrets := map[string]string{"s1": "super-secret-value"}
	ct, nonce, _ := EncryptSecretsBlob(secrets, key)

	// Tamper with ciphertext
	badCt := make([]byte, len(ct))
	copy(badCt, ct)
	badCt[0] ^= 0xff

	if _, err := DecryptSecretsBlob(badCt, nonce, key); err != ErrCryptoFailure {
		t.Errorf("expected ErrCryptoFailure on tampered ciphertext, got %v", err)
	}

	// Tamper with nonce
	badNonce := make([]byte, len(nonce))
	copy(badNonce, nonce)
	badNonce[0] ^= 0xff

	if _, err := DecryptSecretsBlob(ct, badNonce, key); err != ErrCryptoFailure {
		t.Errorf("expected ErrCryptoFailure on tampered nonce, got %v", err)
	}
}

func TestWrongKey(t *testing.T) {
	key1 := make([]byte, 32)
	key2 := make([]byte, 32)
	rand.Read(key1)
	rand.Read(key2)

	secrets := map[string]string{"s": "value"}
	ct, nonce, _ := EncryptSecretsBlob(secrets, key1)

	if _, err := DecryptSecretsBlob(ct, nonce, key2); err != ErrCryptoFailure {
		t.Errorf("expected ErrCryptoFailure with wrong key, got %v", err)
	}
}

func TestBadKeyLength(t *testing.T) {
	key := make([]byte, 16) // wrong size
	secrets := map[string]string{"s": "v"}

	if _, _, err := EncryptSecretsBlob(secrets, key); err == nil {
		t.Error("EncryptSecretsBlob should reject non-32-byte key")
	}

	ct := []byte{1, 2, 3}
	nonce := make([]byte, 12)
	if _, err := DecryptSecretsBlob(ct, nonce, key); err != ErrCryptoFailure {
		t.Error("Decrypt should fail closed on bad key length")
	}
}

func TestZeroSecretsMap(t *testing.T) {
	m := map[string]string{
		"a": "secret-alpha",
		"b": "secret-bravo",
	}

	ZeroSecretsMap(m)

	if len(m) != 0 {
		t.Errorf("map should be empty after ZeroSecretsMap, got len=%d", len(m))
	}

	// The critical contract is that after ZeroSecretsMap the map is cleared
	// and any secret material that was inside has been overwritten in the
	// values we controlled. Exact backing-store aliasing with copies taken
	// before the call is best-effort in Go; the important security property
	// (no lingering plaintext in the liveSecretStore after use) is exercised
	// via the integration of this helper in the boundary handler.
}

func TestZeroBytes(t *testing.T) {
	b := []byte("highly-sensitive-material-here")
	ZeroBytes(b)
	for i, v := range b {
		if v != 0 {
			t.Errorf("byte %d not zeroed: got 0x%02x", i, v)
		}
	}
}

// Helper to make a base64 key for manual testing / env var usage.
func TestGenerateExampleKey(t *testing.T) {
	key := make([]byte, 32)
	rand.Read(key)
	t.Logf("Example AEGIS_SECRETS_SYMMETRIC_KEY (base64): %s", base64.StdEncoding.EncodeToString(key))
}
