# proxy.go

## Purpose
Implements `SecretProxy`, the component that resolves secrets from the vault and assembles them into an injection payload for delivery to guest VMs. The proxy fetches the requested secrets, verifies skill ownership, and builds a structured `SecretInjectRequest` payload for transmission over vsock. After the payload has been sent, `Zero()` performs a best-effort wipe of the plaintext bytes in memory to reduce the window in which secrets exist unencrypted on the host.

## Key Types and Functions
- `SecretProxy`: holds a reference to a `*Vault`; panics if constructed with nil vault
- `NewSecretProxy(vault *Vault) *SecretProxy`: constructor with nil guard
- `SecretInjectRequest`: wire type containing a list of `SecretInjection` entries; sent over vsock
- `SecretInjectResponse`: acknowledgement from guest VM
- `SecretInjection`: Name + Payload (raw decrypted bytes)
- `ResolveSecrets(names []string, skillID string) ([]*SecretInjection, error)`: fetches each named secret from the vault, checking skill ownership; returns an error if any secret is missing or owned by a different skill
- `BuildPayload(injections []*SecretInjection) *SecretInjectRequest`: wraps injections into a request struct
- `Zero(injections []*SecretInjection)`: overwrites the `Payload` byte slices with zeros after transmission

## Role in the System
Called by `FirecrackerRuntime` during VM boot to deliver skill secrets over the vsock channel before the guest agent starts executing. The `Zero` call ensures secrets are not retained in host memory longer than necessary.

## Dependencies
- `internal/vault`: `Vault` for secret retrieval
- Standard library: `fmt`, `errors`
