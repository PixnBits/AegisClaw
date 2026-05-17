package main

// gateway_daemon.go — wires the multi-channel Gateway into the AegisClaw daemon.
//
// This is Phase 2, Task 4 of the OpenClaw integration plan.
//
// The Gateway is an optional, host-side component that receives messages from
// external channels (HTTP webhooks, future: Telegram, Discord, etc.) and routes
// them to the daemon's chat.message API handler.  All routing decisions are
// logged to the kernel audit log.
//
// The Gateway is only started when config.Gateway.Enabled is true.  Each
// entry in config.Gateway.Channels with Enabled:true is registered as a
// channel adapter.  Currently the only built-in adapter is "webhook"
// (HTTPWebhookChannel); unrecognised types are skipped with a warning.
//
// Security invariants maintained here:
//   - Every inbound message is routed through the standard chat.message handler,
//     which enforces the full ReAct loop, capability checks, and audit logging.
//   - The RouteFunc is host-side only; it calls apiSrv.CallDirect (in-process)
//     so no additional socket is opened.
//   - Channel secrets are read from config, never logged.
//   - The Gateway goroutine is stopped when the daemon's context is cancelled.

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/PixnBits/AegisClaw/internal/api"
	"github.com/PixnBits/AegisClaw/internal/gateway"
	"go.uber.org/zap"
)

// startGateway starts the multi-channel Gateway if gateway.enabled is true in
// the daemon config.  It is non-blocking: the Gateway runs in background
// goroutines and is cancelled when ctx is cancelled.
//
// apiSrv must be fully initialised (all handlers registered) before this is
// called, because the route function calls apiSrv.CallDirect.
//
// NEUTRALIZED: Gateway (webhook, Slack, Discord, etc.) integration now belongs
// to a dedicated Gateway component or is mediated by AegisHub.
// Host Daemon only handles core VM lifecycle + privileged socket.
func startGateway(ctx context.Context, env *runtimeEnv, apiSrv *api.Server) {
	// Gateway startup disabled in Host Daemon during aggressive extraction.
	// Multi-channel ingress (Slack, Discord, webhooks) ownership moved out of TCB.
	_ = ctx
	_ = env
	_ = apiSrv
}
		}
		if !resp.Success {
			return "", fmt.Errorf("gateway: chat error: %s", resp.Error)
		}

		var chatResp api.ChatMessageResponse
		if err := json.Unmarshal(resp.Data, &chatResp); err != nil {
			return "", fmt.Errorf("gateway: parse chat response: %w", err)
		}
		return chatResp.Content, nil
	}

	gw := gateway.New(routeFunc)

	// Register configured channel adapters.
	registeredCount := 0
	for _, cc := range env.Config.Gateway.Channels {
		if !cc.Enabled {
			continue
		}
		switch cc.Type {
		case "webhook":
			ch, err := gateway.NewHTTPWebhookChannel(gateway.ChannelConfig{
				ID:      cc.ID,
				Type:    cc.Type,
				Enabled: cc.Enabled,
				Addr:    cc.Addr,
				Secret:  cc.Secret,
				Extra:   cc.Extra,
			})
			if err != nil {
				env.Logger.Warn("gateway: skipping channel with invalid config",
					zap.String("channel_id", cc.ID),
					zap.String("type", cc.Type),
					zap.Error(err),
				)
				continue
			}
			gw.Register(ch)
			registeredCount++
			env.Logger.Info("gateway: registered webhook channel",
				zap.String("channel_id", cc.ID),
				zap.String("addr", cc.Addr),
			)
		default:
			env.Logger.Warn("gateway: unknown channel type; skipping",
				zap.String("channel_id", cc.ID),
				zap.String("type", cc.Type),
			)
		}
	}

	if registeredCount == 0 {
		env.Logger.Info("gateway: enabled but no channels configured; not starting")
		return
	}

	// Drain gateway errors in the background so channel goroutines are never
	// blocked on the error channel.
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case err, ok := <-gw.Errors():
				if !ok {
					return
				}
				env.Logger.Warn("gateway: channel error", zap.Error(err))
			}
		}
	}()

	// Start the gateway in a background goroutine; it blocks until ctx is done.
	go func() {
		env.Logger.Info("gateway: starting",
			zap.Int("channels", registeredCount),
			zap.Strings("channel_ids", gw.Channels()),
		)
		if err := gw.Start(ctx); err != nil && ctx.Err() == nil {
			env.Logger.Error("gateway: unexpected shutdown", zap.Error(err))
		}
		env.Logger.Info("gateway: stopped")
	}()
}
