# pii.go

## Purpose
Implements a PII (Personally Identifiable Information) scrubber that redacts sensitive data from memory entries before they are written to the encrypted vault. This is a defence-in-depth measure: even though the vault is age-encrypted, PII should never be stored in plaintext form inside the ciphertext. The scrubber applies a set of compiled regular-expression rules to both the `Key` and `Value` fields of a `MemoryEntry`.

## Key Types and Functions
- `Scrubber`: struct holding a slice of compiled regexp rules with replacement tokens
- `NewScrubber() *Scrubber`: initialises a scrubber with built-in rules covering:
  - Email addresses → `[EMAIL]`
  - US phone numbers → `[PHONE]`
  - US Social Security Numbers → `[SSN]`
  - IPv4 addresses → `[IPv4]`
  - JWT tokens → `[JWT]`
  - AWS access keys → `[AWS_KEY]`
  - Generic secrets and API keys → `[SECRET]`
- `ScrubEntry(entry *MemoryEntry)`: applies all rules sequentially to both `Key` and `Value` fields in place

## Role in the System
The scrubber is invoked by the memory `Store` before persisting any entry. It forms part of the memory subsystem's security pipeline, ensuring that agent memories containing tool outputs or user messages never leak PII into long-term storage, regardless of whether the vault encryption is ever compromised.

## Dependencies
- `regexp`: standard library compiled regular expressions
- `internal/memory`: uses `MemoryEntry` type defined in `store.go`
