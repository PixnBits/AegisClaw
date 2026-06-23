package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"AegisClaw/internal/channeldata"
	"AegisClaw/internal/collab"
	"AegisClaw/internal/dashboard/contracts"
	"AegisClaw/internal/dashboard/ratelimit"
	"AegisClaw/internal/dashboard/sanitize"
)

func (s *Server) registerExtendedPortalRoutes() {
	s.mux.HandleFunc("/api/active-work", s.handleAPIActiveWork)
	s.mux.HandleFunc("/api/agents", s.handleAPIAgents)
	s.mux.HandleFunc("/api/agents/", s.handleAPIAgentDetail)
	s.mux.HandleFunc("/api/canvas", s.handleAPICanvas)
	s.mux.HandleFunc("/api/security/posture", s.handleAPISecurityPosture)
}

func (s *Server) handleAPIActiveWork(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET required", http.StatusMethodNotAllowed)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), spaAPITimeout)
	defer cancel()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.collectActiveWork(ctx)) //nolint:errcheck
}

func (s *Server) collectActiveWork(ctx context.Context) map[string]interface{} {
	items := []interface{}{}
	workers, _ := s.fetchRaw(ctx, "worker.list", nil)
	if list, ok := workers.([]interface{}); ok {
		for _, raw := range list {
			m, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			items = append(items, sanitize.JSONMap(sanitize.ContextChat, map[string]interface{}{
				"id":          stringField(m, "id"),
				"persona":     spaStringOr(m["role"], spaStringOr(m["name"], "agent")),
				"scope":       spaStringOr(m["task"], "idle"),
				"stage":       "Execute",
				"progress":    spaStringOr(m["progress"], "—"),
				"status":      spaStringOr(m["status"], "running"),
				"channel_id":  spaStringOr(m["channel_id"], spaStringOr(m["team_id"], "")),
				"last_update": time.Now().UTC().Format(time.RFC3339),
			}))
		}
	}
	proposals, _ := s.fetchRaw(ctx, "proposal.list", nil)
	if list, ok := proposals.([]interface{}); ok {
		for _, raw := range list {
			m, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			state := strings.ToLower(spaStringOr(m["state"], spaStringOr(m["status"], "")))
			if state == "approved" || state == "rejected" {
				continue
			}
			items = append(items, sanitize.JSONMap(sanitize.ContextProposal, map[string]interface{}{
				"id":          stringField(m, "id"),
				"persona":     "court",
				"scope":       spaStringOr(m["title"], "Proposal"),
				"stage":       "Court Review",
				"progress":    spaStringOr(m["votes"], "pending"),
				"status":      "pending",
				"channel_id":  spaStringOr(m["channel_id"], ""),
				"proposal_id": stringField(m, "id"),
				"last_update": time.Now().UTC().Format(time.RFC3339),
			}))
		}
	}
	return map[string]interface{}{"items": items, "count": len(items)}
}

func (s *Server) handleAPIAgents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET required", http.StatusMethodNotAllowed)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), spaAPITimeout)
	defer cancel()
	bundle := s.collectOverviewBundle(ctx)
	workers, _ := bundle["workers"].([]interface{})
	cards := spaWorkersToAgentCards(workers)

	// Merge channel turn-state (last_seen, cycles, outcome, pending) for observability on #agents (spec §8.1).
	// Fetch for "main" (common case); attach by matching normalized role (handles "coder-main" VM id vs "coder" member role).
	if tsRaw, err := s.fetchRaw(ctx, "channel.turn_state", map[string]interface{}{"channel_id": "main"}); err == nil {
		if ts, ok := tsRaw.(map[string]interface{}); ok {
			if mems, ok := ts["members"].([]interface{}); ok {
				turnByRole := map[string]map[string]interface{}{}
				for _, raw := range mems {
					if m, ok := raw.(map[string]interface{}); ok {
						role := collab.NormalizeMemberRole(channeldata.MemberRole(m))
						if role == "" {
							role = collab.NormalizeMemberRole(spaStringOr(m["role"], ""))
						}
						if role != "" {
							turnByRole[role] = m
						}
					}
				}
				for _, c := range cards {
					if cm, ok := c.(map[string]interface{}); ok {
						name, _ := cm["name"].(string)
						roleKey := collab.NormalizeMemberRole(name)
						if t, hit := turnByRole[roleKey]; hit {
							cm["last_seen_seq"] = t["last_seen_seq"]
							cm["cycles_since_turn"] = t["cycles_since_turn"]
							cm["last_outcome"] = t["last_outcome"]
							cm["pending"] = t["pending"]
							cm["last_activity"] = t["last_activity"]
						}
					}
				}
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
		"agents": cards,
	})
}

