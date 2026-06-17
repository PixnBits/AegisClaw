package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"AegisClaw/internal/dashboard"
	guestlog "AegisClaw/internal/guest/log"
	"AegisClaw/internal/timing"

	"github.com/spf13/cobra"
)

func runWebPortal(cmd *cobra.Command, args []string) {
	timing.RecordPhase("main_entry")

	// === HOLISTIC DEBUG BUILD FOR WEB-PORTAL GUEST ===
	// Build ID / timestamp printed as early as possible.
	debugBuildID := time.Now().UTC().Format("2006-01-02T15:04:05Z") + " (debug-build)"
	fmt.Printf("!!! WEB-PORTAL GUEST BUILD ID: %s\n", debugBuildID)
	writeToConsole("!!! WEB-PORTAL GUEST BUILD ID: " + debugBuildID + "\n")
	writeToSerial("!!! WEB-PORTAL GUEST BUILD ID: " + debugBuildID + "\n")

	// Open persistent debug log file inside the guest.
	// This survives even if serial console capture is broken.
	debugLogPath := "/tmp/web-portal-guest-debug.log"
	debugLogFile, _ := os.OpenFile(debugLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if debugLogFile != nil {
		fmt.Fprintf(debugLogFile, "=== WEB-PORTAL GUEST DEBUG LOG STARTED %s ===\n", debugBuildID)
		debugLogFile.Sync()
	}

	debug := func(format string, a ...interface{}) {
		msg := fmt.Sprintf(format, a...)
		log.Print(msg)
		writeToConsole(msg + "\n")
		writeToSerial(msg + "\n")
		if debugLogFile != nil {
			fmt.Fprintln(debugLogFile, msg)
			debugLogFile.Sync()
		}
	}

	debug("!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!")
	debug("!!! WEB-PORTAL GUEST BINARY STARTING (runWebPortal)      !!!")
	debug("!!! Hostname: %s", getHostname())
	debug("!!! AEGIS_WEB_PORTAL_LISTEN_ADDR env: %q", os.Getenv("AEGIS_WEB_PORTAL_LISTEN_ADDR"))
	debug("!!! Full /proc/cmdline: %s", getProcCmdline())
	debug("!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!")

	// === AGGRESSIVE EARLY CONSOLE OUTPUT (for debugging vsock / startup issues) ===
	// These go straight to the serial console captured by Firecracker.
	fmt.Fprintf(os.Stdout, "!!! WEB-PORTAL GUEST [EARLY]: entered runWebPortal, build=%s\n", debugBuildID)
	fmt.Fprintf(os.Stdout, "!!! WEB-PORTAL GUEST [EARLY]: AEGIS_WEB_PORTAL_LISTEN_ADDR=%q\n", os.Getenv("AEGIS_WEB_PORTAL_LISTEN_ADDR"))

	// Start Phase 1 structured logger as early as possible
	guestLogger := guestlog.NewDefault()
	guestLogger.Info("web-portal guest binary starting", "build_id", debugBuildID)
	fmt.Fprintf(os.Stdout, "!!! WEB-PORTAL GUEST [EARLY]: guest structured logger initialized\n")

	var client dashboard.APIClient
	useFixtures := false
	if fixtureClient := tryNewE2EFixtureClient(); fixtureClient != nil {
		client = fixtureClient
		useFixtures = true
		log.Println("E2E fixture data loaded — contract tests will see seeded skills/proposals.")
	} else {
		// Connect to Hub/portal-bridge in the background so vsock :18080 and /health
		// are available immediately (guest entropy pool can block crypto/rand for 60s+).
		client = newLazyBridgeClient()
	}

	// Support being managed by the Host Daemon (reverse proxy mode per web-portal-vm.md)
	// When AEGIS_WEB_PORTAL_LISTEN_ADDR (or kernel cmdline aegis.web_portal_listen_addr= for
	// Firecracker guests) is set, listen there instead of the unsafe default.
	// This allows the daemon to start us on an internal address (127.0.0.1:18080) and proxy
	// from :8080. The cmdline fallback is the exact pattern used by court-persona for persona
	// injection (see cmd/court-persona/main.go).
	listenAddr := getWebPortalListenAddr()
	log.Printf("!!! DEBUG: getWebPortalListenAddr() returned: %q", listenAddr)
	if listenAddr == "" {
		listenAddr = ":8080"
		log.Println("!!! DEBUG: No listen addr from env or cmdline — falling back to :8080 (will likely hit guard)")
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

	log.Printf("!!! DEBUG: About to start listeners. TCP target=%s", listenAddr)
	log.Printf("Web Portal (thin) starting on %s", listenAddr)
	if useFixtures {
		log.Println("  (E2E fixture mode — seeded data for contract/UI tests)")
	} else {
		log.Println("  (hub/portal bridge connects in background — /health and vsock :18080 available immediately)")
	}

	// Serve the modern channels-first SPA UI (copied to /static in the guest image via Dockerfile,
	// with dev fallbacks). This is the UI the e2e collaboration tests expect (data-testid channels-panel,
	// nav-channels, channel-detail, channelPostForm etc). The SPA calls the /api/* endpoints provided
	// by the dashboard srv below. We serve static assets + index.html for SPA routes (#channels etc),
	// and delegate everything else (APIs, health, legacy pages) to the dashboard handler.
	staticDir := "/static"
	if _, err := os.Stat(filepath.Join(staticDir, "index.html")); err != nil {
		if _, err2 := os.Stat("cmd/web-portal/static/index.html"); err2 == nil {
			staticDir = "cmd/web-portal/static"
		} else if _, err3 := os.Stat("../../cmd/web-portal/static/index.html"); err3 == nil {
			staticDir = "../../cmd/web-portal/static"
		}
	}
	log.Printf("web-portal: serving channels SPA static from %s", staticDir)
	staticFS := http.FileServer(http.Dir(staticDir))

	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if strings.HasSuffix(p, ".js") || strings.HasSuffix(p, ".css") {
			w.Header().Set("Cache-Control", "no-cache")
		}
		if p == "/" || p == "/index.html" || strings.HasSuffix(p, ".js") || strings.HasSuffix(p, ".css") || strings.HasSuffix(p, ".map") || strings.HasSuffix(p, ".ico") || strings.HasSuffix(p, ".png") || strings.HasSuffix(p, ".svg") {
			if p == "/" || p == "/index.html" {
				http.ServeFile(w, r, filepath.Join(staticDir, "index.html"))
				return
			}
			staticFS.ServeHTTP(w, r)
			return
		}
		// Delegate /api/* , /health, legacy /chat etc to dashboard (SPA consumes the APIs; legacy pages still work)
		srv.ServeHTTP(w, r)
	})

	// Additionally serve the exact same handler over vsock port 18080 when possible.
	// This is the path the Host Daemon reverse proxy uses for Firecracker microVM web-portal
	// instances (no NIC / no direct network per web-portal-vm.md). Safe no-op on Docker
	// Sandbox, host dev runs, or non-Linux (the stub returns nil).
	go func() {
		fmt.Fprintf(os.Stdout, "!!! WEB-PORTAL GUEST [VSOCK]: Attempting vsock listener on port 18080\n")
		log.Println("!!! DEBUG: Attempting vsock listener on port 18080 (for host proxy reachability)")
		guestLogger.Info("attempting vsock listener", "port", 18080)

		if l, err := tryVsockListen(18080); err == nil && l != nil {
			fmt.Fprintf(os.Stdout, "!!! WEB-PORTAL GUEST [VSOCK]: SUCCESS - listening on vsock:18080\n")
			log.Printf("!!! DEBUG: SUCCESS - Web Portal also serving over vsock:18080 for host daemon proxy")
			guestLogger.Info("vsock listener ready", "port", 18080, "status", "success")
			timing.RecordPhase("vsock_18080_ready")
			// Separate server so the main tcp ListenAndServe below can still be fatal.
			s2 := &http.Server{Handler: h}
			if serveErr := s2.Serve(l); serveErr != nil && serveErr != http.ErrServerClosed {
				log.Printf("vsock HTTP server exited: %v", serveErr)
			}
		} else {
			fmt.Fprintf(os.Stdout, "!!! WEB-PORTAL GUEST [VSOCK]: FAILED on port 18080: %v\n", err)
			log.Printf("!!! DEBUG: vsock listen on 18080 FAILED or not available: %v (normal outside real Firecracker)", err)
			guestLogger.Error("vsock listener failed", "port", 18080, "error", fmt.Sprintf("%v", err))
		}
	}()

	// === SECONDARY VSOCK DEBUG CHANNEL (port 18081) ===
	// Host can connect here (e.g. via a future `aegis debug` or raw vsock tool)
	// to retrieve the persistent guest debug log even if serial console is broken.
	go func() {
		if l, err := tryVsockListen(18081); err == nil && l != nil {
			debug("!!! DEBUG: Started secondary debug vsock listener on port 18081")
			for {
				conn, err := l.Accept()
				if err != nil {
					continue
				}
				go func(c net.Conn) {
					defer c.Close()
					if debugLogFile != nil {
						// Rewind and send current log content
						debugLogFile.Seek(0, 0)
						io.Copy(c, debugLogFile)
					} else {
						c.Write([]byte("No debug log file available yet\n"))
					}
				}(conn)
			}
		} else {
			debug("!!! DEBUG: Secondary debug vsock on 18081 not available: %v", err)
		}
	}()

	log.Println("!!! DEBUG: Starting main TCP ListenAndServe on", listenAddr)
	timing.RecordPhase("http_listeners_ready")
	timing.WriteComponentReadySentinel()
	log.Fatal(http.ListenAndServe(listenAddr, h))
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
// AegisHub/daemon connection (see AGENTS.md for proper startup via `sudo ./bin/aegis start`).
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
		Error:   "web-portal: no live daemon connection (start via `sudo ./bin/aegis start` per AGENTS.md). Action not available: " + action,
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
		var req map[string]string
		json.Unmarshal(payload, &req)
		propID := req["proposal_id"]
		if propID == "" {
			propID = "prop-demo-001"
		}
		data, _ := json.Marshal(map[string]interface{}{
			"proposal_id": propID,
			"approved":    false,
			"reviews": []interface{}{
				map[string]interface{}{"persona": "ciso-persona", "verdict": "approve", "comments": "No elevated network risk detected.", "timestamp": "2026-06-17T10:00:00Z"},
				map[string]interface{}{"persona": "architect-persona", "verdict": "approve", "comments": "Design aligns with narrow-scope task pattern.", "timestamp": "2026-06-17T10:01:00Z"},
				map[string]interface{}{"persona": "user-advocate", "verdict": "defer", "comments": "Request clearer rollback plan.", "timestamp": "2026-06-17T10:02:00Z"},
			},
		})
		return &dashboard.APIResponse{Success: true, Data: data}, nil

	case "proposal.approve", "proposal.reject", "proposal.defer":
		data, _ := json.Marshal(map[string]interface{}{"ok": true, "action": action})
		return &dashboard.APIResponse{Success: true, Data: data}, nil

	case "agent.pause", "agent.resume", "agent.cancel":
		data, _ := json.Marshal(map[string]interface{}{"ok": true, "action": action})
		return &dashboard.APIResponse{Success: true, Data: data}, nil

	case "goal.submit", "harness.get":
		data, _ := json.Marshal(map[string]interface{}{
			"plan_id": "plan_main", "channel_id": "main", "goal": "Fixture goal",
			"plan": map[string]interface{}{
				"plan_id": "plan_main", "channel_id": "main", "goal": "Fixture harness goal",
				"stages": []interface{}{
					map[string]interface{}{"name": "Plan", "status": "completed"},
					map[string]interface{}{"name": "Execute", "status": "in_progress"},
				},
			},
			"tasks": []interface{}{
				map[string]interface{}{"task_id": "task_1", "agent_persona": "researcher", "scope": "Fixture narrow task", "status": "active", "current_stage": "Execute", "progress": 40},
			},
		})
		return &dashboard.APIResponse{Success: true, Data: data}, nil

	case "channel.list":
		data, _ := json.Marshal([]interface{}{
			map[string]interface{}{"id": "main", "members": []interface{}{
				map[string]interface{}{"role": "project-manager"},
				map[string]interface{}{"role": "user"},
			}},
		})
		return &dashboard.APIResponse{Success: true, Data: data}, nil

	case "channel.get":
		data, _ := json.Marshal(map[string]interface{}{
			"id": "main", "messages": []interface{}{},
			"members": []interface{}{
				map[string]interface{}{"role": "project-manager"},
				map[string]interface{}{"role": "court-persona-user-advocate"},
				map[string]interface{}{"role": "user"},
			},
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

// getWebPortalListenAddr returns the address the web-portal should listen on.
// Priority: AEGIS_WEB_PORTAL_LISTEN_ADDR env (set by daemon for managed start),
// then kernel cmdline "aegis.web_portal_listen_addr=..." (injected via ExtraBootArgs
// for real Firecracker web-portal VMs), then empty (caller defaults to unsafe :8080
// which the guard below will reject outside fixture mode).
//
// This is the canonical way the Host Daemon tells the presentation VM its *internal*
// (to the sandbox) HTTP address (web-portal-vm.md §Startup & Readiness + Networking).
func getWebPortalListenAddr() string {
	if env := os.Getenv("AEGIS_WEB_PORTAL_LISTEN_ADDR"); env != "" {
		return env
	}
	// Firecracker microVM path (no env from host; kernel cmdline only).
	// Mirrors the parser in cmd/court-persona/main.go exactly.
	if data, err := os.ReadFile("/proc/cmdline"); err == nil {
		for _, kv := range strings.Fields(string(data)) {
			if strings.HasPrefix(kv, "aegis.web_portal_listen_addr=") {
				return strings.TrimPrefix(kv, "aegis.web_portal_listen_addr=")
			}
		}
	}
	return ""
}

// === TEMPORARY DEBUG HELPERS ===

func getHostname() string {
	h, _ := os.Hostname()
	return h
}

func getProcCmdline() string {
	data, err := os.ReadFile("/proc/cmdline")
	if err != nil {
		return fmt.Sprintf("<error reading /proc/cmdline: %v>", err)
	}
	return string(data)
}

// writeToConsole opens the serial console device and writes directly to it.
// This is a debugging aid for minimal Firecracker guests where normal
// stdout/stderr may not reach the captured console.
func writeToConsole(s string) {
	f, err := os.OpenFile("/dev/console", os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		// Fallback: try to write anyway via syscall if open fails
		// (rare, but helps in some early-boot scenarios).
		_, _ = syscall.Write(1, []byte(s))
		return
	}
	defer f.Close()
	_, _ = f.WriteString(s)
}

// forceSerialOpenForDebug explicitly opens the serial port early.
// In some Firecracker setups, this is what actually causes the backend
// console log file to be created on the host.
func forceSerialOpenForDebug() {
	// Try the standard serial port name first
	for _, dev := range []string{"/dev/ttyS0", "/dev/console"} {
		if f, err := os.OpenFile(dev, os.O_WRONLY|os.O_APPEND, 0); err == nil {
			f.WriteString("!!! DEBUG: Serial port opened successfully from web-portal binary\n")
			f.Close()
			return
		}
	}
}

// writeToSerial writes directly to the first available serial port.
// This is more aggressive than writeToConsole for forcing output visibility.
func writeToSerial(s string) {
	for _, dev := range []string{"/dev/ttyS0", "/dev/console"} {
		if f, err := os.OpenFile(dev, os.O_WRONLY|os.O_APPEND, 0); err == nil {
			f.WriteString(s)
			f.Close()
			return
		}
	}
	// Last resort
	_, _ = syscall.Write(1, []byte(s))
}
