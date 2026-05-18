package main

import (
	"github.com/PixnBits/AegisClaw/internal/aegishub"
)

// In initRuntime, prefer real client:
// aegisHubClient, err := aegishub.NewClient("")
// if err != nil { use stub }
// env.AegisHubClient = aegisHubClient

// For now we keep the stub but the real client is available.
