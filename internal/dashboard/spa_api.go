package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"AegisClaw/internal/dashboard/ratelimit"
	"AegisClaw/internal/dashboard/realtime"
	"AegisClaw/internal/dashboard/sanitize"
	"AegisClaw/internal/collab"
	"AegisClaw/internal/portalstomp"
)

const spaAPITimeout = 25 * time.Second

// ChannelNotifyHeader is set by the host daemon when pushing agent posts over
// Firecracker hybrid vsock (RemoteAddr is not loopback inside the guest).
const ChannelNotifyHeader = "X-Aegis-Channel-Notify"

func (s *Server) initSTOMP() {
	if s.stompHub == nil {
		s.stompHub = portalstomp.NewHub()
	}
}

// PublishChannelSTOMP notifies subscribed browsers of a channel activity event.
func (s *Server) PublishChannelSTOMP(chID, from, content string) {
	s.initSTOMP()
	collab.Tracef("web-portal", "stomp.publish", "ch=%s from=%s len=%d", chID, from, len(content))
	realtime.NewPublisher(s.stompHub).PublishChannelActivity(chID, from, content)
}

// stompPublisher returns a sanitized STOMP publisher for the server hub.
func (s *Server) stompPublisher() *realtime.Publisher {
	s.initSTOMP()
	return realtime.NewPublisher(s.stompHub)
}

func (s *Server) handleSTOMP(w http.ResponseWriter, r *http.Request) {
	s.initSTOMP()
	portalstomp.ServeWebSocket(s.stompHub, w, r)
}

func (s *Server) handleInternalChannelActivitySTOMP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	if !requestAuthorizedChannelNotify(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var req struct {
		ChannelID string `json:"channel_id"`
		From      string `json:"from"`
		Content   string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ChannelID == "" {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}
	collab.Tracef("web-portal", "stomp.notify.recv", "ch=%s from=%s", req.ChannelID, req.From)
	s.PublishChannelSTOMP(req.ChannelID, req.From, req.Content)
	go func(chID string) {
		ctx, cancel := context.WithTimeout(context.Background(), spaAPITimeout)
		defer cancel()
		s.publishHarnessDeltas(ctx, chID)
	}(req.ChannelID)
	w.WriteHeader(http.StatusNoContent)
}

func requestFromLoopback(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func requestAuthorizedChannelNotify(r *http.Request) bool {
	if r.Header.Get(ChannelNotifyHeader) == "1" {
		return true
	}
	return requestFromLoopback(r)
}

func (s *Server) handleAPIDashboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET required", http.StatusMethodNotAllowed)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), spaAPITimeout)
	defer cancel()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.collectDashboardSPA(ctx)) //nolint:errcheck
}

func (s *Server) handleAPIMonitoring(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET required", http.StatusMethodNotAllowed)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), spaAPITimeout)
	defer cancel()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.collectMonitoringSPA(ctx)) //nolint:errcheck
}

