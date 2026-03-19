#!/usr/bin/env python3
"""Writes internal/vault/proxy.go — host-side secret proxy service."""
import os

code = r'''package vault

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
func NewSecretProxy(vault *Vault, logger *zap.Logger) *SecretProxy {
	return &SecretProxy{
		vault:  vault,
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
'''

outpath = os.path.join(os.path.dirname(__file__), '..', 'internal', 'vault', 'proxy.go')
outpath = os.path.abspath(outpath)
with open(outpath, 'w') as f:
    f.write(code)
print(f"proxy.go: {len(code)} bytes -> {outpath}")
