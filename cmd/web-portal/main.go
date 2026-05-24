package main

import (
	"log"
	"net/http"

	"AegisClaw/internal/dashboard"

	"github.com/spf13/cobra"
)

func runWebPortal(cmd *cobra.Command, args []string) {
	client, err := newHubBridgeClient()
	if err != nil {
		log.Fatalf("Failed to create thin bridge client for Web Portal: %v", err)
	}

	// The rich Server from internal/dashboard already implements the full
	// feature set described in web-portal.md (Canvas, streaming chat + thoughts/tools,
	// proposals, Court, workspace, PRs, source, approvals, etc.).
	//
	// By wiring it to our hubBridgeClient (which speaks the standard signed
	// Message protocol to the Hub), we make the entire web-portal binary
	// strictly thin / presentation-only — exactly as required by the spec
	// and web-portal-vm.md.
	srv, err := dashboard.New(":8080", client)
	if err != nil {
		log.Fatalf("Failed to create rich dashboard server: %v", err)
	}

	log.Println("Web Portal (thin) starting on :8080 — all logic routed through Hub/Host Daemon")
	log.Fatal(http.ListenAndServe(":8080", srv))
}

func main() {
	var rootCmd = &cobra.Command{
		Use:   "web-portal",
		Short: "Web Portal (thin presentation layer per web-portal.md)",
		Run:   runWebPortal,
	}
	rootCmd.Execute()
}