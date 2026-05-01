# pii_test.go

## Purpose
Unit tests for the `Scrubber` type, validating that every PII detection pattern correctly identifies and redacts sensitive data. Each test case presents an input string containing a known-bad value and asserts the scrubbed output contains the appropriate placeholder token and does not contain the original sensitive value.

## Key Types and Functions
- `TestScrubEmail`: verifies email addresses are replaced with `[EMAIL]`
- `TestScrubSSN`: verifies US Social Security Numbers (e.g. `123-45-6789`) are replaced with `[SSN]`
- `TestScrubPhone`: verifies US phone numbers in various formats are replaced with `[PHONE]`
- `TestScrubJWT`: verifies JWT bearer tokens (three base64url segments separated by `.`) are replaced with `[JWT]`
- `TestScrubAWSKey`: verifies AWS access key IDs (e.g. `AKIAIOSFODNN7EXAMPLE`) are replaced with `[AWS_KEY]`
- `TestScrubIPv4`: verifies IPv4 addresses are replaced with `[IPv4]`
- `TestScrubEntry`: end-to-end test applying `ScrubEntry` to a `MemoryEntry` with PII in both `Key` and `Value` fields

## Role in the System
Guards the PII scrubbing pipeline from regressions. Because scrubbing happens before vault persistence, any missed pattern could permanently store sensitive data. These tests verify coverage of all defined pattern types.

## Dependencies
- `testing`: standard Go test framework
- `internal/memory`: `Scrubber`, `NewScrubber`, `ScrubEntry`, `MemoryEntry`
