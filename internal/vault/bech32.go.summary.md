# bech32.go

## Purpose
Provides a minimal BIP173 bech32 encoder (encode only — no decode). It is used internally to format the raw Ed25519 scalar bytes of the derived age X25519 private key into a human-readable `age1…` bech32 string, which is the standard wire format for age secret keys. This implementation avoids pulling in a full bech32 library for what is a simple one-way encoding operation.

## Key Types and Functions
- `bech32Encode(hrp string, data []byte) (string, error)`: encodes `data` with the given human-readable part using bech32 encoding; returns the full `hrp1<payload><checksum>` string
- `bech32Polymod(values []byte) uint32`: computes the BIP173 BCH checksum polynomial
- `bech32ConvertBits(data []byte, fromBits, toBits int, pad bool) ([]byte, error)`: re-encodes a byte slice from one bit-width to another (e.g., 8-bit bytes to 5-bit bech32 "words")
- Charset: standard bech32 alphabet `"qpzry9x8gf2tvdw0s3jn54khce6mua7l"`

## Role in the System
Used exclusively within `vault.go`'s `NewVault` function to convert the HKDF-derived age X25519 identity bytes into the `age1…` bech32 secret key format accepted by `filippo.io/age`. It is not exposed as a public API.

## Dependencies
- Standard library only: `fmt`, `strings`
