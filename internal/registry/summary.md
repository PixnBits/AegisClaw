# Package: registry

## Overview
The `registry` package provides a read-only HTTP client for the ClawHub skill registry at `https://registry.clawhub.io`. It enables the AegisClaw daemon to discover, inspect, and import skill specifications from the public registry. All operations are read-only; the client never modifies registry state. Any skill discovered through this client must be submitted as a governance proposal and approved by the Governance Court before it can be deployed or executed.

## Files
- `registry.go`: `Client` implementation with `ListSkills`, `FetchSkill`, and `FetchSkillSpec` methods
- `registry_test.go`: Tests using an in-process `httptest.Server` covering successful responses, error codes, and safety limits

## Key Abstractions
- `Client`: stateless HTTP client with a 15-second timeout and a 512 KiB response cap
- `SkillEntry`: lightweight skill metadata (name, description, version, author, risk, tags)
- `SkillSpec`: full skill specification including language, entry point, network policy, tools, secrets refs, and capability requirements
- Response size cap: `io.LimitReader` at 512 KiB prevents memory exhaustion from oversized registry responses

## System Role
The registry client is used by the `aegisclaw import` CLI command and the proposal wizard (`internal/wizard`) to browse and import skills from ClawHub. When a user runs `aegisclaw import <skill-name>`, the client fetches the skill spec, the wizard transforms it into a governance proposal, and the Governance Court reviews it before any code runs.

## Dependencies
- `net/http`: HTTP transport with timeout configuration
- `encoding/json`: JSON response deserialisation
- `io`: `LimitReader` for response size enforcement
- `context`: per-request cancellation and deadline propagation
