package main

// Thin Memory VM skeleton (Phase 1.2).
//
// All real logic (32k limit, ACLs, TTL, zeroization, semantic store) lives in
// internal/memory. This file only handles transport (hubclient), key loading,
// registration, and delegation.
//
// SPEC REFERENCES:
//   - docs/specs/memory-vm.md (full spec for context, ACLs, commands)
//   - docs/prd/security-model.md (per-agent isolation + zeroization)
//   - docs/specs/agent-runtime.md (1:1 pairing with Agent Runtime via Hub)

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"AegisClaw/internal/memory"
	"AegisClaw/internal/transport/hubclient"
	"github.com/spf13/cobra"
)

var hubSocket = "~/.aegis/hub.sock"

func init() {
	if env := os.Getenv("AEGIS_HUB_SOCKET"); env != "" {
		hubSocket = env
	}
}

func expandPath(path string) string {
	if path[:2] == "~/" {
		home, _ := os.UserHomeDir()
		return home + path[1:]
	}
	return path
}

func getBuildVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		version := info.Main.Version
		if version == "" || version == "(devel)" {
			for _, setting := range info.Settings {
				if setting.Key == "vcs.revision" && len(setting.Value) >= 7 {
					return setting.Value[:7]
				}
			}
			return "dev"
		}
		return version
	}
	return "unknown"
}

func runMemory(cmd *cobra.Command, args []string) {
	// === Paranoid key loading (same contract as Agent Runtime) ===
	priv, pub, err := loadDistributedOrEphemeralKey()
	if err != nil {
		log.Fatal("memory: failed to obtain key (fail-closed):", err)
	}

	// === Transport selection (unix dev / vsock for real guests) ===
	client, err := dialHubTransport(pub, priv)
	if err != nil {
		log.Fatal("memory: failed to connect to AegisHub:", err)
	}
	defer client.Close()

	// === Register ===
	regResp, err := client.Register(context.Background(), "memory", pub, getBuildVersion())
	if err != nil {
		log.Fatal("memory: register failed:", err)
	}
	fmt.Println("Memory VM registered, assigned ID:", regResp.AssignedID)

	// === Real Memory VM ===
	memVM := memory.NewVM(7 * 24 * time.Hour) // 7-day TTL for long-term in skeleton
	memVM.SetHubClient(client)
	memVM.BindAgent(regResp.AssignedID) // 1:1 pairing

	// Main loop - delegate everything to the real VM
	for {
		// For the transitional skeleton we keep a simple receive loop.
		// In full integration the hubclient will drive this.
		fmt.Println("memory (thin 1.2 skeleton): real VM active with ACLs + 32k limit + zeroization")

		// Placeholder: in real usage the Hub would deliver messages here.
		// For now we just keep the process alive so it can be launched as a guest.
		time.Sleep(5 * time.Second)
	}
}

// loadDistributedOrEphemeralKey and dialHubTransport are identical to the
// Agent Runtime implementation (DRY in future; duplicated for skeleton phase).
func loadDistributedOrEphemeralKey() (ed25519.PrivateKey, ed25519.PublicKey, error) {
	keyPath := os.Getenv("AEGIS_VM_PRIVATE_KEY_PATH")
	if keyPath == "" {
		keyPath = "/run/aegis/vmkey"
	}
	if data, err := os.ReadFile(keyPath); err == nil {
		privBytes, _ := base64.StdEncoding.DecodeString(strings.TrimSpace(string(data)))
		if len(privBytes) == ed25519.PrivateKeySize {
			_ = os.WriteFile(keyPath, []byte("shredded"), 0600)
			_ = os.Remove(keyPath)
			priv := ed25519.PrivateKey(privBytes)
			pub := priv.Public().(ed25519.PublicKey)
			hubclient.ZeroPrivateKey(ed25519.PrivateKey(privBytes))
			return priv, pub, nil
		}
	}
	log.Println("memory: no distributed key — generating ephemeral (dev only)")
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	return priv, pub, nil
}

func dialHubTransport(pub ed25519.PublicKey, priv ed25519.PrivateKey) (hubclient.Client, error) {
	if portStr := os.Getenv("AEGIS_HUB_VSOCK_PORT"); portStr != "" {
		return hubclient.DialVsock(hubclient.HostCID, uint32(hubclient.HubVsockPort), priv)
	}
	socket := expandPath(hubSocket)
	if env := os.Getenv("AEGIS_HUB_SOCKET"); env != "" {
		socket = expandPath(env)
	}
	return hubclient.DialUnix(socket, priv)
}

func main() {
	var rootCmd = &cobra.Command{
		Use:   "memory",
		Short: "Memory VM (real skeleton Phase 1.2)",
		Run:   runMemory,
	}
	rootCmd.Execute()
}
