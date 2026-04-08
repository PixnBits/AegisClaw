package vault

import (
	"encoding/json"
	"fmt"

	"go.uber.org/zap"
)

// SecretInjection represents a single secret to be injected into a guest VM.
// The plaintext value is only held in memory during the vsock transfer.
type SecretInjection struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// SecretInjectRequest is the payload sent over vsock to the guest agent.
type SecretInjectRequest struct {
	Secrets []SecretInjection `json:"secrets"`
}

// Zero attempts to overwrite all plaintext secret values in-place with null
// bytes as a best-effort defence-in-depth measure.  Note: converting
// r.Secrets[i].Value back to []byte always creates a copy of the backing
// array (Go strings are immutable), so this does NOT guarantee the original
// bytes are cleared — the GC may or may not reclaim the original allocation
// before this function is called.  Nevertheless, calling Zero() after vsock
// transport closes the window between transmission and the next GC cycle,
// which is sufficient for most threat models that are already defended by
// Firecracker isolation and age-encrypted at-rest storage.
func (r *SecretInjectRequest) Zero() {
	for i := range r.Secrets {
		b := []byte(r.Secrets[i].Value)
		for j := range b {
			b[j] = 0
		}
		r.Secrets[i].Value = ""
	}
}

// SecretInjectResponse is the guest agent's acknowledgment.
type SecretInjectResponse struct {
	Injected int    `json:"injected"`
	Error    string `json:"error,omitempty"`
}

// SecretProxy resolves and delivers secrets to skill microVMs.
// It decrypts secrets from the vault and streams them over a private vsock
// channel. Secret plaintext never touches disk on the host — it exists only
// in memory for the duration of the vsock transfer.
type SecretProxy struct {
	vault  *Vault
	logger *zap.Logger
}

// NewSecretProxy creates a proxy that reads secrets from the given vault.
// vault must not be nil.
func NewSecretProxy(v *Vault, logger *zap.Logger) *SecretProxy {
	if v == nil {
		panic("vault: NewSecretProxy called with nil vault")
	}
	return &SecretProxy{
		vault:  v,
		logger: logger,
	}
}

// ResolveSecrets decrypts and packages the named secrets for injection.
// Returns an error if any referenced secret is missing from the vault.
func (sp *SecretProxy) ResolveSecrets(refs []string) (*SecretInjectRequest, error) {
	if len(refs) == 0 {
		return &SecretInjectRequest{Secrets: nil}, nil
	}

	secrets := make([]SecretInjection, 0, len(refs))
	for _, name := range refs {
		plaintext, err := sp.vault.Get(name)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve secret %q: %w", name, err)
		}
		secrets = append(secrets, SecretInjection{
			Name:  name,
			Value: string(plaintext),
		})
		// Best-effort: zero the decrypted byte slice returned by vault.Get.
		// The string copy above still exists in memory until GC; see Zero().
		for j := range plaintext {
			plaintext[j] = 0
		}
		sp.logger.Debug("secret resolved for injection", zap.String("name", name))
	}

	return &SecretInjectRequest{Secrets: secrets}, nil
}

// BuildPayload serializes a SecretInjectRequest into JSON for vsock transport.
func (sp *SecretProxy) BuildPayload(req *SecretInjectRequest) (json.RawMessage, error) {
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal secret inject request: %w", err)
	}
	return data, nil
}
