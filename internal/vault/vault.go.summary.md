# vault.go

## Purpose
Implements age-encrypted secret storage for skill agents. Each secret is stored as an individual `.age`-encrypted file, with a plaintext metadata index (`index.json`) tracking names, skill associations, sizes, and timestamps. The age X25519 identity is derived deterministically from an Ed25519 private key via HKDF-SHA256 with domain separation, ensuring the vault key is always recoverable from the daemon's signing key without storing a separate secret.

## Key Types and Functions
- `Vault`: holds the age identity, storage directory path, and a `sync.RWMutex`
- `SecretEntry`: Name, SkillID, CreatedAt, UpdatedAt, Size (metadata only — no plaintext)
- `NewVault(dir string, ed25519Key ed25519.PrivateKey) (*Vault, error)`: derives age identity via HKDF-SHA256; loads or creates the index
- `Add(name, skillID string, plaintext []byte) error`: validates name, encrypts to `<name>.age`, updates index; `maxSecretBytes = 1 MiB`
- `Get(name, skillID string) ([]byte, error)`: decrypts and returns secret bytes; checks skill ownership
- `Delete(name, skillID string) error`: removes `.age` file; updates index
- `List() ([]SecretEntry, error)`: returns all index entries
- `ListForSkill(skillID string) ([]SecretEntry, error)`: filters by skill ID
- `Has(name string) bool`: checks index membership without decryption
- `GetEntry(name string) (*SecretEntry, error)`: returns metadata without decrypting
- `validateSecretName`: regex `^[a-zA-Z][a-zA-Z0-9_\-]{0,127}$`; also rejects `..`, `/`, `\`

## Role in the System
Provides the secret management layer for skill deployments. When a skill needs an API key or credential, the vault delivers it as an encrypted payload that is decrypted only inside the guest VM via `SecretProxy`.

## Dependencies
- `filippo.io/age`: encryption/decryption
- `golang.org/x/crypto/hkdf`: deterministic key derivation
- `crypto/sha256`, `crypto/ed25519`: key material
- `sync`, `encoding/json`, `io`
