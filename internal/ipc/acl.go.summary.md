# `acl.go` — IPC Access Control Policy

## Purpose
Defines the `VMRole` type and the compiled-in `ACLPolicy` that controls which VM roles are permitted to send which message types through the `MessageHub`. All traffic not explicitly permitted is denied.

## Key Types & Functions

| Symbol | Description |
|---|---|
| `VMRole` | Typed string: `agent`, `cli`, `court`, `builder`, `skill`, `hub`, `daemon` |
| `ACLPolicy` | Holds an `allowed` map of `(role, msgType)` permit rows |
| `defaultACLPolicy()` | Returns the production policy from `architecture.md §5.1` |
| `ACLPolicy.Check(role, msgType)` | Returns `nil` on permit, error on deny; empty `msgType` in the table acts as wildcard |

## Default Policy Summary

| Role | Permitted Message Types |
|---|---|
| `agent` | `tool.exec`, `chat.message`, `status` |
| `cli` | any (wildcard) |
| `court` | `review.result`, `status` |
| `builder` | `build.result`, `status` |
| `skill` | `tool.result`, `status` |
| `hub` | any (wildcard) |
| `daemon` | `tool.result`, `status` |

## Role in the System
Enforced by `MessageHub.RouteMessage()` before any message is forwarded. Prevents compromised VMs from sending unauthorized message types and ensures skill VMs cannot initiate arbitrary routing calls.

## Notable Dependencies
- Standard library only (`fmt`, `sync`)
