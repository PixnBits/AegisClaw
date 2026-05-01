# `server_test.go` — External (Black-Box) Dashboard Tests

## Purpose
Tests the `Server` as an HTTP handler using `httptest`, verifying HTTP response codes, content-type headers, and basic response shapes for each route. Uses a stub `APIClient` to control daemon responses.

## Key Test Cases

| Test | What It Verifies |
|---|---|
| `TestServer_HealthEndpoint` | `GET /health` returns 200 with body `ok` |
| `TestServer_Index` | `GET /` returns 200 and renders the overview template |
| `TestServer_Agents` | `GET /agents` returns 200 |
| `TestServer_Async` | `GET /async` returns 200 and lists timers/approvals |
| `TestServer_Memory` | `GET /memory` returns 200 |
| `TestServer_Approvals` | `GET /approvals` returns 200 |
| `TestServer_Audit` | `GET /audit` returns 200 |
| `TestServer_Skills` | `GET /skills` returns 200 |
| `TestServer_Settings` | `GET /settings` returns 200 |
| `TestServer_NotFound` | `GET /does-not-exist` returns 404 |
| `TestServer_SSE` | `GET /events` responds with `Content-Type: text/event-stream` |

## Role in the System
Acts as a regression harness to catch template rendering panics, nil-pointer dereferences in handler code, and broken route registrations.

## Notable Dependencies
- Package under test: `dashboard`
- Standard library (`net/http/httptest`, `encoding/json`, `testing`)
