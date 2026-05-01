# `router_test.go` — Tests for the IPC Router

## Purpose
Unit-tests the `Router` in isolation: registration, deregistration, normal routing, and all error/security paths.

## Key Test Cases

| Test | What It Verifies |
|---|---|
| `TestRouter_Register` | Successful registration; duplicate ID returns error |
| `TestRouter_Unregister` | Handler removed; routing after unregister returns "no route" result |
| `TestRouter_Route_Success` | Valid message delivered to handler; handler response propagated |
| `TestRouter_Route_InvalidMessage` | Missing `ID`/`From`/`To`/`Type` fields return validation error |
| `TestRouter_Route_SenderMismatch` | `msg.From != senderVMID` returns identity-mismatch error |
| `TestRouter_Route_SelfRouting` | `from == to` returns self-routing error |
| `TestRouter_Route_NoRoute` | Message to unregistered ID returns `DeliveryResult{Success: false}` |
| `TestRouter_RegisteredRoutes` | Returns all registered IDs |
| `TestRouter_HasRoute` | Returns true/false correctly |

## Role in the System
Isolated unit coverage of the anti-spoofing and self-routing security controls in the routing layer, independent of the hub or kernel.

## Notable Dependencies
- Package under test: `ipc`
- Standard library only
