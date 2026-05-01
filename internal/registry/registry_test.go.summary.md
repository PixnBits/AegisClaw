# registry_test.go

## Purpose
Tests for the registry `Client` using an in-process `httptest.Server` to avoid network dependencies. Each test sets up a fake registry server that returns known JSON responses and verifies that the client correctly parses and returns the expected data structures. Tests also cover error handling for non-200 responses and malformed JSON.

## Key Types and Functions
- `TestListSkills`: starts a test server returning a JSON array of `SkillEntry` objects; verifies `ListSkills` returns correct count and field values
- `TestFetchSkill`: test server returns a single `SkillEntry`; verifies `FetchSkill` parses all fields including tags and risk level
- `TestFetchSkillSpec`: test server returns a full `SkillSpec`; verifies network policy, tools list, and capabilities are correctly deserialised
- `TestClientErrorHandling`: verifies appropriate errors are returned for HTTP 404 and 500 responses
- `TestResponseSizeCap`: verifies that a response exceeding 512 KiB is rejected or truncated
- `TestTimeout`: verifies the 15-second request timeout is enforced using a delayed test server

## Role in the System
Ensures the registry client reliably parses skill discovery responses from ClawHub. Because the registry is the entry point for all external skill imports, parsing correctness directly affects the governance proposal workflow.

## Dependencies
- `testing`, `net/http/httptest`: in-process HTTP test server
- `encoding/json`: test response construction
- `internal/registry`: package under test