func (s *Server) handleAPIAgentDetail(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/agents/")
	path = strings.Trim(path, "/")
	parts := strings.Split(path, "/")
	if len(parts) == 0 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	agentID := parts[0]
	ctx, cancel := context.WithTimeout(r.Context(), spaAPITimeout)
	defer cancel()

	if len(parts) >= 2 {
		switch parts[1] {
		case "permissions":
			s.handleAPIAgentPermissions(w, r, ctx, agentID)
			return
		case "trace":
			if r.Method != http.MethodGet {
				http.Error(w, "GET required", http.StatusMethodNotAllowed)
				return
			}
			trace := s.collectAgentTrace(ctx, agentID)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(trace) //nolint:errcheck
			return
		case "pause", "resume", "cancel":
			if r.Method != http.MethodPost {
				http.Error(w, "POST required", http.StatusMethodNotAllowed)
				return
			}
			if !ratelimit.Guard(w, r, ratelimit.CategoryAgentControl) {
				return
			}
			action := "agent." + parts[1]
			if bridgeGuard.NeedsConfirmation(action) {
				if r.Header.Get("X-Aegis-Confirmed") != "1" {
					http.Error(w, "confirmation required", http.StatusPreconditionRequired)
					return
				}
			}
			_, err := s.fetchRaw(ctx, action, map[string]string{"agent_id": agentID})
			if err != nil {
				http.Error(w, sanitize.Text(sanitize.ContextChat, err.Error()), http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "action": parts[1]}) //nolint:errcheck
			return
		}
	}
	http.NotFound(w, r)
}

func (s *Server) collectAgentTrace(ctx context.Context, agentID string) map[string]interface{} {
	sessionID := strings.TrimPrefix(agentID, "agent-")
	if sessionID == agentID {
		sessionID = agentID
	}
	tools, _ := s.fetchRaw(ctx, "chat.tool_events", map[string]interface{}{"limit": 40, "session_id": sessionID})
	thoughts, _ := s.fetchRaw(ctx, "chat.thought_events", map[string]interface{}{"limit": 60, "session_id": sessionID})

	phases := []interface{}{}
	if list, ok := thoughts.([]interface{}); ok {
		for _, raw := range list {
			m, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			phases = append(phases, map[string]interface{}{
				"phase":   "Think",
				"summary": sanitize.Text(sanitize.ContextTrace, spaStringOr(m["description"], "")),
				"ts":      time.Now().UTC().Format(time.RFC3339),
			})
		}
	}
	if list, ok := tools.([]interface{}); ok {
		for _, raw := range list {
			m, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			phases = append(phases, map[string]interface{}{
				"phase":   "Act",
				"tool":    spaStringOr(m["tool"], "tool"),
				"summary": traceToolSummary(m),
				"status":  spaStringOr(m["status"], "success"),
				"ts":      time.Now().UTC().Format(time.RFC3339),
			})
		}
	}
	if len(phases) == 0 {
		phases = []interface{}{
			map[string]interface{}{"phase": "Observe", "summary": "Awaiting agent activity", "ts": time.Now().UTC().Format(time.RFC3339)},
		}
	}
	return map[string]interface{}{
		"agent_id":   agentID,
		"session_id": sessionID,
		"phases":     phases,
	}
}

