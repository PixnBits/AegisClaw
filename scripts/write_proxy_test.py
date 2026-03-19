#!/usr/bin/env python3
"""Writes internal/vault/proxy_test.go — tests for the secret proxy."""
import os

code = r'''package vault

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"testing"

	"go.uber.org/zap/zaptest"
)

func TestSecretProxy_ResolveSecrets(t *testing.T) {
	dir := t.TempDir()
	logger := zaptest.NewLogger(t)
	_, priv, _ := ed25519.GenerateKey(rand.Reader)

	v, err := NewVault(dir, priv, logger)
	if err != nil {
		t.Fatalf("NewVault: %v", err)
	}

	v.Add("apikey", "skill-a", []byte("key123"))
	v.Add("dbpass", "skill-a", []byte("pass456"))

	proxy := NewSecretProxy(v, logger)

	req, err := proxy.ResolveSecrets([]string{"apikey", "dbpass"})
	if err != nil {
		t.Fatalf("ResolveSecrets: %v", err)
	}

	if len(req.Secrets) != 2 {
		t.Fatalf("expected 2 secrets, got %d", len(req.Secrets))
	}

	found := map[string]string{}
	for _, s := range req.Secrets {
		found[s.Name] = s.Value
	}

	if found["apikey"] != "key123" {
		t.Fatalf("expected apikey=key123, got %q", found["apikey"])
	}
	if found["dbpass"] != "pass456" {
		t.Fatalf("expected dbpass=pass456, got %q", found["dbpass"])
	}
}

func TestSecretProxy_ResolveEmpty(t *testing.T) {
	dir := t.TempDir()
	logger := zaptest.NewLogger(t)
	_, priv, _ := ed25519.GenerateKey(rand.Reader)

	v, _ := NewVault(dir, priv, logger)
	proxy := NewSecretProxy(v, logger)

	req, err := proxy.ResolveSecrets(nil)
	if err != nil {
		t.Fatalf("ResolveSecrets nil: %v", err)
	}
	if req.Secrets != nil {
		t.Fatalf("expected nil secrets, got %d", len(req.Secrets))
	}
}

func TestSecretProxy_ResolveMissing(t *testing.T) {
	dir := t.TempDir()
	logger := zaptest.NewLogger(t)
	_, priv, _ := ed25519.GenerateKey(rand.Reader)

	v, _ := NewVault(dir, priv, logger)
	proxy := NewSecretProxy(v, logger)

	_, err := proxy.ResolveSecrets([]string{"nonexistent"})
	if err == nil {
		t.Fatal("expected error for missing secret")
	}
}

func TestSecretProxy_BuildPayload(t *testing.T) {
	dir := t.TempDir()
	logger := zaptest.NewLogger(t)
	_, priv, _ := ed25519.GenerateKey(rand.Reader)

	v, _ := NewVault(dir, priv, logger)
	v.Add("tok", "skill-x", []byte("val"))
	proxy := NewSecretProxy(v, logger)

	req, _ := proxy.ResolveSecrets([]string{"tok"})
	payload, err := proxy.BuildPayload(req)
	if err != nil {
		t.Fatalf("BuildPayload: %v", err)
	}

	var parsed SecretInjectRequest
	if err := json.Unmarshal(payload, &parsed); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if len(parsed.Secrets) != 1 {
		t.Fatalf("expected 1 secret in payload, got %d", len(parsed.Secrets))
	}
	if parsed.Secrets[0].Name != "tok" || parsed.Secrets[0].Value != "val" {
		t.Fatalf("unexpected secret: %+v", parsed.Secrets[0])
	}
}
'''

outpath = os.path.join(os.path.dirname(__file__), '..', 'internal', 'vault', 'proxy_test.go')
outpath = os.path.abspath(outpath)
with open(outpath, 'w') as f:
    f.write(code)
print(f"proxy_test.go: {len(code)} bytes -> {outpath}")
