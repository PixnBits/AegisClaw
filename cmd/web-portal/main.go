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
		log.Println("No live Hub/daemon connection — continuing with UI shell + public REST only (full functionality requires `make start` per AGENTS.md).")
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

	// Enforce daemon mediation per web-portal-vm.md §Startup & Runtime Characteristics
	// and host-daemon.md (Web Portal receives traffic ONLY through the Host Daemon's reverse proxy).
	// Direct public binding is only allowed for explicit fixture/E2E test modes.
	isFixture := os.Getenv("AEGIS_E2E_FIXTURE") != "" || os.Getenv("AEGIS_SKILLS_FILE") != "" || os.Getenv("AEGIS_PROPOSALS_FILE") != ""
	if listenAddr == ":8080" && !isFixture {
		log.Printf("web-portal: direct public listen on :8080 is not allowed. Must be started by Host Daemon (AEGIS_WEB_PORTAL_LISTEN_ADDR set to internal addr, traffic via daemon proxy on :8080). See web-portal-vm.md and AGENTS.md.")
		log.Printf("  (For local E2E/contract testing use the fixture env vars or explicit internal addr.)")
		os.Exit(1)
	}

	srv, err := dashboard.New(listenAddr, client)
	if err != nil {
		log.Fatalf("Failed to create rich dashboard server: %v", err)
	}

	log.Printf("Web Portal (thin) starting on %s", listenAddr)
	if useFixtures {
		log.Println("  (E2E fixture mode — seeded data for contract/UI tests)")
	} else if _, ok := client.(*noopAPIClient); ok {
		log.Println("  (no-Hub fallback mode — UI shell + public REST available; full actions require live daemon per AGENTS.md)")
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
// This is the intentional fallback when the web-portal is started without a live
// AegisHub/daemon connection (see AGENTS.md for proper startup via `make start`).
// It allows the static UI shell and documented public REST endpoints to remain
// functional for contract testing and development.
//
// Group 4 note: Full action support requires a live daemon. Any remaining
// "limited mode" surface in production paths for portal actions will be
// addressed as part of closing the Web Portal residuals in
// additional-requirements-and-gaps.md.
//
// Citations: additional-requirements-and-gaps.md (Web Portal residuals);
// web-portal.md §Testability & E2E; docs/no-stubs-left-resolution-plan.md (Phase 5 Group 4).
type noopAPIClient struct{}

func (n *noopAPIClient) Call(ctx context.Context, action string, payload json.RawMessage) (*dashboard.APIResponse, error) {
	return &dashboard.APIResponse{
		Success: false,
		Error:   "web-portal: no live daemon connection (start via `make start` per AGENTS.md). Action not available: " + action,
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

	case "court.get_reviews":
		// Phase 3: No simulation in Court path. When running in pure fixture mode (no daemon),
		// we return a neutral shape that does not fake Court approval or decisions.
		data, _ := json.Marshal(map[string]interface{}{
			"note": "Court data requires real daemon + Court Scribe + personas (see Phase 3)",
		})
		return &dashboard.APIResponse{Success: true, Data: data}, nil

	// Phase 5 Group 1 polish (final): Complete deterministic fixture responses for Git/Workspace/Memory/Approvals
	// surfaces. Shapes are valid for templates + public /api/* REST. Real delegation happens via bridge
	// in live mode (no fallback disclaimers leak into rendered content). Default remains {} for any
	// still-unwired actions (will be expanded in Group 3 E2E + Group 4 full audit).
	// Citations: web-portal.md §Key Features & Screens (Git, Workspace, Memory Vault, Approvals) +
	// §API for the Web Portal + §Testability & E2E; testing-standards.md; additional-requirements-and-gaps.md
	// (zero open stub disclaimers in user-facing paths for these screens).

	case "git.branches":
		data, _ := json.Marshal(map[string]interface{}{
			"branches":        []string{"main", "proposal-123-feature"},
			"current_branch":  "main",
		})
		return &dashboard.APIResponse{Success: true, Data: data}, nil

	case "git.browse":
		// Clean shape; no visible fixture note (template controls messaging).
		data, _ := json.Marshal(map[string]interface{}{
			"path":    (func() string { var m map[string]string; json.Unmarshal(payload, &m); return m["path"] })(),
			"entries": []interface{}{},
		})
		return &dashboard.APIResponse{Success: true, Data: data}, nil

	case "git.commits":
		data, _ := json.Marshal(map[string]interface{}{
			"commits": []interface{}{},
		})
		return &dashboard.APIResponse{Success: true, Data: data}, nil

	case "git.diff":
		data, _ := json.Marshal(map[string]interface{}{
			"diff": "# No changes in fixture\n",
		})
		return &dashboard.APIResponse{Success: true, Data: data}, nil

	case "workspace.list":
		data, _ := json.Marshal([]interface{}{
			map[string]interface{}{"name": "SOUL.md", "type": "file", "size": 1240},
			map[string]interface{}{"name": "AGENTS.md", "type": "file", "size": 892},
		})
		return &dashboard.APIResponse{Success: true, Data: data}, nil

	case "workspace.read":
		var req map[string]string
		json.Unmarshal(payload, &req)
		filename := req["filename"]
		// Clean deterministic content for E2E editor tests (no "fixture" or "real daemon" strings).
		content := "# " + filename + "\n\nThis is a deterministic fixture sample for isolated E2E contract tests.\nSee web-portal.md §Testability & E2E."
		data, _ := json.Marshal(map[string]interface{}{"filename": filename, "content": content})
		return &dashboard.APIResponse{Success: true, Data: data}, nil

	case "memory.list":
		data, _ := json.Marshal([]interface{}{})
		return &dashboard.APIResponse{Success: true, Data: data}, nil

	case "memory.search":
		// Group 3: Return realistic memory entries so Journey 03/05 (collaborative task + monitoring)
		// can assert meaningful vault content in isolated E2E.
		// Citations: web-portal.md §Testability & E2E; user-journeys/03-collaborative-task-execution.md,
		// user-journeys/05-monitoring-agent-activity.md.
		data, _ := json.Marshal([]interface{}{
			map[string]interface{}{"key": "session.last_prompt", "value": "Analyze the new Discord integration", "ttl_tier": "session"},
			map[string]interface{}{"key": "agent.researcher.context", "value": "Current focus: skill proposal for web_search", "ttl_tier": "short"},
		})
		return &dashboard.APIResponse{Success: true, Data: data}, nil

	// Group 3 (E2E enablement): Rich worker + sandbox data for Canvas (J05/J08 monitoring + teams).
	// This lets the live SSE-driven Canvas render meaningful agent cards, roles, teams, and tool feeds
	// in fixture mode without a real daemon. Shapes match what handleCanvas + canvasTmpl + SSE handler expect.
	// Citations: web-portal.md §2 Canvas + Real-time & Streaming; user-journeys/05-monitoring-agent-activity.md,
	// user-journeys/08-multi-agent-team-workflows.md; testing-standards.md (E2E for portal flows).
	case "event.approvals.list":
		// Enhanced for Group 3 J07 (autonomy) + J06 (Court). Provide richer pending approvals
		// with risk_level and description so detail views and rejection flows render deterministically.
		// Citations: web-portal.md §Key Features & Screens (Approvals); user-journeys/07-granting-adjusting-autonomy.md,
		// user-journeys/06-reviewing-court-decisions.md.
		data, _ := json.Marshal([]interface{}{
			map[string]interface{}{
				"approval_id":  "appr-demo-001",
				"title":        "Approve new Discord Monitor skill",
				"risk_level":   "medium",
				"status":       "pending",
				"requested_by": "user",
				"created_at":   "2026-05-20T10:00:00Z",
				"description":  "Registers a Discord monitoring skill with read-only message access. Requires court review for external integration scope.",
			},
			map[string]interface{}{
				"approval_id":  "appr-demo-007",
				"title":        "Grant elevated autonomy to researcher agent",
				"risk_level":   "high",
				"status":       "pending",
				"requested_by": "operator",
				"created_at":   "2026-05-28T09:15:00Z",
				"description":  "High-risk autonomy grant for external tool execution. Must pass Court review per J07.",
			},
		})
		return &dashboard.APIResponse{Success: true, Data: data}, nil

	case "worker.list":
		data, _ := json.Marshal([]interface{}{
			map[string]interface{}{"id": "worker-research", "name": "researcher", "status": "running", "task": "Analyzing proposal", "team_id": "alpha", "role": "researcher", "progress": "65%"},
			map[string]interface{}{"id": "worker-builder", "name": "builder", "status": "idle", "task": "Waiting", "team_id": "alpha", "role": "builder"},
		})
		return &dashboard.APIResponse{Success: true, Data: data}, nil

	case "sandbox.list":
		data, _ := json.Marshal([]interface{}{
			map[string]interface{}{"id": "vm-1", "name": "researcher-vm", "status": "running", "cpu": "23%", "mem": "180MB"},
		})
		return &dashboard.APIResponse{Success: true, Data: data}, nil

	// Tool/thought events for Canvas live log and per-agent tool feeds (J05 monitoring).
	case "chat.tool_events":
		data, _ := json.Marshal([]interface{}{
			map[string]interface{}{"id": 42, "tool": "web.search", "session_id": "worker-research", "status": "success", "duration_ms": 340},
		})
		return &dashboard.APIResponse{Success: true, Data: data}, nil

	case "chat.thought_events":
		data, _ := json.Marshal([]interface{}{
			map[string]interface{}{"id": 99, "description": "Evaluating risk of external call", "session_id": "worker-research"},
		})
		return &dashboard.APIResponse{Success: true, Data: data}, nil

	// Group 2: Minimal realistic fixture for proposal detail (round feedback etc.)
	// so /skills/proposals/* renders fully in isolated E2E without daemon.
	// Shape matches what handleSkillProposal + proposalDetailTmpl expect.
	// Citations: web-portal.md §Key Features & Screens (proposal detail with round feedback);
	// web-portal.md §Testability & E2E; chat-ui-data-flow.md related flows.
	case "dashboard.proposal":
		var req map[string]string
		json.Unmarshal(payload, &req)
		id := req["id"]
		if id == "" { id = "prop-demo-001" }
		proposal := map[string]interface{}{
			"id": id, "title": "Demo skill proposal", "description": "Fixture proposal for E2E contract tests of round feedback.",
			"status": "in_review", "round": 2, "risk": "medium",
		}
		currentFeedback := []interface{}{
			map[string]interface{}{"persona": "ciso", "verdict": "approve", "risk_score": 3, "comments": "Looks safe for network scope.", "timestamp": "2026-05-27T12:00:00Z"},
			map[string]interface{}{"persona": "architect", "verdict": "ask", "risk_score": 5, "comments": "Consider adding rate limiting.", "questions": []string{"How will you bound the external calls?"}, "timestamp": "2026-05-27T12:05:00Z"},
		}
		previousRounds := []interface{}{
			map[string]interface{}{
				"round": 1,
				"reviews": []interface{}{
					map[string]interface{}{"persona": "ciso", "verdict": "approve", "risk_score": 2, "comments": "Initial pass.", "timestamp": "2026-05-26T10:00:00Z"},
				},
			},
		}
		data, _ := json.Marshal(map[string]interface{}{
			"proposal":             proposal,
			"review_status":        map[string]interface{}{"current_round": 2, "current_count": 2, "pending_reviews": 1, "approval_count": 1, "reject_count": 0, "ask_count": 1, "abstain_count": 0},
			"current_round_feedback": currentFeedback,
			"previous_rounds":        previousRounds,
			"revision_history":       []interface{}{},
		})
		return &dashboard.APIResponse{Success: true, Data: data}, nil

	default:
		// Unwired actions return neutral empty for contract stability in fixture/E2E mode.
		// Group 1–3 targeted the Git/Workspace/Memory/Approvals/Canvas/Streaming/Chat surfaces.
		// Any remaining items will be closed during the Group 4 full "no stubs left" audit
		// (see additional-requirements-and-gaps.md §Confirmed Remaining Gaps).
		// Citations: additional-requirements-and-gaps.md (Web Portal + overall gaps); web-portal.md §Testability & E2E.
		data := []byte("{}")
		return &dashboard.APIResponse{Success: true, Data: data}, nil
	}
}

// tiny helper (no rand import needed for test fixture ids)
func randomSuffix() string {
	return time.Now().Format("05000")
}