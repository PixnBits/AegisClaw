package main

import (
	"context"
	"encoding/json"

	"github.com/PixnBits/AegisClaw/internal/api"
	"github.com/PixnBits/AegisClaw/internal/dashboard"
	"go.uber.org/zap"
)

// daemonAPIClient wraps the daemon's api.Server to satisfy dashboard.APIClient.
// The dashboard runs in the same process as the daemon, so we call handlers
// directly without going through the Unix socket.
type daemonAPIClient struct {
	srv *api.Server
}

func (c *daemonAPIClient) Call(ctx context.Context, action string, payload json.RawMessage) (*dashboard.APIResponse, error) {
	resp := c.srv.CallDirect(ctx, action, payload)
	if resp == nil {
		return &dashboard.APIResponse{Error: "unknown action: " + action}, nil
	}
	return &dashboard.APIResponse{
		Success: resp.Success,
		Error:   resp.Error,
		Data:    resp.Data,
	}, nil
}

// startDashboard starts the local web dashboard if enabled in config.
// Runs in a background goroutine; errors are logged but don't crash the daemon.
func startDashboard(ctx context.Context, env *runtimeEnv, apiSrv *api.Server) {
	if env.Config == nil || !env.Config.Dashboard.Enabled {
		return
	}
	addr := env.Config.Dashboard.Addr
	if addr == "" {
		addr = "127.0.0.1:7878"
	}

	client := &daemonAPIClient{srv: apiSrv}
	srv, err := dashboard.New(addr, client)
	if err != nil {
		env.Logger.Error("dashboard init failed", zap.Error(err))
		return
	}
	env.Logger.Info("starting dashboard", zap.String("addr", addr))
	go func() {
		if err := srv.Start(ctx); err != nil && ctx.Err() == nil {
			env.Logger.Error("dashboard stopped", zap.Error(err))
		}
	}()
}