func (s *Server) handleAPIChannels(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	path := strings.TrimPrefix(r.URL.Path, "/api/channels")
	path = strings.Trim(path, "/")
	parts := []string{}
	if path != "" {
		parts = strings.Split(path, "/")
	}

	ctx, cancel := context.WithTimeout(r.Context(), spaAPITimeout)
	defer cancel()

	switch {
	case len(parts) == 0 && r.Method == http.MethodGet:
		data, err := s.fetchRaw(ctx, "channel.list", nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
			"channels": sanitize.Value(sanitize.ContextChat, data),
		})

	case len(parts) == 0 && r.Method == http.MethodPost:
		var req struct{ ID string `json:"id"` }
		_ = json.NewDecoder(r.Body).Decode(&req)
		_, err := s.fetchRaw(ctx, "channel.create", map[string]interface{}{"id": req.ID})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{"id": req.ID}) //nolint:errcheck

	case len(parts) == 1 && r.Method == http.MethodGet:
		data, err := s.fetchRaw(ctx, "channel.get", map[string]interface{}{"id": parts[0]})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(sanitize.Value(sanitize.ContextChat, data)) //nolint:errcheck

	case len(parts) == 1 && r.Method == http.MethodPost:
		var postReq struct {
			From    string `json:"from"`
			Content string `json:"content"`
		}
		_ = json.NewDecoder(r.Body).Decode(&postReq)
		from := postReq.From
		if from == "" {
			from = "user"
		}
		content := sanitize.Text(sanitize.ContextChat, postReq.Content)
		_, err := s.fetchRaw(ctx, "channel.post", map[string]interface{}{
			"channel_id": parts[0],
			"from":       from,
			"content":    content,
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// Fan-out to channel members is triggered by store → channel.updated on the daemon.
		s.initSTOMP()
		s.PublishChannelSTOMP(parts[0], from, content)
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true}) //nolint:errcheck

	case len(parts) == 2 && parts[1] == "harness" && r.Method == http.MethodGet:
		state := s.collectHarnessState(ctx, parts[0])
		json.NewEncoder(w).Encode(state) //nolint:errcheck

	case len(parts) == 2 && parts[1] == "archive" && r.Method == http.MethodPost:
		if !ratelimit.Guard(w, r, ratelimit.CategoryChannelArchive) {
			return
		}
		if bridgeGuard.NeedsConfirmation("channel.archive") && r.Header.Get("X-Aegis-Confirmed") != "1" {
			http.Error(w, "confirmation required", http.StatusPreconditionRequired)
			return
		}
		_, err := s.fetchRaw(ctx, "channel.archive", map[string]interface{}{"channel_id": parts[0]})
		if err != nil {
			http.Error(w, sanitize.Text(sanitize.ContextChat, err.Error()), http.StatusBadRequest)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true}) //nolint:errcheck

	case len(parts) == 2 && parts[1] == "members" && r.Method == http.MethodPost:
		var m struct{ Role string `json:"role"` }
		_ = json.NewDecoder(r.Body).Decode(&m)
		_, err := s.fetchRaw(ctx, "channel.add_member", map[string]interface{}{"channel_id": parts[0], "role": m.Role})
		if err != nil {
			http.Error(w, sanitize.Text(sanitize.ContextChat, err.Error()), http.StatusBadRequest)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true}) //nolint:errcheck

	case len(parts) == 3 && parts[1] == "members" && parts[2] == "remove" && r.Method == http.MethodPost:
		if !ratelimit.Guard(w, r, ratelimit.CategoryMemberRemove) {
			return
		}
		if bridgeGuard.NeedsConfirmation("channel.remove_member") && r.Header.Get("X-Aegis-Confirmed") != "1" {
			http.Error(w, "confirmation required", http.StatusPreconditionRequired)
			return
		}
		var m struct{ Role string `json:"role"` }
		_ = json.NewDecoder(r.Body).Decode(&m)
		_, err := s.fetchRaw(ctx, "channel.remove_member", map[string]interface{}{"channel_id": parts[0], "role": m.Role})
		if err != nil {
			http.Error(w, sanitize.Text(sanitize.ContextChat, err.Error()), http.StatusBadRequest)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true}) //nolint:errcheck

	default:
		http.NotFound(w, r)
	}
}

func (s *Server) collectDashboardSPA(ctx context.Context) map[string]interface{} {
	bundle := s.collectOverviewBundle(ctx)
	workers, _ := bundle["workers"].([]interface{})
	agents := spaWorkersToAgentCards(workers)
	skillCount := 0
	if skills, err := s.fetchRaw(ctx, "skill.list", nil); err == nil {
		skillCount = spaCountItems(skills)
	}
	activeWork := s.collectActiveWork(ctx)
	return map[string]interface{}{
		"notifications": 0,
		"safe_mode":     false,
		"channel_count": bundle["channel_count"],
		"quick_stats": map[string]interface{}{
			"active_agents":     len(agents),
			"background_tasks":  bundle["worker_count"],
			"skills_installed":  skillCount,
			"pending_proposals": bundle["approval_count"],
			"channel_count":     bundle["channel_count"],
		},
		"agents":          agents,
		"active_work":     activeWork["items"],
		"recent_activity": []interface{}{},
	}
}

func (s *Server) collectMonitoringSPA(ctx context.Context) map[string]interface{} {
	bundle := s.collectOverviewBundle(ctx)
	workers, _ := bundle["workers"].([]interface{})
	return map[string]interface{}{
		"stats": map[string]interface{}{
			"running_vms":      bundle["running_vm_count"],
			"background_tasks": bundle["worker_count"],
			"cpu_usage":        bundle["host_load_label"],
			"memory_usage":     bundle["host_ram_label"],
		},
		"agents": spaWorkersToAgentCards(workers),
		"logs":   []interface{}{},
	}
}

