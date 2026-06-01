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
	"fmt"
	"log"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"AegisClaw/internal/bootargs"
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
	priv, pub, err := bootargs.LoadDistributedVMKey("memory")
	if err != nil {
		if bootargs.UseHubVsock() {
			log.Fatal("memory: ", err, " (rebuild memory.img with make build-microvms)")
		}
		log.Printf("memory: %v — generating ephemeral key (dev only)", err)
		pub, priv, err = ed25519.GenerateKey(rand.Reader)
		if err != nil {
			log.Fatal("memory: failed to obtain key (fail-closed):", err)
		}
	}

	// === Transport selection (unix dev / vsock for real guests) ===
	client, err := dialHubTransport(pub, priv)
	if err != nil {
		log.Fatal("memory: failed to connect to AegisHub:", err)
	}
	defer client.Close()

	// === Register ===
	componentID := bootargs.ComponentID("memory")
	regResp, err := client.Register(context.Background(), componentID, pub, getBuildVersion())
	if err != nil {
		log.Fatal("memory: register failed:", err)
	}
	fmt.Println("Memory VM registered, assigned ID:", regResp.AssignedID)

	// === Real Memory VM (Phase 1.3 integration) ===
	memVM := memory.NewVM(7 * 24 * time.Hour) // 7-day TTL
	memVM.SetHubClient(client)
	agentPeer := bootargs.PairedAgentID()
	if agentPeer == "" && strings.HasPrefix(regResp.AssignedID, "memory-") {
		agentPeer = strings.Replace(regResp.AssignedID, "memory-", "agent-", 1)
	}
	memVM.BindAgent(agentPeer) // paired agent (not this VM's own id)

	fmt.Println("memory: real receive-driven loop active, dispatching to VM with ACL enforcement + zeroization")

	// Proper message loop (symmetric to agent)
	for {
		msg, err := client.Receive(context.Background())
		if err != nil {
			log.Println("memory receive error:", err)
			time.Sleep(300 * time.Millisecond)
			continue
		}

		// Per-message logging reduced (will be proper audit via Store later)
		_ = msg.Command

		payload, handleErr := memVM.Handle(context.Background(), msg)
		if handleErr != nil {
			resp := hubclient.Message{
				Source:      client.AssignedID(),
				Destination: msg.Source,
				Command:     "error",
				Payload:     handleErr.Error(),
				Timestamp:   time.Now().UTC().Format(time.RFC3339),
			}
			_ = client.Reply(context.Background(), resp)
			continue
		}

		resp := hubclient.Message{
			Source:      client.AssignedID(),
			Destination: msg.Source,
			Command:     "memory.response",
			Payload:     payload,
			Timestamp:   time.Now().UTC().Format(time.RFC3339),
		}
		_ = client.Reply(context.Background(), resp)
	}
}

func dialHubTransport(pub ed25519.PublicKey, priv ed25519.PrivateKey) (hubclient.Client, error) {
	if bootargs.UseHubVsock() {
		fmt.Printf("memory: waiting for host hub bridge on vsock :%d (Firecracker inverted path)\n", hubclient.GuestHubBridgePort)
		return hubclient.AcceptVsockHubBridge(hubclient.GuestHubBridgePort, priv)
	}
	socket := expandPath(hubSocket)
	if env := os.Getenv("AEGIS_HUB_SOCKET"); env != "" {
		socket = expandPath(env)
	}
	if _, err := os.Stat(socket); err == nil {
		return hubclient.DialUnix(socket, priv)
	}
	return hubclient.DialVsock(hubclient.HostCID, hubclient.HubVsockPort, priv)
}

func main() {
	var rootCmd = &cobra.Command{
		Use:   "memory",
		Short: "Memory VM (real skeleton Phase 1.2)",
		Run:   runMemory,
	}
	rootCmd.Execute()
}
