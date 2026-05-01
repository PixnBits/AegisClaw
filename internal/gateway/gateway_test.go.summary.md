# `gateway_test.go` — Tests for the Message Gateway

## Purpose
Unit-tests the `Gateway` routing core and the `HTTPWebhookChannel` constructor using a `stubChannel` (no real network).

## Key Test Cases

| Test | What It Verifies |
|---|---|
| `TestGateway_RegisterAndChannels` | Registering two channels with one duplicate results in exactly 2 channels |
| `TestGateway_DispatchCallsRouteAndSendsReply` | `dispatch()` calls the `RouteFunc` with the correct message and sends the reply back to the originating channel |
| `TestGateway_DispatchTruncatesOversizedMessage` | Messages exceeding `maxMessageBytes` are truncated before reaching the `RouteFunc` |
| `TestNewHTTPWebhookChannel_InvalidAddr` | Non-host:port string returns error |
| `TestNewHTTPWebhookChannel_EmptyAddr` | Empty addr string returns error |
| `TestHTTPWebhookChannel_ID` | `ID()` returns the configured channel ID |

## Role in the System
Verifies that the gateway's size-capping security control and reply routing work correctly without spinning up real HTTP listeners.

## Notable Dependencies
- Package under test: `gateway`
- Standard library (`context`, `testing`, `time`)
