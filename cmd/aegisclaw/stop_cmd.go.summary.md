# stop_cmd.go — cmd/aegisclaw

## Purpose
Implements the `aegisclaw stop` command. Sends a shutdown request to a running daemon via the daemon's Unix domain socket API.

## Key Types / Functions
- `runStop(cmd, args)` — resolves the socket path from config, calls `api.Client.Shutdown()`, prints confirmation.

## System Fit
Provides a clean shutdown path without killing the process directly; allows the daemon to finish in-flight requests and persist state before exiting.

## Notable Dependencies
- `github.com/PixnBits/AegisClaw/internal/api` — daemon API client
