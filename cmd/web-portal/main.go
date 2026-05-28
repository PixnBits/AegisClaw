package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"AegisClaw/internal/dashboard"

	"github.com/spf13/cobra"
)

func runWebPortal(cmd *cobra.Command, args []string) {
	client, err := newHubBridgeClient()
	useFixtures := false
	if err != nil {
		// Do not hard-fail in test / contract / isolated E2E scenarios.
		// The public REST endpoints we expose (/api/proposals*, /api/status, etc.)
		// and the static UI shell can still be useful for Playwright contract tests
		// and development even when the Hub is not reachable.
		log.Printf("WARNING: Failed to create thin bridge client for Web Portal: %v", err)
		log.Println("Continuing in limited mode (REST endpoints + static UI will still work; live actions will return errors).")
		log.Println("For full functionality start the daemon first (see AGENTS.md).")

		// Try E2E fixture-backed client first (when playwright sets the env vars).
		// This makes isolated E2E tests see realistic data for skills/proposals lists etc.
		if fixtureClient := tryNewE2EFixtureClient(); fixtureClient != nil {
			client = fixtureClient
			useFixtures = true
			log.Println("E2E fixture data loaded — contract tests will see seeded skills/proposals.")
		} else {
			// Provide a no-op client so the rich dashboard server can still start
			// and serve the UI shell + our documented public REST endpoints.
			client = &noopAPIClient{}
		}
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
	if useFixtures {
		log.Println("  (E2E fixture mode — seeded data for contract/UI tests)")
	} else if _, ok := client.(*noopAPIClient); ok {
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

// e2eFixtureClient provides realistic seeded responses for isolated E2E / contract tests.
// Activated automatically in the web-portal binary when playwright sets the fixture env vars.
// This makes the thin layer return data that matches the shapes expected by E2E specs
// and the dashboard templates (skills list, proposals, etc.) without needing a full daemon/Hub.
type e2eFixtureClient struct {
	skills    map[string]map[string]interface{}
	proposals map[string]map[string]interface{}
	created   map[string]map[string]interface{} // ephemeral creates during a test run
}

func tryNewE2EFixtureClient() *e2eFixtureClient {
	dataDir := os.Getenv("AEGIS_STORE_DATA_DIR")
	if dataDir == "" {
		dataDir = "."
	}
	skillsFile := os.Getenv("AEGIS_SKILLS_FILE")
	propsFile := os.Getenv("AEGIS_PROPOSALS_FILE")
	if skillsFile == "" && propsFile == "" {
		return nil
	}

	c := &e2eFixtureClient{
		skills:    map[string]map[string]interface{}{},
		proposals: map[string]map[string]interface{}{},
		created:   map[string]map[string]interface{}{},
	}

	if skillsFile != "" {
		if b, err := os.ReadFile(filepath.Join(dataDir, skillsFile)); err == nil {
			var raw map[string]map[string]interface{}
			if json.Unmarshal(b, &raw) == nil {
				c.skills = raw
			}
		}
	}
	if propsFile != "" {
		if b, err := os.ReadFile(filepath.Join(dataDir, propsFile)); err == nil {
			var raw map[string]map[string]interface{}
			if json.Unmarshal(b, &raw) == nil {
				c.proposals = raw
			}
		}
	}
	if len(c.skills) == 0 && len(c.proposals) == 0 {
		return nil
	}
	return c
}

func (c *e2eFixtureClient) Call(ctx context.Context, action string, payload json.RawMessage) (*dashboard.APIResponse, error) {
	switch action {
	case "proposal.list", "dashboard.skills":
		// Combine fixture proposals + any created in this run
		list := []interface{}{}
		for id, p := range c.proposals {
			entry := map[string]interface{}{"id": id}
			for k, v := range p {
				entry[k] = v
			}
			list = append(list, entry)
		}
		for id, p := range c.created {
			entry := map[string]interface{}{"id": id}
			for k, v := range p {
				entry[k] = v
			}
			list = append(list, entry)
		}
		data, _ := json.Marshal(list)
		if action == "dashboard.skills" {
			// Shape expected by handleSkills + skillsTmpl
			skillsList := []interface{}{}
			for id, s := range c.skills {
				entry := map[string]interface{}{"id": id}
				for k, v := range s {
					entry[k] = v
				}
				skillsList = append(skillsList, entry)
			}
			shape := map[string]interface{}{
				"runtime_skills":     skillsList,
				"built_in_skills":    []interface{}{},
				"built_in_templates": []interface{}{},
				"proposals":          list,
			}
			data, _ = json.Marshal(shape)
		}
		return &dashboard.APIResponse{Success: true, Data: data}, nil

	case "proposal.create":
		var p map[string]interface{}
		json.Unmarshal(payload, &p) // best effort
		if p == nil {
			p = map[string]interface{}{}
		}
		id, _ := p["id"].(string)
		if id == "" {
			id = "prop-" + time.Now().Format("20060102150405") + "-" + randomSuffix()
		}
		p["id"] = id
		c.created[id] = p
		resp := map[string]string{"id": id}
		data, _ := json.Marshal(resp)
		return &dashboard.APIResponse{Success: true, Data: data}, nil

	case "proposal.get":
		var req map[string]string
		json.Unmarshal(payload, &req)
		id := req["id"]
		if prop, ok := c.proposals[id]; ok {
			data, _ := json.Marshal(prop)
			return &dashboard.APIResponse{Success: true, Data: data}, nil
		}
		if prop, ok := c.created[id]; ok {
			data, _ := json.Marshal(prop)
			return &dashboard.APIResponse{Success: true, Data: data}, nil
		}
		// Graceful fallback for unknown
		fb := map[string]interface{}{"id": id, "state": "unknown"}
		data, _ := json.Marshal(fb)
		return &dashboard.APIResponse{Success: true, Data: data}, nil

	case "skill.list":
		list := []interface{}{}
		for id, s := range c.skills {
			entry := map[string]interface{}{"id": id}
			for k, v := range s {
				entry[k] = v
			}
			list = append(list, entry)
		}
		data, _ := json.Marshal(list)
		return &dashboard.APIResponse{Success: true, Data: data}, nil

	case "event.approvals.list":
		// Phase 5 Group 1 improvement: Return a small but realistic set of pending approvals
		// so the Approvals UI renders meaningfully in fixture mode.
		approvals := []interface{}{
			map[string]interface{}{
				"approval_id": "appr-demo-001",
				"title":       "Approve new Discord Monitor skill",
				"risk":        "medium",
				"status":      "pending",
				"requested_by": "user",
				"created_at":  "2026-05-20T10:00:00Z",
			},
		}
		data, _ := json.Marshal(approvals)
		return &dashboard.APIResponse{Success: true, Data: data}, nil

	case "court.get_reviews":
		// Phase 3: No simulation in Court path. When running in pure fixture mode (no daemon),
		// we return a neutral shape that does not fake Court approval or decisions.
		data, _ := json.Marshal(map[string]interface{}{
			"note": "Court data requires real daemon + Court Scribe + personas (see Phase 3)",
		})
		return &dashboard.APIResponse{Success: true, Data: data}, nil

	// Phase 5 Group 1: Wire remaining handlers for fixture mode so UI renders cleanly.
	// Real paths are delegated via the bridge when a daemon is present.
	// Citations: web-portal.md §Key Features & Screens (Git, Workspace, Memory Vault, Approvals)
	// and §API for the Web Portal (public REST contract expectations).

	case "git.branches":
		// Return a minimal but valid shape for the Source / Git History views.
		data, _ := json.Marshal(map[string]interface{}{
			"branches": []string{"main", "proposal-123-feature"},
		})
		return &dashboard.APIResponse{Success: true, Data: data}, nil

	case "git.browse":
		data, _ := json.Marshal(map[string]interface{}{
			"path":    (func() string { var m map[string]string; json.Unmarshal(payload, &m); return m["path"] })(),
			"entries": []interface{}{},
			"note":    "Fixture mode - real git.browse available when daemon running",
		})
		return &dashboard.APIResponse{Success: true, Data: data}, nil

	case "git.commits":
		data, _ := json.Marshal(map[string]interface{}{
			"commits": []interface{}{},
		})
		return &dashboard.APIResponse{Success: true, Data: data}, nil

	case "git.diff":
		data, _ := json.Marshal(map[string]interface{}{
			"diff": "# Fixture diff\n\nReal diff available with live daemon + proposal branch.",
		})
		return &dashboard.APIResponse{Success: true, Data: data}, nil

	case "workspace.list":
		data, _ := json.Marshal([]interface{}{
			map[string]interface{}{"name": "SOUL.md", "type": "file"},
			map[string]interface{}{"name": "AGENTS.md", "type": "file"},
		})
		return &dashboard.APIResponse{Success: true, Data: data}, nil

	case "workspace.read":
		var req map[string]string
		json.Unmarshal(payload, &req)
		filename := req["filename"]
		content := "# " + filename + " (fixture)\n\nReal content available with live daemon."
		data, _ := json.Marshal(map[string]interface{}{"filename": filename, "content": content})
		return &dashboard.APIResponse{Success: true, Data: data}, nil

	case "memory.list":
		data, _ := json.Marshal([]interface{}{})
		return &dashboard.APIResponse{Success: true, Data: data}, nil

	case "memory.search":
		data, _ := json.Marshal([]interface{}{})
		return &dashboard.APIResponse{Success: true, Data: data}, nil

	default:
		// Everything else succeeds with empty object so UI doesn't explode.
		// For Phase 5 we are systematically replacing these with real shapes above.
		data := []byte("{}")
		return &dashboard.APIResponse{Success: true, Data: data}, nil
	}
}

// tiny helper (no rand import needed for test fixture ids)
func randomSuffix() string {
	return time.Now().Format("05000")
}