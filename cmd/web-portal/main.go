package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"

	"AegisClaw/internal/dashboard"

	"github.com/spf13/cobra"
)

func runWebPortal(cmd *cobra.Command, args []string) {
	client, err := newHubBridgeClient()
	if err != nil {
		// Do not hard-fail in test / contract / isolated E2E scenarios.
		// The public REST endpoints we expose (/api/proposals*, /api/status, etc.)
		// and the static UI shell can still be useful for Playwright contract tests
		// and development even when the Hub is not reachable.
		log.Printf("WARNING: Failed to create thin bridge client for Web Portal: %v", err)
		log.Println("Continuing in limited mode (REST endpoints + static UI will still work; live actions will return errors).")
		log.Println("For full functionality start the daemon first (see AGENTS.md).")

		// Provide a no-op client so the rich dashboard server can still start
		// and serve the UI shell + our documented public REST endpoints.
		client = &noopAPIClient{}
	}

	// Support being managed by the Host Daemon (reverse proxy mode per web-portal-vm.md)
	// When AEGIS_WEB_PORTAL_LISTEN_ADDR is set, listen there instead of the default.
	// This allows the daemon to start us on an internal address and proxy from :8080.
	listenAddr := os.Getenv("AEGIS_WEB_PORTAL_LISTEN_ADDR")
	if listenAddr == "" {
		listenAddr = ":8080"
	}

	srv, err := dashboard.New(listenAddr, client)
	if err != nil {
		log.Fatalf("Failed to create rich dashboard server: %v", err)
	}

	log.Printf("Web Portal (thin) starting on %s", listenAddr)
	if client == nil {
		log.Println("  (limited / no-Hub mode — good for E2E contract tests of UI + public REST)")
	} else {
		log.Println("  (full mode — all actions routed through Hub/Host Daemon)")
	}
	log.Fatal(http.ListenAndServe(listenAddr, srv))
}

func main() {
	var rootCmd = &cobra.Command{
		Use:   "web-portal",
		Short: "Web Portal (thin presentation layer per web-portal.md)",
		Run:   runWebPortal,
	}
	rootCmd.Execute()
}

// noopAPIClient satisfies dashboard.APIClient when the Hub is unreachable.
// Used for limited / E2E-contract mode so the server + public REST endpoints
// can still start and serve the UI shell.
type noopAPIClient struct{}

func (n *noopAPIClient) Call(ctx context.Context, action string, payload json.RawMessage) (*dashboard.APIResponse, error) {
	return &dashboard.APIResponse{
		Success: false,
		Error:   "web-portal running in limited mode (no Hub connection): " + action + " not available",
	}, nil
}