func (s *Server) collectOverviewBundle(ctx context.Context) map[string]interface{} {
	workers, _ := s.fetchRaw(ctx, "worker.list", nil)
	sandboxes, _ := s.fetchRaw(ctx, "sandbox.list", nil)
	stats, _ := s.fetchRaw(ctx, "system.stats", nil)

	workerList, _ := workers.([]interface{})
	sandboxList, _ := sandboxes.([]interface{})
	statsMap, _ := stats.(map[string]interface{})

	runningVMCount := 0
	for _, raw := range sandboxList {
		m, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		status := strings.ToLower(fmt.Sprintf("%v", m["status"]))
		if status == "running" || status == "" {
			runningVMCount++
		}
	}

	channelCount := 0
	if chs, err := s.fetchRaw(ctx, "channel.list", nil); err == nil {
		channelCount = spaCountItems(chs)
	}

	hostLoad := "0.00"
	hostRAM := "—"
	if statsMap != nil {
		if v, ok := statsMap["host_load_label"].(string); ok && v != "" {
			hostLoad = v
		}
		if v, ok := statsMap["host_ram_label"].(string); ok && v != "" {
			hostRAM = v
		}
	}

	return map[string]interface{}{
		"worker_count":     len(workerList),
		"approval_count":   0,
		"channel_count":    channelCount,
		"running_vm_count": runningVMCount,
		"host_load_label":  hostLoad,
		"host_ram_label":   hostRAM,
		"workers":          workerList,
	}
}

func spaWorkersToAgentCards(workers []interface{}) []interface{} {
	out := make([]interface{}, 0, len(workers))
	for _, raw := range workers {
		m, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := m["id"].(string)
		if name == "" {
			name, _ = m["name"].(string)
		}
		status, _ := m["status"].(string)
		if status == "" {
			status = "running"
		}
		task, _ := m["task"].(string)
		if task == "" {
			task = "idle"
		}
		progress, _ := m["progress"].(string)
		if progress == "" {
			progress = "—"
		}
		ch, _ := m["channel"].(string)
		card := map[string]interface{}{
			"name":     name,
			"status":   status,
			"task":     task,
			"progress": progress,
		}
		if ch != "" {
			card["channel"] = ch
		}
		out = append(out, card)
	}
	return out
}

func spaNormalizeSkills(data interface{}) []interface{} {
	list, ok := data.([]interface{})
	if !ok {
		return []interface{}{}
	}
	out := make([]interface{}, 0, len(list))
	for _, raw := range list {
		m, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		id, _ := m["id"].(string)
		name, _ := m["name"].(string)
		if name == "" {
			name = id
		}
		out = append(out, sanitize.JSONMap(sanitize.ContextChat, map[string]interface{}{
			"id":              id,
			"name":            name,
			"version":         spaStringOr(m["version"], "0.0.0"),
			"status":          spaStringOr(m["status"], "registered"),
			"description":     spaStringOr(m["description"], ""),
			"required_scopes": spaStringSliceOr(m["required_scopes"]),
			"secrets":         spaStringSliceOr(m["secrets"]),
		}))
	}
	return out
}

func spaNormalizeProposals(data interface{}) []interface{} {
	list, ok := data.([]interface{})
	if !ok {
		return []interface{}{}
	}
	out := make([]interface{}, 0, len(list))
	for _, raw := range list {
		m, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		id, _ := m["id"].(string)
		title, _ := m["title"].(string)
		if title == "" {
			title = id
		}
		out = append(out, sanitize.JSONMap(sanitize.ContextProposal, map[string]interface{}{
			"id":             id,
			"title":          title,
			"status":         spaStringOr(m["state"], spaStringOr(m["status"], "pending")),
			"summary":        spaStringOr(m["description"], spaStringOr(m["summary"], "")),
			"votes":          spaStringOr(m["votes"], "0/0"),
			"security_gates": []interface{}{},
		}))
	}
	return out
}

func spaStringOr(v interface{}, fallback string) string {
	if s, ok := v.(string); ok && s != "" {
		return s
	}
	return fallback
}

func spaStringSliceOr(v interface{}) []string {
	switch t := v.(type) {
	case []string:
		return t
	case []interface{}:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return []string{}
	}
}

func spaCountItems(v interface{}) int {
	switch t := v.(type) {
	case []interface{}:
		return len(t)
	case map[string]interface{}:
		return len(t)
	default:
		return 0
	}
}
