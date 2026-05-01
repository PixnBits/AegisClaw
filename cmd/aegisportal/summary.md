# Package: cmd/aegisportal

## Overview
`cmd/aegisportal` is the web dashboard microVM binary. It runs inside a Firecracker microVM and serves the AegisClaw web dashboard over vsock. All communication with the host daemon goes through a vsock API bridge on port 1030; the browser-visible dashboard HTTP server listens on vsock port 18080 (reverse-proxied by the host daemon).

## Architecture
```
Browser → Host edge proxy (vsock:18080)
                └─ AegisPortal VM (HTTP dashboard)
                        └─ portalAPIClient
                                └─ vsock:1030 → host daemon api.Server
```

## Files

| File | Description |
|------|-------------|
| `main.go` | Complete portal implementation: vsock HTTP server, API bridge client, filesystem mount |
