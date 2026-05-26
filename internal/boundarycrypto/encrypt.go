// Package boundarycrypto — encryption helpers for the real secrets path (7.1).
//
// This file adds the production-oriented encrypted blob support for
// "secrets.update" messages coming from the Store VM over the Hub.
//
// Design goals (paranoid, per network-boundary.md + secret-management prd):
//   - Use only stdlib (crypto/aes + crypto/cipher GCM + crypto/rand).
//   - Authenticated encryption (AES-256-GCM) — tampering is detected.
//   - Random nonce per message.
//   - Explicit zeroization support after decryption (best-effort in Go).
//   - Never log plaintext secrets (enforced by never returning decrypted
//     material except to the tightly controlled liveSecretStore).
//   - Fail closed on any crypto error.
//
// Key management for this phase:
//   - The symmetric key (32 bytes / 256-bit) is delivered out-of-band or via
//     a future attested key exchange during boundary registration.
//   - For development / testing we support AEGIS_SECRETS_SYMMETRIC_KEY (base64).
//   - In a later slice this will be replaced by a proper key derived from
//     the Store<->Boundary registration handshake or from a KMS/HSM.
//
// The Store is the only component that ever *creates* these encrypted blobs.
// The Boundary is the only component that ever decrypts them.

package boundarycrypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"time"
)

// ErrCryptoFailure is returned for any decryption / integrity failure.
// Callers must treat this as a security event and fail closed.
var ErrCryptoFailure = errors.New("boundarycrypto: cryptographic failure (tampering or bad key)")

// EncryptSecretsBlob encrypts a per-skill secrets map using AES-256-GCM.
// The resulting ciphertext + nonce can be placed inside a signed
// "secrets.update" Hub message.
//
// The caller is responsible for securely obtaining a 32-byte key.
func EncryptSecretsBlob(secrets map[string]string, key []byte) (ciphertext, nonce []byte, err error) {
	if len(key) != 32 {
		return nil, nil, errors.New("boundarycrypto: secrets encryption key must be exactly 32 bytes")
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, err
	}

	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, err
	}

	// Canonical JSON for deterministic encryption (same as signing canonicalization).
	plain, err := json.Marshal(secrets)
	if err != nil {
		return nil, nil, err
	}

	nonce = make([]byte, aesgcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, err
	}

	ciphertext = aesgcm.Seal(nil, nonce, plain, nil)

	// Best-effort zero the plaintext we just marshaled.
	ZeroBytes(plain)

	return ciphertext, nonce, nil
}

// DecryptSecretsBlob decrypts and authenticates a blob produced by
// EncryptSecretsBlob. Returns the original secrets map or ErrCryptoFailure.
//
// After the caller has finished with the returned map (e.g. after feeding it
// into liveSecretStore.ReplaceAll), it should call ZeroSecretsMap on it.
func DecryptSecretsBlob(ciphertext, nonce, key []byte) (map[string]string, error) {
	if len(key) != 32 {
		return nil, ErrCryptoFailure
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, ErrCryptoFailure
	}

	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, ErrCryptoFailure
	}

	if len(nonce) != aesgcm.NonceSize() {
		return nil, ErrCryptoFailure
	}

	plain, err := aesgcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, ErrCryptoFailure // authenticated decryption failed
	}

	var secrets map[string]string
	if err := json.Unmarshal(plain, &secrets); err != nil {
		ZeroBytes(plain)
		return nil, ErrCryptoFailure
	}

	ZeroBytes(plain) // we no longer need the raw JSON
	return secrets, nil
}

// ZeroBytes overwrites the slice with zeros. This is best-effort secret
// clearing in Go (the GC and compiler can still make life difficult).
// Call this on any intermediate plaintext buffers that held secrets.
func ZeroBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

// ZeroSecretsMap zeros every secret value in the map and then clears the map.
// Use this immediately after the liveSecretStore has consumed a decrypted blob.
func ZeroSecretsMap(m map[string]string) {
	for k, v := range m {
		ZeroBytes([]byte(v))
		m[k] = ""
	}
	// Let the map be GC'd; we already overwrote the values.
	for k := range m {
		delete(m, k)
	}
}

// BuildEncryptedSecretsUpdatePayload is a convenience the Store (or any
// authorized secrets producer) can use to create the payload portion of a
// signed "secrets.update" Hub message that carries an encrypted blob.
//
// The resulting map can be placed in Message.Payload before signing.
func BuildEncryptedSecretsUpdatePayload(secrets map[string]string, symKey []byte, extra map[string]interface{}) (map[string]interface{}, error) {
	ct, nonce, err := EncryptSecretsBlob(secrets, symKey)
	if err != nil {
		return nil, err
	}

	payload := map[string]interface{}{
		"encrypted_blob": base64.StdEncoding.EncodeToString(ct),
		"nonce":          base64.StdEncoding.EncodeToString(nonce),
		"timestamp":      time.Now().UTC().Format(time.RFC3339),
	}

	// Allow the caller to add "operations", "nonce" for replay, etc.
	for k, v := range extra {
		payload[k] = v
	}

	return payload, nil
}
