# gateway_daemon.go — cmd/aegisclaw

## Purpose
Starts the optional Gateway daemon when `config.Gateway.Enabled` is true. The Gateway receives external HTTP webhook messages and forwards them to the agent's `chat.message` handler.

## Key Types / Functions
- `startGateway(ctx, env)` — creates an HTTP listener on the configured gateway address; validates webhook signatures; calls `api.Server.CallDirect("chat.message", ...)` to inject the message.
- Rate limiting and source-IP allow-listing are applied per gateway rule.

## System Fit
Extensibility point: allows external systems (GitHub, Slack, custom webhooks) to interact with the agent without direct socket access. All injected messages are audit-logged.

## Notable Dependencies
- `github.com/PixnBits/AegisClaw/internal/api` — `CallDirect` for in-process routing
- `net/http` — webhook listener