func (s *Server) handleAPIAgentPermissions(w http.ResponseWriter, r *http.Request, ctx context.Context, agentID string) {
	w.Header().Set("Content-Type", "application/json")
	switch r.Method {
	case http.MethodGet:
		grants, _ := s.fetchRaw(ctx, "permission.list", map[string]interface{}{"subject": agentID})
		requests, _ := s.fetchRaw(ctx, "permission.requests.list", map[string]interface{}{"subject": agentID})
		visibility, _ := s.fetchRaw(ctx, "visibility.list", map[string]interface{}{"subject": agentID})
		snapshot, _ := s.fetchRaw(ctx, "permission.snapshot", map[string]interface{}{"subject": agentID})
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
			"agent_id":   agentID,
			"grants":     grants,
			"requests":   requests,
			"visibility": visibility,
			"snapshot":   snapshot,
		})
	case http.MethodPost:
		if !ratelimit.Guard(w, r, ratelimit.CategoryAgentControl) {
			return
		}
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		action, _ := body["action"].(string)
		capability, _ := body["capability"].(string)
		switch action {
		case "grant":
			_, err := s.fetchRaw(ctx, "permission.grant", map[string]interface{}{
				"subject": agentID, "capability": capability, "reason": body["reason"],
			})
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		case "revoke":
			_, err := s.fetchRaw(ctx, "permission.revoke", map[string]interface{}{
				"subject": agentID, "capability": capability,
			})
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		case "hide":
			_, err := s.fetchRaw(ctx, "visibility.set", map[string]interface{}{
				"subject": agentID, "capability": capability, "level": "hidden", "reason": body["reason"],
			})
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		default:
			http.Error(w, "unknown action", http.StatusBadRequest)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "action": action}) //nolint:errcheck
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func traceToolSummary(m map[string]interface{}) string {
	tool := spaStringOr(m["tool"], "tool")
	parts := []string{tool}
	for _, key := range []string{"input", "output", "args", "result", "path", "error"} {
		if v := spaStringOr(m[key], ""); v != "" {
			parts = append(parts, fmt.Sprintf("%s: %s", key, sanitize.Text(sanitize.ContextTrace, v)))
		}
	}
	status := spaStringOr(m["status"], "ok")
	return sanitize.Text(sanitize.ContextTrace, fmt.Sprintf("%s (%s)", strings.Join(parts, ", "), status))
}

func (s *Server) handleAPICanvas(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET required", http.StatusMethodNotAllowed)
		return
	}
	channelID := strings.TrimSpace(r.URL.Query().Get("channel_id"))
	if channelID == "" {
		channelID = "main"
	}
	ctx, cancel := context.WithTimeout(r.Context(), spaAPITimeout)
	defer cancel()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.collectCanvasState(ctx, channelID)) //nolint:errcheck
}

func (s *Server) collectCanvasState(ctx context.Context, channelID string) map[string]interface{} {
	state := s.collectHarnessState(ctx, channelID)
	body, _ := json.Marshal(state)
	out := map[string]interface{}{"channel_id": channelID}
	_ = json.Unmarshal(body, &out)
	return out
}

func (s *Server) handleAPISecurityPosture(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET required", http.StatusMethodNotAllowed)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), spaAPITimeout)
	defer cancel()
	w.Header().Set("Content-Type", "application/json")
	data, err := s.fetchRaw(ctx, "security.posture", nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(data) //nolint:errcheck
}

func (s *Server) handleProposalAction(w http.ResponseWriter, r *http.Request, proposalID, action string) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	if !ratelimit.Guard(w, r, ratelimit.CategoryProposalAction) {
		return
	}
	bridgeAction := "proposal." + action
	if bridgeGuard.NeedsConfirmation(bridgeAction) && r.Header.Get("X-Aegis-Confirmed") != "1" {
		http.Error(w, "confirmation required", http.StatusPreconditionRequired)
		return
	}
	var body map[string]interface{}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body == nil {
		body = map[string]interface{}{}
	}
	body["proposal_id"] = proposalID
	body["id"] = proposalID

	ctx, cancel := context.WithTimeout(r.Context(), spaAPITimeout)
	defer cancel()
	_, err := s.fetchRaw(ctx, bridgeAction, body)
	if err != nil {
		http.Error(w, sanitize.Text(sanitize.ContextChat, err.Error()), http.StatusBadRequest)
		return
	}
	s.stompPublisher().PublishHarness("plan_court", "", contracts.HarnessStageTransition{
		Type:   contracts.TypeHarnessStageTrans,
		PlanID: "plan_court",
		Stage:  "Court Review",
		Status: action,
	})
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "action": action}) //nolint:errcheck
}

func (s *Server) handleAPIProposalReviews(w http.ResponseWriter, r *http.Request, proposalID string) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET required", http.StatusMethodNotAllowed)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), spaAPITimeout)
	defer cancel()

	propData, _ := s.fetchRaw(ctx, "proposal.get", map[string]string{"id": proposalID})
	courtData, _ := s.fetchRaw(ctx, "court.get_reviews", map[string]string{"proposal_id": proposalID})

	reviews := []interface{}{}
	if m, ok := courtData.(map[string]interface{}); ok {
		if list, ok := m["reviews"].([]interface{}); ok {
			reviews = list
		} else if list, ok := m["current_round_feedback"].([]interface{}); ok {
			reviews = list
		}
	}

	out := map[string]interface{}{
		"proposal_id": proposalID,
		"reviews":     reviews,
	}
	if p, ok := propData.(map[string]interface{}); ok {
		out["proposal"] = sanitize.JSONMap(sanitize.ContextProposal, p)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out) //nolint:errcheck
}