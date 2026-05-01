# portal_contract_test.go — cmd/aegisclaw

## Purpose
Contract tests asserting the exact JSON shape of `ToolCallEvent` and `ThoughtEvent` payloads consumed by the web dashboard. Any accidental field rename, removal, or type change is caught here before it breaks the dashboard UI.

## Key Tests
- `TestPortalContractToolCallEvent_StartShape` — verifies JSON keys `trace_id`, `tool`, `args`, `started_at` are present on a start event.
- `TestPortalContractToolCallEvent_FinishShape` — verifies `result`, `error`, `elapsed_ms` on a finish event.
- `TestPortalContractThoughtEvent_Shape` — verifies `phase`, `tool`, `summary`, `details`, `recorded_at` on a thought event.

## System Fit
Prevents silent API contract breaks between the daemon and the portal dashboard. No external dependencies required.

## Notable Dependencies
- Standard library only (`encoding/json`, `errors`, `strings`, `testing`, `time`).
