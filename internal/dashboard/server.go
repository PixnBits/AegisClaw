// Package dashboard implements the local web dashboard for AegisClaw Phase 4.
//
// Architecture: pure Go + html/template, no external frameworks.
// SSE endpoint (/events) pushes real-time updates for live views.
// All state is fetched from the daemon via the Unix socket API.
package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"time"
)

// Server is the dashboard HTTP server.
type Server struct {
	addr      string
	apiClient APIClient
	funcMap   template.FuncMap
	mux       *http.ServeMux
}

// APIClient abstracts daemon API calls for the dashboard.
type APIClient interface {
	Call(ctx context.Context, action string, payload json.RawMessage) (*APIResponse, error)
}

// APIResponse mirrors api.Response.
type APIResponse struct {
	Success bool            `json:"success"`
	Error   string          `json:"error,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// New creates the dashboard server.
func New(addr string, client APIClient) (*Server, error) {
	s := &Server{
		addr:      addr,
		apiClient: client,
		mux:       http.NewServeMux(),
	}
	s.funcMap = template.FuncMap{
		"fmtTime": func(t time.Time) string {
			if t.IsZero() {
				return "-"
			}
			return t.Format("2006-01-02 15:04:05")
		},
		"truncate": func(s string, n int) string {
			if len(s) <= n {
				return s
			}
			return s[:n] + "…"
		},
		"join": strings.Join,
		"toJSON": func(v interface{}) template.JS {
			b, err := json.Marshal(v)
			if err != nil {
				return template.JS("null")
			}
			return template.JS(b)
		},
		"substr": func(s string, start int) string {
			if start >= len(s) {
				return ""
			}
			return s[start:]
		},
		// len counts items in slices or maps returned by fetchRaw (interface{}).
		// Returns 0 for nil or unrecognised types rather than panicking.
		"len": func(v interface{}) int {
			if v == nil {
				return 0
			}
			switch val := v.(type) {
			case []interface{}:
				return len(val)
			case map[string]interface{}:
				return len(val)
			case []map[string]interface{}:
				return len(val)
			case string:
				return len(val)
			default:
				return 0
			}
		},
	}
	s.registerRoutes()
	return s, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// Start starts the dashboard HTTP server (blocks until ctx is done).
func (s *Server) Start(ctx context.Context) error {
	srv := &http.Server{
		Addr:    s.addr,
		Handler: s,
	}
	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutCtx) //nolint:errcheck
	}()
	return srv.ListenAndServe()
}

func (s *Server) registerRoutes() {
	s.mux.HandleFunc("/", s.handleIndex)
	s.mux.HandleFunc("/agents", s.handleAgents)
	s.mux.HandleFunc("/async", s.handleAsync)
	s.mux.HandleFunc("/memory", s.handleMemory)
	s.mux.HandleFunc("/approvals", s.handleApprovals)
	s.mux.HandleFunc("/approvals/decide", s.handleApprovalsDecide)
	s.mux.HandleFunc("/audit", s.handleAudit)
	s.mux.HandleFunc("/skills", s.handleSkills)
	s.mux.HandleFunc("/skills/proposals/", s.handleSkillProposal)
	s.mux.HandleFunc("/settings", s.handleSettings)
	s.mux.HandleFunc("/chat", s.handleChat)
	s.mux.HandleFunc("/chat/send", s.handleChatSend)
	s.mux.HandleFunc("/canvas", s.handleCanvas)
	s.mux.HandleFunc("/events", s.handleSSE)
	// Phase 2: Source Code & Git routes
	s.mux.HandleFunc("/source", s.handleSource)
	s.mux.HandleFunc("/source/browse", s.handleSourceBrowse)
	s.mux.HandleFunc("/workspace", s.handleWorkspace)
	s.mux.HandleFunc("/workspace/edit", s.handleWorkspaceEdit)
	// Phase 3: Git History routes
	s.mux.HandleFunc("/git", s.handleGitHistory)
	s.mux.HandleFunc("/git/diff", s.handleGitDiff)
	// Phase 4: Pull Request routes
	s.mux.HandleFunc("/pullrequests", s.handlePRList)
	s.mux.HandleFunc("/pullrequests/detail", s.handlePRDetail)
	s.mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	})
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	// Fetch quick-stats from the daemon.
	workers, _ := s.fetchRaw(r.Context(), "worker.list", map[string]bool{"active_only": true})
	approvals, _ := s.fetchRaw(r.Context(), "event.approvals.list", map[string]bool{"pending_only": true})
	timers, _ := s.fetchRaw(r.Context(), "event.timers.list", nil)
	sandboxes, _ := s.fetchRaw(r.Context(), "sandbox.list", map[string]bool{"running_only": true})
	memories, _ := s.fetchRaw(r.Context(), "memory.list", map[string]interface{}{"limit": 1, "count_only": true})
	sysStats, _ := s.fetchRaw(r.Context(), "system.stats", nil)

	workerCount := countItems(workers)
	approvalCount := countItems(approvals)
	timerCount := countItems(timers)
	runningVMCount := countItems(sandboxes)
	runningVMVCPUs, runningVMMemoryMB, runningVMRSSMB := sandboxResourceTotals(sandboxes)

	var memCount int
	if m, ok := memories.(map[string]interface{}); ok {
		if c, ok := m["total"].(float64); ok {
			memCount = int(c)
		}
	}

	var hostRAMTotalMB, hostRAMUsedMB int64
	var hostRAMPct int
	var hostLoadAvg1 float64
	if m, ok := sysStats.(map[string]interface{}); ok {
		hostRAMTotalMB = int64(toFloat(m["host_ram_total_mb"]))
		hostRAMUsedMB = int64(toFloat(m["host_ram_used_mb"]))
		hostRAMPct = int(toFloat(m["host_ram_pct"]))
		hostLoadAvg1 = toFloat(m["host_load_avg_1"])
	}
	hostRAMLabel := fmt.Sprintf("%s / %s", fmtDashMB(hostRAMUsedMB), fmtDashMB(hostRAMTotalMB))
	hostLoadLabel := fmt.Sprintf("%.2f", hostLoadAvg1)

	s.renderTemplate(w, "Overview", overviewTmpl, map[string]interface{}{
		"WorkerCount":       workerCount,
		"ApprovalCount":     approvalCount,
		"TimerCount":        timerCount,
		"MemoryCount":       memCount,
		"RunningVMCount":    runningVMCount,
		"RunningVMVCPUs":    runningVMVCPUs,
		"RunningVMMemoryMB": runningVMMemoryMB,
		"RunningVMRSSMB":    runningVMRSSMB,
		"RunningVMs":        sandboxes,
		"Workers":           workers,
		"Approvals":         approvals,
		"HostRAMLabel":      hostRAMLabel,
		"HostRAMPct":        hostRAMPct,
		"HostLoadLabel":     hostLoadLabel,
	})
}

func (s *Server) handleAgents(w http.ResponseWriter, r *http.Request) {
	workers, _ := s.fetchRaw(r.Context(), "worker.list", map[string]bool{"active_only": false})
	s.renderTemplate(w, "Agents", agentsTmpl, map[string]interface{}{
		"Workers": workers,
	})
}

func (s *Server) handleAsync(w http.ResponseWriter, r *http.Request) {
	timers, _ := s.fetchRaw(r.Context(), "event.timers.list", nil)
	signals, _ := s.fetchRaw(r.Context(), "event.signals.list", map[string]interface{}{"limit": 20})
	s.renderTemplate(w, "Async Hub", asyncTmpl, map[string]interface{}{
		"Timers":  timers,
		"Signals": signals,
	})
}

func (s *Server) handleMemory(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	var memories interface{}
	var err error
	if query != "" {
		memories, err = s.fetchRaw(r.Context(), "memory.search", map[string]interface{}{"query": query, "k": 20})
	} else {
		memories, err = s.fetchRaw(r.Context(), "memory.list", map[string]interface{}{"limit": 50})
	}
	memErr := ""
	if err != nil {
		memErr = err.Error()
	}
	s.renderTemplate(w, "Memory Vault", memoryTmpl, map[string]interface{}{
		"Memories": memories,
		"Query":    query,
		"Error":    memErr,
	})
}

func (s *Server) handleApprovals(w http.ResponseWriter, r *http.Request) {
	showAll := r.URL.Query().Get("all") == "1"
	approvals, _ := s.fetchRaw(r.Context(), "event.approvals.list", map[string]bool{"pending_only": !showAll})
	s.renderTemplate(w, "Approvals", approvalsTmpl, map[string]interface{}{
		"Approvals": approvals,
		"ShowAll":   showAll,
	})
}

func (s *Server) handleApprovalsDecide(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	approvalID := r.FormValue("approval_id")
	approved := r.FormValue("decision") == "approve"
	reason := r.FormValue("reason")
	if approvalID == "" {
		http.Error(w, "approval_id required", http.StatusBadRequest)
		return
	}
	s.apiClient.Call(r.Context(), "event.approvals.decide", mustMarshal(map[string]interface{}{ //nolint:errcheck
		"approval_id": approvalID,
		"approved":    approved,
		"decided_by":  "user",
		"reason":      reason,
	}))
	http.Redirect(w, r, "/approvals", http.StatusSeeOther)
}

func (s *Server) handleSkills(w http.ResponseWriter, r *http.Request) {
	catalog, err := s.fetchRaw(r.Context(), "dashboard.skills", nil)
	catMap, _ := catalog.(map[string]interface{})
	pageErr := ""
	if err != nil {
		pageErr = err.Error()
	}
	s.renderTemplate(w, "Skills & Proposals", skillsTmpl, map[string]interface{}{
		"RuntimeSkills":    catMap["runtime_skills"],
		"BuiltInSkills":    catMap["built_in_skills"],
		"BuiltInTemplates": catMap["built_in_templates"],
		"Proposals":        catMap["proposals"],
		"Error":            pageErr,
	})
}

func (s *Server) handleSkillProposal(w http.ResponseWriter, r *http.Request) {
	prefix := "/skills/proposals/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		http.NotFound(w, r)
		return
	}
	id := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, prefix))
	if id == "" {
		http.Redirect(w, r, "/skills", http.StatusSeeOther)
		return
	}

	detail, err := s.fetchRaw(r.Context(), "dashboard.proposal", map[string]string{"id": id})
	detailMap, _ := detail.(map[string]interface{})
	pageErr := ""
	if err != nil {
		pageErr = err.Error()
	}

	s.renderTemplate(w, "Proposal Details", proposalDetailTmpl, map[string]interface{}{
		"ProposalID":           id,
		"Proposal":             detailMap["proposal"],
		"ReviewStatus":         detailMap["review_status"],
		"CurrentRoundFeedback": detailMap["current_round_feedback"],
		"PreviousRounds":       detailMap["previous_rounds"],
		"RevisionHistory":      detailMap["revision_history"],
		"Error":                pageErr,
	})
}

func (s *Server) handleAudit(w http.ResponseWriter, r *http.Request) {
	s.renderTemplate(w, "Audit Explorer", auditTmpl, nil)
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	s.renderTemplate(w, "Settings", settingsTmpl, nil)
}

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	s.renderTemplate(w, "Chat", chatTmpl, nil)
}

func (s *Server) handleCanvas(w http.ResponseWriter, r *http.Request) {
	// Canvas provides a real-time visual workspace: active agents, live
	// tool-call feed, and an agent interaction graph.  All data is served
	// via the existing /events SSE stream so no additional server state is
	// needed.
	workers, _ := s.fetchRaw(r.Context(), "worker.list", map[string]bool{"active_only": true})
	sandboxes, _ := s.fetchRaw(r.Context(), "sandbox.list", map[string]bool{"running_only": true})
	skills, _ := s.fetchRaw(r.Context(), "skill.list", nil)
	s.renderTemplate(w, "Canvas", canvasTmpl, map[string]interface{}{
		"Workers":   workers,
		"Sandboxes": sandboxes,
		"Skills":    skills,
	})
}

func (s *Server) handleChatSend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 512<<10) // 512 KB limit
	var req struct {
		Input     string `json:"input"`
		SessionID string `json:"session_id,omitempty"`
		History   []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"history,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid JSON: " + err.Error()}) //nolint:errcheck
		return
	}
	req.Input = strings.TrimSpace(req.Input)
	if req.Input == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "input required"}) //nolint:errcheck
		return
	}
	if r.URL.Query().Get("stream") == "1" || strings.Contains(strings.ToLower(r.Header.Get("Accept")), "text/event-stream") {
		streamID := fmt.Sprintf("chat-%d", time.Now().UnixNano())
		payload := mustMarshal(map[string]interface{}{
			"input":      req.Input,
			"history":    req.History,
			"session_id": req.SessionID,
			"stream_id":  streamID,
		})
		s.handleChatSendStream(w, r, payload, streamID)
		return
	}
	payload := mustMarshal(map[string]interface{}{
		"input":      req.Input,
		"history":    req.History,
		"session_id": req.SessionID,
	})
	resp, err := s.apiClient.Call(r.Context(), "chat.message", payload)
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()}) //nolint:errcheck
		return
	}
	if resp == nil || !resp.Success {
		errMsg := "unknown error"
		if resp != nil && resp.Error != "" {
			errMsg = resp.Error
		}
		json.NewEncoder(w).Encode(map[string]string{"error": errMsg}) //nolint:errcheck
		return
	}
	w.Write(resp.Data) //nolint:errcheck
}

func (s *Server) handleChatSendStream(w http.ResponseWriter, r *http.Request, payload json.RawMessage, streamID string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	writeSSE := func(v interface{}) bool {
		b, err := json.Marshal(v)
		if err != nil {
			return false
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}

	ctx := r.Context()
	lastToolID := s.latestEventID(ctx, "chat.tool_events", 60)
	lastThoughtID := s.latestEventID(ctx, "chat.thought_events", 80)
	emittedThinkingRunes := 0
	emittedContentRunes := 0
	lastProgressRequestID := ""

	if !writeSSE(map[string]interface{}{"type": "start", "ts": time.Now().UTC().Format(time.RFC3339)}) {
		return
	}

	type callResult struct {
		resp *APIResponse
		err  error
	}
	callDone := make(chan callResult, 1)
	go func() {
		resp, err := s.apiClient.Call(ctx, "chat.message", payload)
		callDone <- callResult{resp: resp, err: err}
	}()

	ticker := time.NewTicker(700 * time.Millisecond)
	defer ticker.Stop()

	sendNewEvents := func() bool {
		toolEvents := s.fetchEventsSince(ctx, "chat.tool_events", 60, lastToolID)
		for _, ev := range toolEvents {
			if id := eventID(ev); id > lastToolID {
				lastToolID = id
			}
			if !writeSSE(map[string]interface{}{"type": "tool_event", "event": ev}) {
				return false
			}
		}
		if streamID != "" {
			progressRaw, err := s.fetchRaw(ctx, "chat.stream_progress", map[string]string{"stream_id": streamID})
			if err == nil {
				if progress, ok := progressRaw.(map[string]interface{}); ok {
					requestID := toString(progress["request_id"])
					if requestID != "" && requestID != lastProgressRequestID {
						lastProgressRequestID = requestID
						emittedThinkingRunes = 0
						emittedContentRunes = 0
					}
					if !emitSnapshotDelta(writeSSE, "thought_delta", toString(progress["thinking"]), &emittedThinkingRunes) {
						return false
					}
					content := suppressInFlightStructuredContent(toString(progress["content"]))
					if !emitSnapshotDelta(writeSSE, "content_delta", content, &emittedContentRunes) {
						return false
					}
				}
			}
		}
		thoughtEvents := s.fetchEventsSince(ctx, "chat.thought_events", 80, lastThoughtID)
		for _, ev := range thoughtEvents {
			if id := eventID(ev); id > lastThoughtID {
				lastThoughtID = id
			}
			if !writeSSE(map[string]interface{}{"type": "thought_event", "event": ev}) {
				return false
			}
		}
		return true
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !sendNewEvents() {
				return
			}
		case out := <-callDone:
			if !sendNewEvents() {
				return
			}
			if out.err != nil {
				writeSSE(map[string]interface{}{"type": "error", "error": out.err.Error()}) //nolint:errcheck
				return
			}
			if out.resp == nil || !out.resp.Success {
				errMsg := "unknown error"
				if out.resp != nil && out.resp.Error != "" {
					errMsg = out.resp.Error
				}
				writeSSE(map[string]interface{}{"type": "error", "error": errMsg}) //nolint:errcheck
				return
			}
			var data interface{}
			if len(out.resp.Data) > 0 {
				if err := json.Unmarshal(out.resp.Data, &data); err != nil {
					writeSSE(map[string]interface{}{"type": "error", "error": "invalid chat response JSON: " + err.Error()}) //nolint:errcheck
					return
				}
			}
			if m, ok := data.(map[string]interface{}); ok {
				if !emitSnapshotDelta(writeSSE, "content_delta", toString(m["content"]), &emittedContentRunes) {
					return
				}
			}
			writeSSE(map[string]interface{}{"type": "final", "data": data}) //nolint:errcheck
			return
		}
	}
}

func suppressInFlightStructuredContent(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return text
	}

	// During streaming, suppress in-flight structured outputs and fenced blocks.
	// They are intermediate protocol artifacts, not user-visible prose.
	if strings.HasPrefix(trimmed, "```") {
		return ""
	}
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		return ""
	}

	return text
}

func emitSnapshotDelta(writeSSE func(interface{}) bool, eventType, text string, emittedRunes *int) bool {
	r := []rune(text)
	if emittedRunes == nil {
		return true
	}
	if *emittedRunes >= len(r) {
		return true
	}
	delta := string(r[*emittedRunes:])
	*emittedRunes = len(r)
	if strings.TrimSpace(delta) == "" && delta == "" {
		return true
	}
	return writeSSE(map[string]interface{}{"type": eventType, "delta": delta})
}

func emitTextDeltas(ctx context.Context, writeSSE func(interface{}) bool, eventType, text string, chunkSize int, delay time.Duration) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return true
	}
	if chunkSize <= 0 {
		chunkSize = 90
	}
	r := []rune(text)
	for i := 0; i < len(r); i += chunkSize {
		end := i + chunkSize
		if end > len(r) {
			end = len(r)
		}
		if !writeSSE(map[string]interface{}{"type": eventType, "delta": string(r[i:end])}) {
			return false
		}
		if delay > 0 {
			select {
			case <-ctx.Done():
				return false
			case <-time.After(delay):
			}
		}
	}
	return true
}

func toString(v interface{}) string {
	switch t := v.(type) {
	case string:
		return t
	case json.Number:
		return t.String()
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", t)
	}
}

func eventID(ev map[string]interface{}) int {
	v, ok := ev["id"]
	if !ok {
		return 0
	}
	switch t := v.(type) {
	case float64:
		return int(t)
	case float32:
		return int(t)
	case int:
		return t
	case int64:
		return int(t)
	case int32:
		return int(t)
	case json.Number:
		i, _ := t.Int64()
		return int(i)
	default:
		return 0
	}
}

func (s *Server) latestEventID(ctx context.Context, action string, limit int) int {
	raw, err := s.fetchRaw(ctx, action, map[string]int{"limit": limit})
	if err != nil {
		return 0
	}
	items := toEventMaps(raw)
	maxID := 0
	for _, ev := range items {
		if id := eventID(ev); id > maxID {
			maxID = id
		}
	}
	return maxID
}

func (s *Server) fetchEventsSince(ctx context.Context, action string, limit int, lastID int) []map[string]interface{} {
	raw, err := s.fetchRaw(ctx, action, map[string]int{"limit": limit})
	if err != nil {
		return nil
	}
	items := toEventMaps(raw)
	out := make([]map[string]interface{}, 0, len(items))
	for _, ev := range items {
		if eventID(ev) > lastID {
			out = append(out, ev)
		}
	}
	return out
}

func toEventMaps(raw interface{}) []map[string]interface{} {
	arr, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	out := make([]map[string]interface{}, 0, len(arr))
	for _, it := range arr {
		if m, ok := it.(map[string]interface{}); ok {
			out = append(out, m)
		}
	}
	return out
}

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	ctx := r.Context()
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	fmt.Fprintf(w, "data: {\"type\":\"heartbeat\"}\n\n")
	flusher.Flush()

	// Per-connection cursors so each client only receives new events.
	var lastToolEventID, lastWorkerEventID int64

	writeSSEMsg := func(v interface{}) bool {
		b, err := json.Marshal(v)
		if err != nil {
			return false
		}
		_, werr := fmt.Fprintf(w, "data: %s\n\n", b)
		flusher.Flush()
		return werr == nil
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			workers, _ := s.fetchRaw(ctx, "worker.list", map[string]bool{"active_only": true})
			approvals, _ := s.fetchRaw(ctx, "event.approvals.list", map[string]bool{"pending_only": true})
			toolEvents, _ := s.fetchRaw(ctx, "chat.tool_events", map[string]int{"limit": 40})
			thoughtEvents, _ := s.fetchRaw(ctx, "chat.thought_events", map[string]int{"limit": 60})
			sessionsList, _ := s.fetchRaw(ctx, "sessions.list", nil)

			// Emit individual tool_start/tool_end events for new tool events
			// so Canvas and other subscribers can react without parsing the
			// full update bundle.
			if toolEvSlice, ok := toolEvents.([]interface{}); ok {
				for _, raw := range toolEvSlice {
					ev, ok := raw.(map[string]interface{})
					if !ok {
						continue
					}
					id := int64(toFloat(ev["id"]))
					if id <= lastToolEventID {
						continue
					}
					lastToolEventID = id
					evType := "tool_end"
					if toString(ev["status"]) == "running" {
						evType = "tool_start"
					}
					writeSSEMsg(map[string]interface{}{ //nolint:errcheck
						"type": evType,
						"data": map[string]interface{}{
							"tool":        toString(ev["tool"]),
							"agent_id":    toString(ev["session_id"]),
							"agent_name":  toString(ev["session_id"]),
							"error":       toString(ev["error"]),
							"duration_ms": ev["duration_ms"],
						},
					})
				}
			}

			// Emit worker_start/worker_stop events for new worker transitions.
			if workerSlice, ok := workers.([]interface{}); ok {
				for _, raw := range workerSlice {
					wk, ok := raw.(map[string]interface{})
					if !ok {
						continue
					}
					id := int64(toFloat(wk["created_at_unix"]))
					if id <= lastWorkerEventID {
						continue
					}
					lastWorkerEventID = id
					writeSSEMsg(map[string]interface{}{ //nolint:errcheck
						"type": "worker_start",
						"data": wk,
					})
				}
			}

			payload, _ := json.Marshal(map[string]interface{}{
				"type":              "update",
				"active_workers":    workers,
				"pending_approvals": approvals,
				"tool_events":       toolEvents,
				"thought_events":    thoughtEvents,
				"sessions":          sessionsList,
				"ts":                time.Now().UTC().Format(time.RFC3339),
			})
			fmt.Fprintf(w, "data: %s\n\n", payload)
			flusher.Flush()
		}
	}
}

// toFloat safely converts an interface{} to float64 (used for numeric ID comparisons).
func toFloat(v interface{}) float64 {
	if f, ok := v.(float64); ok {
		return f
	}
	return 0
}

func (s *Server) fetchRaw(ctx context.Context, action string, req interface{}) (interface{}, error) {
	var payload json.RawMessage
	if req != nil {
		payload, _ = json.Marshal(req)
	}
	resp, err := s.apiClient.Call(ctx, action, payload)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, fmt.Errorf("empty response for action: %s", action)
	}
	if !resp.Success {
		if resp.Error != "" {
			return nil, fmt.Errorf("%s", resp.Error)
		}
		return nil, fmt.Errorf("action failed: %s", action)
	}
	var out interface{}
	json.Unmarshal(resp.Data, &out) //nolint:errcheck
	return out, nil
}

func (s *Server) renderTemplate(w http.ResponseWriter, title, tmplStr string, data map[string]interface{}) {
	if data == nil {
		data = make(map[string]interface{})
	}
	data["Title"] = title
	tmpl, err := template.New("page").Funcs(s.funcMap).Parse(tmplStr)
	if err != nil {
		http.Error(w, "template parse error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	var sb strings.Builder
	if err := tmpl.Execute(&sb, data); err != nil {
		http.Error(w, "template exec error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, pageWrap(title, sb.String()))
}

func mustMarshal(v interface{}) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

// countItems returns the number of items in v if it's a slice, else 0.
func countItems(v interface{}) int {
	if v == nil {
		return 0
	}
	if s, ok := v.([]interface{}); ok {
		return len(s)
	}
	return 0
}

func sandboxResourceTotals(v interface{}) (vcpus int64, memoryMB int64, rssMB int64) {
	if v == nil {
		return 0, 0, 0
	}
	list, ok := v.([]interface{})
	if !ok {
		return 0, 0, 0
	}
	for _, raw := range list {
		m, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if c, ok := m["vcpus"].(float64); ok {
			vcpus += int64(c)
		}
		if mem, ok := m["memory_mb"].(float64); ok {
			memoryMB += int64(mem)
		}
		if rss, ok := m["rss_mb"].(float64); ok {
			rssMB += int64(rss)
		}
	}
	return vcpus, memoryMB, rssMB
}

// fmtDashMB formats a megabyte count as "X.X GB" (when ≥ 1024) or "X MB".
func fmtDashMB(mb int64) string {
	if mb >= 1024 {
		return fmt.Sprintf("%.1f GB", float64(mb)/1024)
	}
	return fmt.Sprintf("%d MB", mb)
}

// pageWrap renders a full HTML page with shared chrome around the body content.
func pageWrap(title, body string) string {
	return `<!DOCTYPE html><html lang="en"><head><meta charset="UTF-8">` +
		`<meta name="viewport" content="width=device-width,initial-scale=1">` +
		`<title>` + template.HTMLEscapeString(title) + ` — AegisClaw</title>` +
		`<style>` + dashboardCSS + `</style></head><body>` +
		dashboardNav + `<main>` + body + `</main>` + dashboardSSEScript +
		`</body></html>`
}

const dashboardCSS = `
*{box-sizing:border-box;margin:0;padding:0}
body{font-family:system-ui,-apple-system,sans-serif;background:#0d1117;color:#e6edf3;line-height:1.5}
nav{background:#161b22;border-bottom:1px solid #30363d;padding:0 1.5rem;display:flex;align-items:center;gap:2rem;height:3rem}
nav a{color:#8b949e;text-decoration:none;font-size:.9rem;padding:.5rem 0;border-bottom:2px solid transparent}
nav a:hover{color:#e6edf3;border-bottom-color:#58a6ff}
.logo{font-weight:700;font-size:1rem;color:#58a6ff;margin-right:1rem}
main{max-width:1200px;margin:2rem auto;padding:0 1.5rem}
h1{font-size:1.4rem;font-weight:600;margin-bottom:1.5rem}
table{width:100%;border-collapse:collapse;font-size:.875rem}
th{text-align:left;padding:.5rem .75rem;color:#8b949e;border-bottom:1px solid #30363d;font-weight:500}
td{padding:.5rem .75rem;border-bottom:1px solid #21262d;vertical-align:top}
tr:hover td{background:#161b22}
.badge{display:inline-block;padding:.15rem .5rem;border-radius:9999px;font-size:.75rem;font-weight:500}
.badge-running{background:#1a7f37;color:#3fb950}
.badge-done{background:#0d419d;color:#58a6ff}
.badge-failed{background:#6e1a1a;color:#f85149}
.badge-pending{background:#633d00;color:#d29922}
.badge-active{background:#1a7f37;color:#3fb950}
.badge-approved,.badge-complete{background:#1a7f37;color:#3fb950}
.badge-implementing,.badge-in_review{background:#0d419d;color:#58a6ff}
.badge-draft,.badge-submitted,.badge-escalated{background:#633d00;color:#d29922}
.badge-inactive,.badge-stopped,.badge-not_bootstrapped{background:#21262d;color:#8b949e}
.badge-error,.badge-rejected{background:#6e1a1a;color:#f85149}
.badge-fired,.badge-cancelled{background:#21262d;color:#8b949e}
.empty{color:#8b949e;font-style:italic;padding:2rem;text-align:center}
.section{background:#161b22;border:1px solid #30363d;border-radius:6px;margin-bottom:1.5rem;overflow:hidden}
.section-header{padding:.75rem 1rem;border-bottom:1px solid #30363d;font-weight:600;font-size:.9rem;color:#e6edf3}
.muted{color:#8b949e;font-size:.82rem}
.tool-disclosure summary{cursor:pointer;color:#9ec1e6}
.tool-disclosure ul{margin:.5rem 0 0 1rem;padding:0}
.tool-disclosure li{margin:.2rem 0}
button{background:#21262d;color:#e6edf3;border:1px solid #30363d;border-radius:6px;padding:.3rem .75rem;cursor:pointer;font-size:.8rem}
button:hover{background:#30363d}
button.danger{background:#6e1a1a;border-color:#f85149;color:#f85149}
button.approve{background:#1a7f37;border-color:#3fb950;color:#3fb950}
input[type=text],input[type=search]{background:#0d1117;border:1px solid #30363d;border-radius:6px;color:#e6edf3;padding:.3rem .6rem;font-size:.875rem}
a.nav-link{color:#58a6ff}
#sse-status{font-size:.75rem;color:#8b949e;margin-left:auto}
#chat-wrap{position:fixed;top:3rem;bottom:0;left:0;right:0;display:flex;z-index:1}
#chat-layout{display:flex;flex:1;min-height:0}
#chat-sidebar{width:260px;background:#11161d;border-right:1px solid #30363d;display:flex;flex-direction:column}
#chat-sessions-header{padding:.8rem;border-bottom:1px solid #30363d;display:flex;justify-content:space-between;align-items:center}
#chat-sessions{overflow-y:auto;padding:.5rem;display:flex;flex-direction:column;gap:.35rem}
.session-item{border:1px solid #2b3440;background:#0d1117;border-radius:6px;padding:.5rem .6rem;cursor:pointer}
.session-item.active{border-color:#58a6ff;background:#122033}
.session-title{font-size:.82rem;color:#dbe5f1;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.session-meta{font-size:.72rem;color:#8b949e;margin-top:.2rem}
#chat-main{display:flex;flex-direction:column;flex:1;min-width:0}
#chat-msgs{flex:1;overflow-y:auto;padding:1.2rem;display:flex;flex-direction:column;gap:.9rem}
#chat-input-area{border-top:1px solid #30363d;padding:.75rem 1rem;background:#161b22}
.msg{display:flex}
.msg-user{justify-content:flex-end}
.msg-assistant,.msg-error{justify-content:flex-start}
.bubble{max-width:75%;padding:.6rem .9rem;border-radius:8px;white-space:pre-wrap;word-break:break-word;font-size:.875rem;line-height:1.6}
.msg-user .bubble{background:#1a3a6b;border:1px solid #2952a3;color:#e6edf3}
.msg-assistant .bubble{background:#161b22;border:1px solid #30363d}
.msg-error .bubble{background:#2d0f0f;border:1px solid #f85149;color:#f85149}
.markdown-bubble{white-space:normal}
.markdown-bubble p{margin:.45rem 0}
.markdown-bubble p:first-child{margin-top:0}
.markdown-bubble p:last-child{margin-bottom:0}
.markdown-bubble h1,.markdown-bubble h2,.markdown-bubble h3{margin:.65rem 0 .35rem 0;line-height:1.35}
.markdown-bubble h1{font-size:1.05rem}
.markdown-bubble h2{font-size:1rem}
.markdown-bubble h3{font-size:.95rem}
.markdown-bubble hr{border:none;border-top:1px solid #2b3440;margin:.65rem 0}
.markdown-bubble blockquote{margin:.55rem 0;padding:.25rem .75rem;border-left:3px solid #3a4a5e;background:#10161f;color:#c9d4e0}
.markdown-bubble blockquote p{margin:.25rem 0}
.markdown-bubble ul,.markdown-bubble ol{margin:.4rem 0 .45rem 1.2rem}
.markdown-bubble li{margin:.2rem 0}
.markdown-bubble ul.task-list{list-style:none;margin:.35rem 0 .45rem 0;padding-left:.1rem}
.markdown-bubble li.task-item{display:flex;align-items:flex-start;gap:.45rem}
.markdown-bubble li.task-item input{margin-top:.18rem;accent-color:#58a6ff}
.markdown-bubble li.task-item span{display:inline-block}
.markdown-bubble table{border-collapse:collapse;width:100%;margin:.55rem 0;background:#121821;border:1px solid #2b3440;border-radius:6px;overflow:hidden;display:block;overflow-x:auto}
.markdown-bubble th,.markdown-bubble td{border:1px solid #2b3440;padding:.35rem .55rem;text-align:left;vertical-align:top;white-space:nowrap}
.markdown-bubble th{background:#0f151d;color:#dce7f3;font-weight:600}
.markdown-bubble pre{white-space:pre-wrap;word-break:break-word;background:#0b1016;border:1px solid #2a323d;border-radius:6px;padding:.5rem .6rem;margin:.5rem 0;overflow:auto}
.markdown-bubble code{font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;background:#0f151d;border:1px solid #2a323d;border-radius:4px;padding:.08rem .28rem;font-size:.86em}
.markdown-bubble pre code{background:transparent;border:none;padding:0}
.markdown-bubble s{opacity:.9}
.markdown-bubble a{color:#79c0ff;text-decoration:underline}
.typing .bubble{color:#8b949e;font-style:italic}
.assistant-stack{display:flex;flex-direction:column;gap:.45rem;max-width:80%}
.assistant-meta{display:flex;gap:.5rem;align-items:center;flex-wrap:wrap}
.model-pill{font-size:.74rem;color:#dce7f3;background:#1b2330;border:1px solid #31405a;border-radius:999px;padding:.14rem .5rem}
.assistant-model-inline{font-size:.78rem;color:#9ec1e6;margin-bottom:.35rem}
.tool-log{border:1px solid #2f3a47;background:#0f151d;border-radius:8px;padding:.5rem .65rem;font-size:.8rem}
.tool-log-title{color:#b5c6da;font-weight:600;margin-bottom:.35rem}
.tool-call{border-top:1px dashed #2b3440;padding-top:.35rem;margin-top:.35rem}
.tool-call:first-of-type{border-top:none;margin-top:0;padding-top:0}
.tool-summary{display:flex;gap:.5rem;align-items:center;flex-wrap:wrap}
.tool-name{font-weight:600;color:#dce7f3}
.tool-state-ok{color:#3fb950}
.tool-state-fail{color:#f85149}
.tool-duration{color:#8b949e}
.tool-details{margin-top:.25rem}
.tool-details summary{cursor:pointer;color:#9ec1e6}
.tool-payload{white-space:pre-wrap;word-break:break-word;background:#0b1016;border:1px solid #2a323d;border-radius:6px;padding:.4rem .55rem;margin-top:.35rem;max-height:220px;overflow:auto}
.thought-log{border:1px solid #4a3f24;background:#1d1710;border-radius:8px;padding:.5rem .65rem;font-size:.8rem}
.thought-log-title{color:#f2d39b;font-weight:600;margin-bottom:.35rem}
.thought-step{border-top:1px dashed #5b4a2a;padding-top:.35rem;margin-top:.35rem}
.thought-step:first-of-type{border-top:none;margin-top:0;padding-top:0}
.thought-step--thinking{background:#111a0d;border-radius:4px;padding:.35rem .5rem;border-left:2px solid #52a04e;margin-top:.5rem}
.thought-step--thinking .tool-payload{max-height:400px}
.thought-summary{display:flex;gap:.5rem;align-items:center;flex-wrap:wrap}
.thought-phase{color:#f2cc60;font-weight:600}
.thought-phase--thinking{color:#52a04e;font-weight:600}
.thought-tool{color:#e6edf3}
.thought-model{color:#9ec1e6;font-size:.74rem}
.thought-time{color:#8b949e}
@media (max-width: 900px){
  #chat-sidebar{width:190px}
  .bubble{max-width:90%}
  .assistant-stack{max-width:94%}
}
`

const dashboardNav = `
<nav>
  <span class="logo">&#128737; AegisClaw</span>
  <a href="/">Overview</a>
  <a href="/canvas">Canvas</a>
  <a href="/chat">Chat</a>
  <a href="/agents">Agents</a>
  <a href="/skills">Skills</a>
  <a href="/pullrequests">PRs</a>
  <a href="/source">Source</a>
  <a href="/git">Git</a>
  <a href="/workspace">Workspace</a>
  <a href="/async">Async Hub</a>
  <a href="/memory">Memory</a>
  <a href="/approvals">Approvals</a>
  <a href="/audit">Audit</a>
  <a href="/settings">Settings</a>
  <span id="sse-status">&#9679;</span>
</nav>`

const dashboardSSEScript = `
<script>
(function(){
  const s=document.getElementById('sse-status');
  try{
    const es=new EventSource('/events');
    es.onopen=()=>{s.innerHTML='&#9679; live';s.style.color='#3fb950'};
    es.onerror=()=>{s.innerHTML='&#9679; disconnected';s.style.color='#f85149'};
    es.onmessage=(e)=>{const d=JSON.parse(e.data);if(d.type==='update'&&window.onSSEUpdate)window.onSSEUpdate(d)};
  }catch(e){s.innerHTML='&#9679; no sse'}
})();
</script>`

// handleSource displays the source code browser (Phase 2: Source Code Viewer).
func (s *Server) handleSource(w http.ResponseWriter, r *http.Request) {
	// Only support skills repository
	repo := "skills"

	// Get repository branches
	branches, _ := s.fetchRaw(r.Context(), "git.branches", map[string]string{"repo": repo})

	s.renderTemplate(w, "Source Code Browser", sourceTmpl, map[string]interface{}{
		"Repo":     repo,
		"Branches": branches,
	})
}

// handleSourceBrowse handles file browsing within a repository.
func (s *Server) handleSourceBrowse(w http.ResponseWriter, r *http.Request) {
	// Only support skills repository
	repo := "skills"
	path := r.URL.Query().Get("path")
	if path == "" {
		path = "/"
	}

	content, err := s.fetchRaw(r.Context(), "git.browse", map[string]string{
		"repo": repo,
		"path": path,
	})

	var errMsg string
	if err != nil {
		errMsg = err.Error()
	}

	w.Header().Set("Content-Type", "application/json")
	respData, _ := json.Marshal(map[string]interface{}{
		"content": content,
		"error":   errMsg,
	})
	w.Write(respData) //nolint:errcheck
}

// handleWorkspace displays the workspace editor for user files.
func (s *Server) handleWorkspace(w http.ResponseWriter, r *http.Request) {
	files, _ := s.fetchRaw(r.Context(), "workspace.list", nil)

	s.renderTemplate(w, "Workspace", workspaceTmpl, map[string]interface{}{
		"Files": files,
	})
}

// handleWorkspaceEdit handles editing workspace files.
func (s *Server) handleWorkspaceEdit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}

	filename := r.FormValue("filename")
	content := r.FormValue("content")

	if filename == "" {
		http.Error(w, "filename required", http.StatusBadRequest)
		return
	}

	payload := mustMarshal(map[string]string{
		"filename": filename,
		"content":  content,
	})

	_, err := s.apiClient.Call(r.Context(), "workspace.write", payload)
	if err != nil {
		http.Error(w, "failed to save: "+err.Error(), http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/workspace", http.StatusSeeOther)
}

// handleGitHistory displays git commit history and branch information (Phase 3).
func (s *Server) handleGitHistory(w http.ResponseWriter, r *http.Request) {
	// Only support skills repository
	repo := "skills"
	proposalID := r.URL.Query().Get("proposal")

	// Get branches
	branches, _ := s.fetchRaw(r.Context(), "git.branches", map[string]string{"repo": repo})

	// Get commits if proposal ID is specified
	var commits interface{}
	if proposalID != "" {
		commits, _ = s.fetchRaw(r.Context(), "git.commits", map[string]interface{}{
			"repo":        repo,
			"proposal_id": proposalID,
			"limit":       50,
		})
	}

	s.renderTemplate(w, "Git History & Branches", gitHistoryTmpl, map[string]interface{}{
		"Repo":       repo,
		"ProposalID": proposalID,
		"Branches":   branches,
		"Commits":    commits,
	})
}

// handleGitDiff displays a diff for a proposal branch (Phase 3).
func (s *Server) handleGitDiff(w http.ResponseWriter, r *http.Request) {
	// Only support skills repository
	repo := "skills"
	proposalID := r.URL.Query().Get("proposal")

	if proposalID == "" {
		http.Error(w, "proposal ID required", http.StatusBadRequest)
		return
	}

	diff, err := s.fetchRaw(r.Context(), "git.diff", map[string]string{
		"repo":        repo,
		"proposal_id": proposalID,
	})

	var errMsg string
	if err != nil {
		errMsg = err.Error()
	}

	s.renderTemplate(w, "Diff for proposal-"+proposalID, gitDiffTmpl, map[string]interface{}{
		"Repo":       repo,
		"ProposalID": proposalID,
		"Diff":       diff,
		"Error":      errMsg,
	})
}

const agentsTmpl = `
<h1>{{.Title}}</h1>
<div class="section">
  <div class="section-header">Workers &mdash; ephemeral agents spawned by Orchestrator</div>
  {{if .Workers}}
  <table>
    <thead><tr><th>ID</th><th>Role</th><th>Status</th><th>Steps</th><th>Task</th><th>Spawned</th></tr></thead>
    <tbody>
    {{range .Workers}}
    <tr>
      <td><code>{{truncate (index . "worker_id") 8}}</code></td>
      <td>{{index . "role"}}</td>
      <td><span class="badge badge-{{index . "status"}}">{{index . "status"}}</span></td>
      <td>{{index . "step_count"}}</td>
      <td>{{truncate (index . "task_description") 60}}</td>
      <td>{{index . "spawned_at"}}</td>
    </tr>
    {{end}}
    </tbody>
  </table>
  {{else}}
  <p class="empty">No workers spawned yet. The Orchestrator spawns workers for complex subtasks.</p>
  {{end}}
</div>`

const asyncTmpl = `
<h1>{{.Title}}</h1>
<div class="section">
  <div class="section-header">Active Timers</div>
  {{if .Timers}}
  <table>
    <thead><tr><th>ID</th><th>Name</th><th>Status</th><th>Next Fire</th><th>Task</th></tr></thead>
    <tbody>
    {{range .Timers}}
    <tr>
      <td><code>{{truncate (index . "timer_id") 8}}</code></td>
      <td>{{index . "name"}}</td>
      <td><span class="badge badge-{{index . "status"}}">{{index . "status"}}</span></td>
      <td>{{index . "next_fire_at"}}</td>
      <td>{{index . "task_id"}}</td>
    </tr>
    {{end}}
    </tbody>
  </table>
  {{else}}
  <p class="empty">No timers. Use set_timer in chat to schedule async work.</p>
  {{end}}
</div>
<div class="section">
  <div class="section-header">Recent Signals</div>
  {{if .Signals}}
  <table>
    <thead><tr><th>ID</th><th>Source</th><th>Type</th><th>Task</th><th>Received</th></tr></thead>
    <tbody>
    {{range .Signals}}
    <tr>
      <td><code>{{truncate (index . "signal_id") 8}}</code></td>
      <td>{{index . "source"}}</td>
      <td>{{index . "type"}}</td>
      <td>{{index . "task_id"}}</td>
      <td>{{index . "received_at"}}</td>
    </tr>
    {{end}}
    </tbody>
  </table>
  {{else}}
  <p class="empty">No signals received yet.</p>
  {{end}}
</div>`

const memoryTmpl = `
<h1>{{.Title}}</h1>
<form method="GET" action="/memory" style="margin-bottom:1rem;display:flex;gap:.5rem">
  <input type="search" name="q" value="{{.Query}}" placeholder="Search memories..." style="width:300px">
  <button type="submit">Search</button>
</form>
<div class="section">
  <div class="section-header">Memory Entries{{if .Query}} &mdash; searching: &#8220;{{.Query}}&#8221;{{end}}</div>
  {{if .Error}}
  <p class="empty" style="color:#f85149">Failed to load memories: {{.Error}}</p>
  {{else if .Memories}}
  <table>
    <thead><tr><th>Key</th><th>Value</th><th>TTL</th></tr></thead>
    <tbody>
    {{range .Memories}}
    <tr>
      <td><code>{{index . "key"}}</code></td>
      <td>{{truncate (index . "value") 100}}</td>
      <td>{{index . "ttl_tier"}}</td>
    </tr>
    {{end}}
    </tbody>
  </table>
  {{else}}
  <p class="empty">No memory entries found.{{if .Query}} Try a different query.{{end}}</p>
  {{end}}
</div>`

const approvalsTmpl = `
<h1>{{.Title}}</h1>
<div style="margin-bottom:1rem">
  {{if .ShowAll}}<a href="/approvals" class="nav-link">Show pending only</a>
  {{else}}<a href="/approvals?all=1" class="nav-link">Show all approvals</a>{{end}}
</div>
<div class="section">
  <div class="section-header">{{if .ShowAll}}All Approvals{{else}}Pending Approvals{{end}}</div>
  {{if .Approvals}}
  {{range .Approvals}}
  <div style="padding:1rem;border-bottom:1px solid #21262d">
    <div style="display:flex;justify-content:space-between;align-items:flex-start;margin-bottom:.5rem">
      <div>
        <strong>{{index . "title"}}</strong>
        <span class="badge badge-{{index . "status"}}" style="margin-left:.5rem">{{index . "status"}}</span>
        <span class="badge badge-pending" style="margin-left:.25rem">risk: {{index . "risk_level"}}</span>
      </div>
      <code style="font-size:.75rem;color:#8b949e">{{index . "approval_id"}}</code>
    </div>
    {{with index . "description"}}<p style="color:#8b949e;font-size:.875rem;margin-bottom:.75rem">{{truncate . 200}}</p>{{end}}
    {{if eq (index . "status") "pending"}}
    <form method="POST" action="/approvals/decide" style="display:flex;gap:.5rem;align-items:center">
      <input type="hidden" name="approval_id" value="{{index . "approval_id"}}">
      <input type="text" name="reason" placeholder="Reason (optional)" style="width:200px">
      <button type="submit" name="decision" value="approve" class="approve">Approve</button>
      <button type="submit" name="decision" value="reject" class="danger">Reject</button>
    </form>
    {{end}}
  </div>
  {{end}}
  {{else}}
  <p class="empty">{{if .ShowAll}}No approval requests found.{{else}}No pending approvals.{{end}}</p>
  {{end}}
</div>`

const auditTmpl = `
<h1>{{.Title}}</h1>
<div class="section">
  <div class="section-header">Merkle Audit Log</div>
  <p style="padding:1rem;color:#8b949e;font-size:.875rem">
    The Merkle audit log is append-only and cryptographically linked.<br>
    Verify: <code>aegisclaw audit verify</code> &nbsp;|&nbsp; Export: <code>aegisclaw audit log</code>
  </p>
</div>`

const settingsTmpl = `
<h1>{{.Title}}</h1>
<div class="section">
  <div class="section-header">System Settings</div>
  <div style="padding:1rem">
    <p style="color:#8b949e;font-size:.875rem;margin-bottom:1rem">
      Configuration: <code>~/.config/aegisclaw/config.yaml</code>. Restart daemon after changes.
    </p>
    <table style="width:auto">
      <tr><th style="width:260px">Setting</th><th>Description</th></tr>
      <tr><td><code>agent.structured_output</code></td><td>Enable JSON-mode for LLM responses</td></tr>
      <tr><td><code>memory.default_ttl</code></td><td>Default TTL tier for new memories (90d/180d/365d/2yr/forever)</td></tr>
      <tr><td><code>memory.pii_redaction</code></td><td>Automatically redact PII (email, phone, SSN, IP, JWT, AWS keys) before storing memories</td></tr>
      <tr><td><code>eventbus.max_pending_timers</code></td><td>Max concurrent active timers</td></tr>
      <tr><td><code>worker.max_concurrent</code></td><td>Max concurrent Worker VMs</td></tr>
      <tr><td><code>worker.default_timeout_mins</code></td><td>Default Worker task timeout</td></tr>
      <tr><td><code>dashboard.addr</code></td><td>Dashboard listen address (default 127.0.0.1:7878)</td></tr>
    </table>
  </div>
</div>
<div class="section">
  <div class="section-header">Privacy Controls</div>
  <div style="padding:1rem">
    <p style="color:#8b949e;font-size:.875rem;margin-bottom:.75rem">
      PII redaction scrubs common sensitive patterns before storing in the encrypted memory vault.<br>
      Enable with <code>memory.pii_redaction: true</code> in config.yaml.<br>
      For GDPR right-to-forget: <code>aegisclaw memory delete &lt;query&gt;</code>
    </p>
    <p style="color:#8b949e;font-size:.875rem">
      Redacted patterns: email addresses, US phone numbers, SSNs, IPv4 addresses, JWT tokens, AWS access keys, generic API keys/passwords.
    </p>
  </div>
</div>`

const overviewTmpl = `
<h1>{{.Title}}</h1>
<div style="display:grid;grid-template-columns:repeat(auto-fit,minmax(180px,1fr));gap:1rem;margin-bottom:1.5rem">
  <div class="section" style="padding:1.25rem;text-align:center">
    <div style="font-size:2rem;font-weight:700;color:#f2cc60">{{.RunningVMCount}}</div>
    <div style="font-size:.85rem;color:#8b949e;margin-top:.25rem">Running MicroVMs</div>
  </div>
  <div class="section" style="padding:1.25rem;text-align:center">
    <div style="font-size:2rem;font-weight:700;color:#3fb950">{{.WorkerCount}}</div>
    <div style="font-size:.85rem;color:#8b949e;margin-top:.25rem">Active Workers</div>
  </div>
  <div class="section" style="padding:1.25rem;text-align:center">
    <div style="font-size:2rem;font-weight:700;color:#d29922">{{.ApprovalCount}}</div>
    <div style="font-size:.85rem;color:#8b949e;margin-top:.25rem">Pending Approvals</div>
  </div>
  <div class="section" style="padding:1.25rem;text-align:center">
    <div style="font-size:2rem;font-weight:700;color:#58a6ff">{{.TimerCount}}</div>
    <div style="font-size:.85rem;color:#8b949e;margin-top:.25rem">Active Timers</div>
  </div>
  <div class="section" style="padding:1.25rem;text-align:center">
    <div style="font-size:2rem;font-weight:700;color:#a5d6ff">{{.MemoryCount}}</div>
    <div style="font-size:.85rem;color:#8b949e;margin-top:.25rem">Memory Entries</div>
  </div>
  <div class="section" style="padding:1.25rem;text-align:center">
    <div style="font-size:2rem;font-weight:700;color:#7ee787">{{.RunningVMVCPUs}}</div>
    <div style="font-size:.85rem;color:#8b949e;margin-top:.25rem">Allocated vCPUs</div>
  </div>
  <div class="section" style="padding:1.25rem;text-align:center">
    <div style="font-size:2rem;font-weight:700;color:#79c0ff">{{.RunningVMMemoryMB}} MB</div>
    {{if .RunningVMRSSMB}}<div style="font-size:.8rem;color:#e8916a;margin-top:.15rem">{{.RunningVMRSSMB}} MB actual RSS</div>{{end}}
    <div style="font-size:.85rem;color:#8b949e;margin-top:.15rem">Allocated VM Memory</div>
  </div>
{{if .HostRAMLabel}}
  <div class="section" style="padding:1.25rem;text-align:center">
    <div style="font-size:1.5rem;font-weight:700;color:#e8916a">{{.HostRAMLabel}}</div>
    <div style="height:6px;border-radius:3px;background:#30363d;margin:.5rem 0">
      <div style="height:100%;border-radius:3px;background:#e8916a;width:{{.HostRAMPct}}%"></div>
    </div>
    <div style="font-size:.85rem;color:#8b949e">Host RAM ({{.HostRAMPct}}%)</div>
  </div>
  <div class="section" style="padding:1.25rem;text-align:center">
    <div style="font-size:2rem;font-weight:700;color:#d2a8ff">{{.HostLoadLabel}}</div>
    <div style="font-size:.85rem;color:#8b949e;margin-top:.25rem">CPU Load Avg (1m)</div>
  </div>
{{end}}
</div>

{{if .RunningVMs}}
<div class="section">
  <div class="section-header">Running MicroVMs</div>
  <table>
    <thead><tr><th>Name</th><th>ID</th><th>Status</th><th>vCPUs</th><th>Alloc Mem</th><th>RSS</th><th>CPU avg</th></tr></thead>
    <tbody>
    {{range .RunningVMs}}
    <tr>
      <td><strong>{{index . "name"}}</strong></td>
      <td><code>{{truncate (index . "id") 12}}</code></td>
      <td><span class="badge badge-running">{{index . "state"}}</span></td>
      <td>{{index . "vcpus"}}</td>
      <td>{{index . "memory_mb"}} MB</td>
      <td>{{if index . "rss_mb"}}{{index . "rss_mb"}} MB{{else}}-{{end}}</td>
      <td>{{if index . "cpu_avg_pct"}}{{index . "cpu_avg_pct"}}%{{else}}-{{end}}</td>
    </tr>
    {{end}}
    </tbody>
  </table>
</div>
{{end}}

{{if .Workers}}
<div class="section">
  <div class="section-header">Active Workers</div>
  <table>
    <thead><tr><th>ID</th><th>Role</th><th>Status</th><th>Task</th></tr></thead>
    <tbody>
    {{range .Workers}}
    <tr>
      <td><code>{{truncate (index . "worker_id") 8}}</code></td>
      <td>{{index . "role"}}</td>
      <td><span class="badge badge-{{index . "status"}}">{{index . "status"}}</span></td>
      <td>{{truncate (index . "task_description") 80}}</td>
    </tr>
    {{end}}
    </tbody>
  </table>
</div>
{{end}}

{{if .Approvals}}
<div class="section">
  <div class="section-header">Pending Approvals &mdash; <a href="/approvals" class="nav-link">View all</a></div>
  {{range .Approvals}}
  <div style="padding:.75rem 1rem;border-bottom:1px solid #21262d;display:flex;justify-content:space-between">
    <div>
      <strong>{{index . "title"}}</strong>
      <span class="badge badge-pending" style="margin-left:.5rem">{{index . "risk_level"}}</span>
    </div>
    <a href="/approvals" class="nav-link" style="font-size:.85rem">Review</a>
  </div>
  {{end}}
</div>
{{end}}

{{if and (eq .WorkerCount 0) (eq .ApprovalCount 0) (eq .TimerCount 0)}}
<div class="section">
  <p class="empty">System is idle. Start a chat session to get going.</p>
</div>
{{end}}`

const skillsTmpl = `
<h1>{{.Title}}</h1>
<div class="section">
  <div class="section-header">Runtime Skills</div>
  {{if .Error}}
  <p class="empty" style="color:#f85149">Failed to load skills catalog: {{.Error}}</p>
  {{else if .RuntimeSkills}}
  <table>
    <thead><tr><th>Name</th><th>Version</th><th>Status</th><th>Sandbox</th><th>Tools</th></tr></thead>
    <tbody>
    {{range .RuntimeSkills}}
    <tr>
      <td>
        <strong>{{index . "name"}}</strong>
        {{with index . "description"}}<div class="muted">{{.}}</div>{{end}}
        {{with index . "proposal_id"}}<div class="muted">proposal {{.}}</div>{{end}}
      </td>
      <td>{{index . "version"}}</td>
      <td><span class="badge badge-{{index . "state"}}">{{index . "state"}}</span></td>
      <td><code>{{truncate (index . "sandbox_id") 12}}</code></td>
      <td>
        {{if index . "tools"}}
        <details class="tool-disclosure">
          <summary>{{len (index . "tools")}} tools</summary>
          <ul>
            {{range index . "tools"}}
            <li><strong>{{index . "name"}}</strong> {{index . "description"}}</li>
            {{end}}
          </ul>
        </details>
        {{else}}
        <span class="muted">No tool metadata available</span>
        {{end}}
      </td>
    </tr>
    {{end}}
    </tbody>
  </table>
  {{else}}
  <p class="empty">No runtime skills registered yet. Use <code>aegisclaw skill add</code> to create one.</p>
  {{end}}
</div>
<div class="section">
  <div class="section-header">Built-In Baselines</div>
  {{if .BuiltInSkills}}
  <table>
    <thead><tr><th>Name</th><th>Status</th><th>Source</th><th>Tools</th></tr></thead>
    <tbody>
    {{range .BuiltInSkills}}
    <tr>
      <td>
        <strong>{{index . "name"}}</strong>
        {{with index . "description"}}<div class="muted">{{.}}</div>{{end}}
      </td>
      <td><span class="badge badge-{{index . "state"}}">{{index . "state"}}</span></td>
      <td>{{index . "source"}}</td>
      <td>
        {{if index . "tools"}}
        <details class="tool-disclosure">
          <summary>{{len (index . "tools")}} tools</summary>
          <ul>
            {{range index . "tools"}}
            <li><strong>{{index . "name"}}</strong> {{index . "description"}}</li>
            {{end}}
          </ul>
        </details>
        {{else}}
        <span class="muted">No tool metadata available</span>
        {{end}}
      </td>
    </tr>
    {{end}}
    </tbody>
  </table>
  {{else}}
  <p class="empty">No built-in baselines detected.</p>
  {{end}}
</div>
<div class="section">
  <div class="section-header">Built-In Templates</div>
  {{if .BuiltInTemplates}}
  <table>
    <thead><tr><th>Name</th><th>Kind</th><th>Description</th></tr></thead>
    <tbody>
    {{range .BuiltInTemplates}}
    <tr>
      <td><strong>{{index . "name"}}</strong></td>
      <td>{{index . "kind"}}</td>
      <td>{{index . "description"}}</td>
    </tr>
    {{end}}
    </tbody>
  </table>
  {{else}}
  <p class="empty">No built-in templates found.</p>
  {{end}}
</div>
<div class="section">
  <div class="section-header">Proposals</div>
  {{if .Proposals}}
  <table>
    <thead><tr><th>ID</th><th>Title</th><th>Status</th><th>Category</th><th>Target Skill</th><th>Details</th></tr></thead>
    <tbody>
    {{range .Proposals}}
    <tr>
      <td><code>{{truncate (index . "id") 8}}</code></td>
      <td>{{truncate (index . "title") 60}}</td>
      <td><span class="badge badge-{{index . "status"}}">{{index . "status"}}</span></td>
      <td>{{index . "category"}}</td>
      <td>{{index . "target_skill"}}</td>
      <td><a href="/skills/proposals/{{index . "id"}}" class="nav-link">View details</a></td>
    </tr>
    {{end}}
    </tbody>
  </table>
  {{else}}
  <p class="empty">No proposals yet. Submit a skill proposal via <code>aegisclaw skill add</code>.</p>
  {{end}}
</div>`

const proposalDetailTmpl = `
<h1>{{.Title}}</h1>
<div class="section">
  <div class="section-header">Summary</div>
  {{if .Error}}
  <p class="empty" style="color:#f85149">Failed to load proposal {{.ProposalID}}: {{.Error}}</p>
  {{else if .Proposal}}
  <div style="padding:1rem">
    <p style="margin-bottom:.4rem"><a href="/skills" class="nav-link">&larr; Back to Skills</a></p>
    <h2 style="font-size:1.15rem;margin-bottom:.6rem">{{index .Proposal "title"}}</h2>
    <p style="color:#8b949e;margin-bottom:1rem">{{index .Proposal "description"}}</p>
    <table style="width:auto">
      <tr><th style="width:220px">Proposal ID</th><td><code>{{index .Proposal "id"}}</code></td></tr>
      <tr><th>Status</th><td><span class="badge badge-{{index .Proposal "status"}}">{{index .Proposal "status"}}</span></td></tr>
      <tr><th>Category</th><td>{{index .Proposal "category"}}</td></tr>
      <tr><th>Risk</th><td>{{index .Proposal "risk"}}</td></tr>
      <tr><th>Round</th><td>{{index .Proposal "round"}}</td></tr>
      <tr><th>Version</th><td>{{index .Proposal "version"}}</td></tr>
      <tr><th>Author</th><td>{{index .Proposal "author"}}</td></tr>
      <tr><th>Target Skill</th><td>{{index .Proposal "target_skill"}}</td></tr>
      <tr><th>Created</th><td>{{index .Proposal "created_at"}}</td></tr>
      <tr><th>Updated</th><td>{{index .Proposal "updated_at"}}</td></tr>
    </table>
  </div>
  {{else}}
  <p class="empty">Proposal not found.</p>
  {{end}}
</div>

{{if .Proposal}}
<div class="section">
  <div class="section-header">Current Review Status</div>
  <div style="padding:1rem;display:grid;grid-template-columns:repeat(auto-fit,minmax(170px,1fr));gap:.75rem">
    <div><div class="muted">Current Round</div><strong>{{index .ReviewStatus "current_round"}}</strong></div>
    <div><div class="muted">Reviews This Round</div><strong>{{index .ReviewStatus "current_count"}}</strong></div>
    <div><div class="muted">Pending Reviews</div><strong>{{index .ReviewStatus "pending_reviews"}}</strong></div>
    <div><div class="muted">Approvals</div><strong>{{index .ReviewStatus "approval_count"}}</strong></div>
    <div><div class="muted">Rejects</div><strong>{{index .ReviewStatus "reject_count"}}</strong></div>
    <div><div class="muted">Asks</div><strong>{{index .ReviewStatus "ask_count"}}</strong></div>
    <div><div class="muted">Abstains</div><strong>{{index .ReviewStatus "abstain_count"}}</strong></div>
  </div>
</div>

<div class="section">
  <div class="section-header">Feedback in Current Round</div>
  {{if .CurrentRoundFeedback}}
  <table>
    <thead><tr><th>Persona</th><th>Verdict</th><th>Risk Score</th><th>Comments</th><th>Questions</th><th>Timestamp</th></tr></thead>
    <tbody>
    {{range .CurrentRoundFeedback}}
    <tr>
      <td>{{index . "persona"}}</td>
      <td><span class="badge">{{index . "verdict"}}</span></td>
      <td>{{index . "risk_score"}}</td>
      <td>{{index . "comments"}}</td>
      <td>
        {{if index . "questions"}}
          {{range index . "questions"}}<div>{{.}}</div>{{end}}
        {{else}}<span class="muted">None</span>{{end}}
      </td>
      <td>{{index . "timestamp"}}</td>
    </tr>
    {{end}}
    </tbody>
  </table>
  {{else}}
  <p class="empty">No review feedback has been recorded for the current round.</p>
  {{end}}
</div>

<div class="section">
  <div class="section-header">Feedback in Previous Rounds</div>
  {{if .PreviousRounds}}
  {{range .PreviousRounds}}
  <div style="padding:1rem;border-bottom:1px solid #21262d">
    <h3 style="font-size:1rem;margin-bottom:.6rem">Round {{index . "round"}}</h3>
    <table>
      <thead><tr><th>Persona</th><th>Verdict</th><th>Risk Score</th><th>Comments</th><th>Timestamp</th></tr></thead>
      <tbody>
      {{range index . "reviews"}}
      <tr>
        <td>{{index . "persona"}}</td>
        <td><span class="badge">{{index . "verdict"}}</span></td>
        <td>{{index . "risk_score"}}</td>
        <td>{{index . "comments"}}</td>
        <td>{{index . "timestamp"}}</td>
      </tr>
      {{end}}
      </tbody>
    </table>
  </div>
  {{end}}
  {{else}}
  <p class="empty">No feedback from previous rounds.</p>
  {{end}}
</div>

<div class="section">
  <div class="section-header">Revision & Status History</div>
  {{if .RevisionHistory}}
  <table>
    <thead><tr><th>Timestamp</th><th>Actor</th><th>From</th><th>To</th><th>Reason</th></tr></thead>
    <tbody>
    {{range .RevisionHistory}}
    <tr>
      <td>{{index . "timestamp"}}</td>
      <td>{{index . "actor"}}</td>
      <td>{{index . "from"}}</td>
      <td>{{index . "to"}}</td>
      <td>{{index . "reason"}}</td>
    </tr>
    {{end}}
    </tbody>
  </table>
  {{else}}
  <p class="empty">No revision history available.</p>
  {{end}}
</div>
{{end}}`

const chatTmpl = `
<div id="chat-wrap">
  <div id="chat-layout">
    <aside id="chat-sidebar">
      <div id="chat-sessions-header">
        <strong>Sessions</strong>
        <button type="button" id="new-session-btn">New</button>
      </div>
      <div id="chat-sessions"></div>
    </aside>
    <section id="chat-main">
      <div id="chat-msgs"></div>
      <div id="chat-input-area">
        <form id="chat-form">
          <div style="display:flex;gap:.5rem;align-items:flex-end">
            <textarea id="chat-input" rows="1"
              placeholder="Message the agent… (Enter to send, Shift+Enter for newline)"
              style="flex:1;resize:none;background:#0d1117;border:1px solid #30363d;border-radius:6px;color:#e6edf3;padding:.5rem .75rem;font-size:.875rem;font-family:inherit;line-height:1.5;max-height:120px;overflow-y:auto"></textarea>
            <button type="submit" id="send-btn">Send</button>
          </div>
        </form>
      </div>
    </section>
  </div>
</div>
<script>
(function(){
  var SESSION_KEY='aegisclaw.chat.sessions.v1';
  var MAX=120;
  var sessions=[];
  var activeSessionId='';

  function appendMsg(role,text){
    var msgs=document.getElementById('chat-msgs');
    var div=document.createElement('div');
    div.className='msg msg-'+role;
    var bub=document.createElement('div');
    bub.className='bubble';
    bub.textContent=text;
    div.appendChild(bub);
    msgs.appendChild(div);
    msgs.scrollTop=msgs.scrollHeight;
    return div;
  }

  function safeText(v){
    return (v===undefined||v===null)?'':String(v);
  }

  function escapeHTML(s){
    return String(s)
      .replace(/&/g,'&amp;')
      .replace(/</g,'&lt;')
      .replace(/>/g,'&gt;')
      .replace(/"/g,'&quot;')
      .replace(/'/g,'&#39;');
  }

  function decodeEntities(s){
    var ta=document.createElement('textarea');
    ta.innerHTML=s;
    return ta.value;
  }

  function sanitizeURL(raw){
    var url=String(raw||'').trim();
    if(!url)return '';
    if(url[0]==='/' || url[0]==='#')return url;
    try{
      var parsed=new URL(url,window.location.origin);
      var p=parsed.protocol.toLowerCase();
      if(p==='http:'||p==='https:'||p==='mailto:')return parsed.href;
      return '';
    }catch(_){
      return '';
    }
  }

  function renderInlineMarkdownSafe(input){
    // input is already escaped by renderMarkdownSafe; do not escape again.
    var text=safeText(input);
    var codeSpans=[];

    text=text.replace(/\x60([^\x60]+)\x60/g,function(_,code){
      codeSpans.push(code);
      return '@@CODESPAN'+(codeSpans.length-1)+'@@';
    });

    text=text.replace(/\[([^\]]+)\]\(([^)]+)\)/g,function(_,label,url){
      var decodedURL=decodeEntities(url);
      var safeURL=sanitizeURL(decodedURL);
      if(!safeURL){
        return label+' ('+url+')';
      }
      return '<a href="'+escapeHTML(safeURL)+'" target="_blank" rel="noopener noreferrer">'+label+'</a>';
    });

    text=text.replace(/\*\*([^*]+)\*\*/g,'<strong>$1</strong>');
    text=text.replace(/~~([^~]+)~~/g,'<s>$1</s>');
    text=text.replace(/(^|[^*])\*([^*]+)\*(?!\*)/g,'$1<em>$2</em>');

    text=text.replace(/@@CODESPAN(\d+)@@/g,function(_,idx){
      var i=parseInt(idx,10);
      return '<code>'+codeSpans[i]+'</code>';
    });

    return text;
  }

  function renderMarkdownSafe(input){
    var src=safeText(input).replace(/\r\n/g,'\n');
    if(!src)return '';

    var escaped=escapeHTML(src);
    var codeBlocks=[];
    escaped=escaped.replace(/\x60\x60\x60([a-zA-Z0-9_+-]+)?\n([\s\S]*?)\x60\x60\x60/g,function(_,lang,code){
      codeBlocks.push('<pre><code>'+code+'</code></pre>');
      return '@@CODEBLOCK'+(codeBlocks.length-1)+'@@';
    });

    var lines=escaped.split('\n');
    var html=[];
    var para=[];
    var inUL=false;
    var inOL=false;
    var inTask=false;

    function closeLists(){
      if(inUL){html.push('</ul>');inUL=false;}
      if(inOL){html.push('</ol>');inOL=false;}
      if(inTask){html.push('</ul>');inTask=false;}
    }
    function flushPara(){
      if(para.length===0)return;
      html.push('<p>'+renderInlineMarkdownSafe(para.join('<br>'))+'</p>');
      para=[];
    }

    function parseTableCells(line){
      var t=line.trim();
      if(t.indexOf('|')===-1)return null;
      if(t[0]==='|')t=t.slice(1);
      if(t[t.length-1]==='|')t=t.slice(0,-1);
      var cells=t.split('|');
      for(var ci=0;ci<cells.length;ci++){
        cells[ci]=cells[ci].trim();
      }
      if(cells.length===0)return null;
      return cells;
    }

    function parseAlignmentCell(cell){
      var c=cell.trim();
      if(!/^:?-{3,}:?$/.test(c))return null;
      var left=c[0]===':';
      var right=c[c.length-1]===':';
      if(left&&right)return 'center';
      if(right)return 'right';
      return 'left';
    }

    function tryParseTable(startIdx){
      if(startIdx+1>=lines.length)return null;
      var header=parseTableCells(lines[startIdx]);
      var alignCells=parseTableCells(lines[startIdx+1]);
      if(!header||!alignCells)return null;
      if(header.length!==alignCells.length)return null;

      var aligns=[];
      for(var ai=0;ai<alignCells.length;ai++){
        var al=parseAlignmentCell(alignCells[ai]);
        if(!al)return null;
        aligns.push(al);
      }

      var rows=[];
      var idx=startIdx+2;
      while(idx<lines.length){
        var ln=lines[idx];
        if(!ln || !ln.trim())break;
        if(/^@@CODEBLOCK\d+@@$/.test(ln.trim()))break;
        var cells=parseTableCells(ln);
        if(!cells || cells.length!==header.length)break;
        rows.push(cells);
        idx++;
      }
      if(rows.length===0)return null;

      var table='<table><thead><tr>';
      for(var hi=0;hi<header.length;hi++){
        var hStyle=' style="text-align:'+aligns[hi]+'"';
        table+='<th'+hStyle+'>'+renderInlineMarkdownSafe(header[hi])+'</th>';
      }
      table+='</tr></thead><tbody>';
      for(var ri=0;ri<rows.length;ri++){
        table+='<tr>';
        for(var ti=0;ti<rows[ri].length;ti++){
          var dStyle=' style="text-align:'+aligns[ti]+'"';
          table+='<td'+dStyle+'>'+renderInlineMarkdownSafe(rows[ri][ti])+'</td>';
        }
        table+='</tr>';
      }
      table+='</tbody></table>';

      return {html:table,nextIndex:idx-1};
    }

    for(var i=0;i<lines.length;i++){
      var line=lines[i];
      var trimmed=line.trim();

      if(trimmed===''){
        flushPara();
        closeLists();
        continue;
      }

      if(/^@@CODEBLOCK\d+@@$/.test(trimmed)){
        flushPara();
        closeLists();
        html.push(trimmed);
        continue;
      }

      var tableParsed=tryParseTable(i);
      if(tableParsed){
        flushPara();
        closeLists();
        html.push(tableParsed.html);
        i=tableParsed.nextIndex;
        continue;
      }

      var heading=trimmed.match(/^(#{1,3})\s+(.*)$/);
      if(heading){
        flushPara();
        closeLists();
        var level=heading[1].length;
        html.push('<h'+level+'>'+renderInlineMarkdownSafe(heading[2])+'</h'+level+'>');
        continue;
      }

      if(/^([-*_])\1\1+$/.test(trimmed.replace(/\s+/g,''))){
        flushPara();
        closeLists();
        html.push('<hr>');
        continue;
      }

      var bq=trimmed.match(/^&gt;\s?(.*)$/);
      if(bq){
        flushPara();
        closeLists();
        var quoteLines=[bq[1]];
        while(i+1<lines.length){
          var nextTrim=lines[i+1].trim();
          var nextMatch=nextTrim.match(/^&gt;\s?(.*)$/);
          if(!nextMatch)break;
          quoteLines.push(nextMatch[1]);
          i++;
        }
        html.push('<blockquote><p>'+renderInlineMarkdownSafe(quoteLines.join('<br>'))+'</p></blockquote>');
        continue;
      }

      var task=trimmed.match(/^[-*]\s+\[([ xX])\]\s+(.*)$/);
      if(task){
        flushPara();
        if(inUL){html.push('</ul>');inUL=false;}
        if(inOL){html.push('</ol>');inOL=false;}
        if(!inTask){html.push('<ul class="task-list">');inTask=true;}
        var checked=(task[1].toLowerCase()==='x')?' checked':'';
        html.push('<li class="task-item"><input type="checkbox" disabled'+checked+'><span>'+renderInlineMarkdownSafe(task[2])+'</span></li>');
        continue;
      }

      var ul=trimmed.match(/^[-*]\s+(.*)$/);
      if(ul){
        flushPara();
        if(inTask){html.push('</ul>');inTask=false;}
        if(inOL){html.push('</ol>');inOL=false;}
        if(!inUL){html.push('<ul>');inUL=true;}
        html.push('<li>'+renderInlineMarkdownSafe(ul[1])+'</li>');
        continue;
      }

      var ol=trimmed.match(/^\d+\.\s+(.*)$/);
      if(ol){
        flushPara();
        if(inTask){html.push('</ul>');inTask=false;}
        if(inUL){html.push('</ul>');inUL=false;}
        if(!inOL){html.push('<ol>');inOL=true;}
        html.push('<li>'+renderInlineMarkdownSafe(ol[1])+'</li>');
        continue;
      }

      para.push(trimmed);
    }

    flushPara();
    closeLists();

    var out=html.join('');
    out=out.replace(/@@CODEBLOCK(\d+)@@/g,function(_,idx){
      var i=parseInt(idx,10);
      return codeBlocks[i];
    });
    return out;
  }

  function appendAssistant(content,toolCalls,thinkingTrace,model){
    var msgs=document.getElementById('chat-msgs');
    var row=document.createElement('div');
    row.className='msg msg-assistant';

    var stack=document.createElement('div');
    stack.className='assistant-stack';

    // Fallback inference in case top-level model is absent on older responses.
    var effectiveModel='';
    if(typeof model==='string' && model.trim()!==''){
      effectiveModel=model.trim();
    }
    if(!effectiveModel && Array.isArray(thinkingTrace)){
      for(var mi=0;mi<thinkingTrace.length;mi++){
        var mstep=thinkingTrace[mi]||{};
        if(typeof mstep.model==='string' && mstep.model.trim()!==''){
          effectiveModel=mstep.model.trim();
          break;
        }
      }
    }
    if(!effectiveModel && Array.isArray(toolCalls)){
      for(var ti=0;ti<toolCalls.length;ti++){
        var tstep=toolCalls[ti]||{};
        if(typeof tstep.model==='string' && tstep.model.trim()!==''){
          effectiveModel=tstep.model.trim();
          break;
        }
      }
    }

    if(effectiveModel){
      var meta=document.createElement('div');
      meta.className='assistant-meta';
      var pill=document.createElement('span');
      pill.className='model-pill';
      pill.textContent='model: '+safeText(effectiveModel);
      meta.appendChild(pill);
      stack.appendChild(meta);
    }

    if(Array.isArray(thinkingTrace) && thinkingTrace.length>0){
      var tlog=document.createElement('div');
      tlog.className='thought-log';
      var ttitle=document.createElement('div');
      ttitle.className='thought-log-title';
      ttitle.textContent='Thinking trace';
      tlog.appendChild(ttitle);
      var hasThoughtSteps=false;

      for(var j=0;j<thinkingTrace.length;j++){
        var step=thinkingTrace[j]||{};
        if(step.phase==='final'){
          continue;
        }
        hasThoughtSteps=true;
        var isThinking=(step.phase==='model_thinking');
        var entry=document.createElement('div');
        entry.className='thought-step'+(isThinking?' thought-step--thinking':'');

        var summary=document.createElement('div');
        summary.className='thought-summary';

        var phase=document.createElement('span');
        phase.className=isThinking?'thought-phase--thinking':'thought-phase';
        phase.textContent=isThinking?'reasoning':safeText(step.phase||'step');
        summary.appendChild(phase);

        if(step.model){
          var smodel=document.createElement('span');
          smodel.className='thought-model';
          smodel.textContent='model: '+safeText(step.model);
          summary.appendChild(smodel);
        }

        if(step.tool){
          var tool=document.createElement('span');
          tool.className='thought-tool';
          tool.textContent='tool: '+safeText(step.tool);
          summary.appendChild(tool);
        }

        if(step.timestamp){
          var ts=document.createElement('span');
          ts.className='thought-time';
          ts.textContent=new Date(step.timestamp).toLocaleTimeString();
          summary.appendChild(ts);
        }

        entry.appendChild(summary);

        var details=document.createElement('details');
        // Auto-expand model reasoning so users see it without extra clicks.
        if(isThinking)details.open=true;
        details.className='tool-details';
        var sum=document.createElement('summary');
        sum.textContent=safeText(step.summary||'Details');
        details.appendChild(sum);
        if(step.details){
          var pre=document.createElement('pre');
          pre.className='tool-payload';
          pre.textContent=safeText(step.details);
          details.appendChild(pre);
        }
        entry.appendChild(details);
        tlog.appendChild(entry);
      }

      if(hasThoughtSteps){
        stack.appendChild(tlog);
      }
    }

    if(Array.isArray(toolCalls) && toolCalls.length>0){
      var log=document.createElement('div');
      log.className='tool-log';
      var title=document.createElement('div');
      title.className='tool-log-title';
      title.textContent='Tool calls';
      log.appendChild(title);

      for(var i=0;i<toolCalls.length;i++){
        var tc=toolCalls[i]||{};
        var call=document.createElement('div');
        call.className='tool-call';

        var summary=document.createElement('div');
        summary.className='tool-summary';
        var name=document.createElement('span');
        name.className='tool-name';
        name.textContent=safeText(tc.tool||'unknown');
        summary.appendChild(name);

        if(tc.model){
          var tmodel=document.createElement('span');
          tmodel.className='thought-model';
          tmodel.textContent='model: '+safeText(tc.model);
          summary.appendChild(tmodel);
        }

        var state=document.createElement('span');
        state.className=(tc.success===false)?'tool-state-fail':'tool-state-ok';
        state.textContent=(tc.success===false)?'error':'ok';
        summary.appendChild(state);

        if(typeof tc.duration_ms==='number'){
          var dur=document.createElement('span');
          dur.className='tool-duration';
          dur.textContent=tc.duration_ms+'ms';
          summary.appendChild(dur);
        }
        call.appendChild(summary);

        var details=document.createElement('details');
        details.className='tool-details';
        var sum=document.createElement('summary');
        sum.textContent='Details';
        details.appendChild(sum);

        if(tc.args){
          var args=document.createElement('pre');
          args.className='tool-payload';
          args.textContent='args:\n'+safeText(tc.args);
          details.appendChild(args);
        }
        if(tc.response){
          var resp=document.createElement('pre');
          resp.className='tool-payload';
          resp.textContent='response:\n'+safeText(tc.response);
          details.appendChild(resp);
        }
        if(tc.error){
          var err=document.createElement('pre');
          err.className='tool-payload';
          err.textContent='error:\n'+safeText(tc.error);
          details.appendChild(err);
        }

        call.appendChild(details);
        log.appendChild(call);
      }

      stack.appendChild(log);
    }

    var bubble=document.createElement('div');
    bubble.className='bubble markdown-bubble';
    var rendered=renderMarkdownSafe(content);
    if(effectiveModel){
      rendered='<div class="assistant-model-inline">Model: '+safeText(effectiveModel)+'</div>'+rendered;
    }
    bubble.innerHTML=rendered;
    stack.appendChild(bubble);

    row.appendChild(stack);
    msgs.appendChild(row);
    msgs.scrollTop=msgs.scrollHeight;
  }

  function uid(){
    return Date.now().toString(36)+Math.random().toString(36).slice(2,8);
  }

  function loadSessions(){
    try{
      var raw=localStorage.getItem(SESSION_KEY);
      sessions=raw?JSON.parse(raw):[];
      if(!Array.isArray(sessions))sessions=[];
    }catch(_){
      sessions=[];
    }
    if(sessions.length===0){
      createSession('New session');
      return;
    }
    activeSessionId=sessions[0].id;
  }

  function saveSessions(){
    localStorage.setItem(SESSION_KEY,JSON.stringify(sessions));
  }

  function getActiveSession(){
    for(var i=0;i<sessions.length;i++){
      if(sessions[i].id===activeSessionId)return sessions[i];
    }
    return null;
  }

  function createSession(title){
    var s={
      id:uid(),
      title:title||'New session',
      created_at:Date.now(),
      updated_at:Date.now(),
      messages:[]
    };
    sessions.unshift(s);
    activeSessionId=s.id;
    saveSessions();
    renderSessionList();
    renderActiveSession();
  }

  function renderSessionList(){
    var root=document.getElementById('chat-sessions');
    root.innerHTML='';
    for(var i=0;i<sessions.length;i++){
      (function(s){
        var item=document.createElement('div');
        item.className='session-item'+(s.id===activeSessionId?' active':'');
        var t=document.createElement('div');
        t.className='session-title';
        t.textContent=s.title||'Untitled session';
        item.appendChild(t);
        var m=document.createElement('div');
        m.className='session-meta';
        m.textContent=new Date(s.updated_at).toLocaleString();
        item.appendChild(m);
        item.addEventListener('click',function(){
          activeSessionId=s.id;
          renderSessionList();
          renderActiveSession();
        });
        root.appendChild(item);
      })(sessions[i]);
    }
  }

  function renderActiveSession(){
    var msgs=document.getElementById('chat-msgs');
    msgs.innerHTML='';
    var s=getActiveSession();
    if(!s)return;
    for(var i=0;i<s.messages.length;i++){
      var msg=s.messages[i];
      if(msg.role==='user')appendMsg('user',msg.content);
      else if(msg.role==='assistant')appendAssistant(msg.content,msg.tool_calls||[],msg.thinking_trace||[],msg.model||'');
      else if(msg.role==='error')appendMsg('error',msg.content);
    }
  }

  function setDisabled(disabled){
    var inp=document.getElementById('chat-input');
    var btn=document.getElementById('send-btn');
    inp.disabled=disabled;
    btn.disabled=disabled;
    btn.textContent=disabled?'…':'Send';
    if(!disabled)inp.focus();
  }

  var typingDiv=null;
  var streamContentText='';
  var suppressStreamContent=false;
  var streamThoughtText='';
  var liveThoughtDeltaPre=null;
  var liveToolRow=null;
  var liveToolLog=null;
  var liveThoughtRow=null;
  var liveThoughtLog=null;
  var awaitingResponse=false;
  var activeChatStream=false;
  var lastToolEventIDSeen=0;
  var lastThoughtEventIDSeen=0;

  function ensureLiveThoughtLog(){
    var msgs=document.getElementById('chat-msgs');
    if(!liveThoughtRow){
      liveThoughtRow=document.createElement('div');
      liveThoughtRow.className='msg msg-assistant';
      var stack=document.createElement('div');
      stack.className='assistant-stack';
      liveThoughtLog=document.createElement('div');
      liveThoughtLog.className='thought-log';
      var title=document.createElement('div');
      title.className='thought-log-title';
      title.textContent='Thinking (live)';
      liveThoughtLog.appendChild(title);
      stack.appendChild(liveThoughtLog);
      liveThoughtRow.appendChild(stack);
    }
    if(!liveThoughtRow.parentNode){
      msgs.appendChild(liveThoughtRow);
    }
    msgs.scrollTop=msgs.scrollHeight;
  }

  function clearLiveThoughtLog(){
    if(liveThoughtRow){
      liveThoughtRow.remove();
    }
    liveThoughtRow=null;
    liveThoughtLog=null;
    liveThoughtDeltaPre=null;
    streamThoughtText='';
  }

  function ensureLiveThoughtDelta(){
    ensureLiveThoughtLog();
    if(!liveThoughtLog)return;
    if(liveThoughtDeltaPre)return;

    var step=document.createElement('div');
    step.className='thought-step thought-step--thinking';

    var summary=document.createElement('div');
    summary.className='thought-summary';
    var phase=document.createElement('span');
    phase.className='thought-phase--thinking';
    phase.textContent='reasoning';
    summary.appendChild(phase);
    step.appendChild(summary);

    liveThoughtDeltaPre=document.createElement('pre');
    liveThoughtDeltaPre.className='tool-payload';
    liveThoughtDeltaPre.textContent='';
    step.appendChild(liveThoughtDeltaPre);

    liveThoughtLog.appendChild(step);
  }

  function appendStreamThoughtDelta(delta){
    if(!delta)return;
    ensureLiveThoughtDelta();
    streamThoughtText+=delta;
    if(liveThoughtDeltaPre){
      liveThoughtDeltaPre.textContent=streamThoughtText;
    }
    var msgs=document.getElementById('chat-msgs');
    msgs.scrollTop=msgs.scrollHeight;
  }

  function appendStreamContentDelta(delta){
    if(!delta)return;
    if(suppressStreamContent)return;
    if(streamContentText===''){
    var trimmed=String(delta||'').replace(/^\s+/,'');
    if(trimmed.indexOf('{')===0 || trimmed.indexOf('[')===0 || trimmed.charCodeAt(0)===96){
      suppressStreamContent=true;
      return;
    }
    }
    streamContentText+=delta;
    if(!typingDiv){
      typingDiv=appendMsg('typing','');
    }
    var bubble=typingDiv.querySelector('.bubble');
    if(bubble){
      bubble.style.fontStyle='normal';
      bubble.textContent=streamContentText;
    }
    var msgs=document.getElementById('chat-msgs');
    msgs.scrollTop=msgs.scrollHeight;
  }

  function appendLiveThoughtEvent(ev){
	if(ev && ev.phase==='final'){
	  return;
	}
    if(!liveThoughtLog)return;
    var isThinking=(ev.phase==='model_thinking');
    var step=document.createElement('div');
    step.className='thought-step'+(isThinking?' thought-step--thinking':'');

    var summary=document.createElement('div');
    summary.className='thought-summary';

    var phase=document.createElement('span');
    phase.className=isThinking?'thought-phase--thinking':'thought-phase';
    phase.textContent=isThinking?'reasoning':safeText(ev.phase||'step');
    summary.appendChild(phase);

    if(ev.tool){
      var tool=document.createElement('span');
      tool.className='thought-tool';
      tool.textContent='tool: '+safeText(ev.tool);
      summary.appendChild(tool);
    }

    if(ev.timestamp){
      var ts=document.createElement('span');
      ts.className='thought-time';
      ts.textContent=new Date(ev.timestamp).toLocaleTimeString();
      summary.appendChild(ts);
    }
    step.appendChild(summary);

    var details=document.createElement('details');
    // Auto-expand model reasoning so users see it live without clicking.
    if(isThinking)details.open=true;
    details.className='tool-details';
    var sum=document.createElement('summary');
    sum.textContent=safeText(ev.summary||'Thought');
    details.appendChild(sum);

    if(ev.details){
      var pre=document.createElement('pre');
      pre.className='tool-payload';
      pre.textContent=safeText(ev.details);
      details.appendChild(pre);
    }
    step.appendChild(details);

    liveThoughtLog.appendChild(step);
    var msgs=document.getElementById('chat-msgs');
    msgs.scrollTop=msgs.scrollHeight;
  }

  function ensureLiveToolLog(){
    var msgs=document.getElementById('chat-msgs');
    if(!liveToolRow){
      liveToolRow=document.createElement('div');
      liveToolRow.className='msg msg-assistant';
      var stack=document.createElement('div');
      stack.className='assistant-stack';
      liveToolLog=document.createElement('div');
      liveToolLog.className='tool-log';
      var title=document.createElement('div');
      title.className='tool-log-title';
      title.textContent='Tool calls (live)';
      liveToolLog.appendChild(title);
      stack.appendChild(liveToolLog);
      liveToolRow.appendChild(stack);
    }
    if(!liveToolRow.parentNode){
      msgs.appendChild(liveToolRow);
    }
    msgs.scrollTop=msgs.scrollHeight;
  }

  function clearLiveToolLog(){
    if(liveToolRow){
      liveToolRow.remove();
    }
    liveToolRow=null;
    liveToolLog=null;
  }

  function appendLiveToolEvent(ev){
  if(ev && ev.phase==='start'){
    streamContentText='';
    suppressStreamContent=false;
    if(typingDiv){
    var typingBubble=typingDiv.querySelector('.bubble');
    if(typingBubble)typingBubble.textContent='';
    }
  }
	if(!liveToolLog){
	  ensureLiveToolLog();
	}
    if(!liveToolLog)return;
    var call=document.createElement('div');
    call.className='tool-call';

    var summary=document.createElement('div');
    summary.className='tool-summary';

    var name=document.createElement('span');
    name.className='tool-name';
    name.textContent=safeText(ev.tool||'unknown');
    summary.appendChild(name);

    var state=document.createElement('span');
    if(ev.phase==='start'){
      state.className='tool-state-ok';
      state.textContent='running';
    }else if(ev.success===false){
      state.className='tool-state-fail';
      state.textContent='error';
    }else{
      state.className='tool-state-ok';
      state.textContent='ok';
    }
    summary.appendChild(state);

    if(typeof ev.duration_ms==='number' && ev.duration_ms>0){
      var dur=document.createElement('span');
      dur.className='tool-duration';
      dur.textContent=ev.duration_ms+'ms';
      summary.appendChild(dur);
    }

    call.appendChild(summary);

    var details=document.createElement('details');
    details.className='tool-details';
    var sum=document.createElement('summary');
    sum.textContent='Details';
    details.appendChild(sum);

    var payload=document.createElement('pre');
    payload.className='tool-payload';
    var ts=safeText(ev.timestamp);
    var phase=safeText(ev.phase||'unknown');
    var text='phase: '+phase+'\n'+'timestamp: '+ts;
    if(ev.error){
      text+='\nerror: '+safeText(ev.error);
    }
    payload.textContent=text;
    details.appendChild(payload);
    call.appendChild(details);

    liveToolLog.appendChild(call);
    var msgs=document.getElementById('chat-msgs');
    msgs.scrollTop=msgs.scrollHeight;
  }

  function showTyping(){
    typingDiv=appendMsg('typing','Agent is thinking…');
  }
  function clearTyping(){
    if(typingDiv){typingDiv.remove();typingDiv=null;}
  }

  document.getElementById('chat-form').addEventListener('submit',function(e){
    e.preventDefault();
    var inp=document.getElementById('chat-input');
    var text=inp.value.trim();
    if(!text)return;
    inp.value='';
    inp.style.height='auto';
    sendMessage(text);
  });

  document.getElementById('chat-input').addEventListener('keydown',function(e){
    if(e.key==='Enter'&&!e.shiftKey){
      e.preventDefault();
      document.getElementById('chat-form').dispatchEvent(new Event('submit'));
    }
  });

  document.getElementById('chat-input').addEventListener('input',function(){
    this.style.height='auto';
    this.style.height=Math.min(this.scrollHeight,120)+'px';
  });

  async function sendMessage(input){
    var s=getActiveSession();
    if(!s){
      createSession('New session');
      s=getActiveSession();
    }

    var snapshot=[];
    for(var i=0;i<s.messages.length;i++){
      if(s.messages[i].role==='user' || s.messages[i].role==='assistant'){
        snapshot.push({role:s.messages[i].role,content:s.messages[i].content});
      }
    }

    appendMsg('user',input);
    s.messages.push({role:'user',content:input});
    if(s.messages.length>MAX)s.messages=s.messages.slice(s.messages.length-MAX);
    if(!s.title || s.title==='New session'){
      s.title=input.slice(0,42);
    }
    s.updated_at=Date.now();
    saveSessions();
    renderSessionList();

    setDisabled(true);
    awaitingResponse=true;
    activeChatStream=true;
    streamContentText='';
      suppressStreamContent=false;
    streamThoughtText='';
    ensureLiveThoughtLog();
    showTyping();
    try{
      var s=getActiveSession();
      var res=await fetch('/chat/send?stream=1',{
        method:'POST',
        headers:{'Content-Type':'application/json','Accept':'text/event-stream'},
        body:JSON.stringify({input:input,history:snapshot,session_id:s?s.id:''})
      });

      var data=null;
      var ctype=(res.headers.get('content-type')||'').toLowerCase();
      if(ctype.indexOf('text/event-stream')>=0 && res.body && res.body.getReader){
        var reader=res.body.getReader();
        var decoder=new TextDecoder();
        var buf='';
        var finalSeen=false;

        var processFrame=function(frame){
          if(!frame)return;
          var lines=frame.split('\n');
          var payload=[];
          for(var li=0;li<lines.length;li++){
            var line=lines[li];
            if(line.indexOf('data:')===0){
              payload.push(line.slice(5).trim());
            }
          }
          if(payload.length===0)return;
          var ev=null;
          try{ev=JSON.parse(payload.join('\n'));}catch(_){return;}

          if(ev.type==='tool_event' && ev.event){
            var tid=Number(ev.event.id||0);
            if(tid>lastToolEventIDSeen){
              appendLiveToolEvent(ev.event);
              lastToolEventIDSeen=tid;
            }
            return;
          }
          if(ev.type==='thought_event' && ev.event){
            var hid=Number(ev.event.id||0);
            if(hid>lastThoughtEventIDSeen){
              appendLiveThoughtEvent(ev.event);
              lastThoughtEventIDSeen=hid;
            }
            return;
          }
          if(ev.type==='error'){
            throw new Error(ev.error||'stream error');
          }
          if(ev.type==='thought_delta'){
            appendStreamThoughtDelta(String(ev.delta||''));
            return;
          }
          if(ev.type==='content_delta'){
            appendStreamContentDelta(String(ev.delta||''));
            return;
          }
          if(ev.type==='final'){
            data=ev.data||{};
            finalSeen=true;
          }
        };

        while(true){
          var part=await reader.read();
          if(part.done)break;
          buf+=decoder.decode(part.value,{stream:true});
          var cut=buf.indexOf('\n\n');
          while(cut>=0){
            var frame=buf.slice(0,cut);
            buf=buf.slice(cut+2);
            processFrame(frame);
            cut=buf.indexOf('\n\n');
          }
        }
        if(buf.trim()!==''){
          processFrame(buf);
        }
        if(!finalSeen){
          throw new Error('stream ended before final response');
        }
      }else{
        data=await res.json();
      }

      clearTyping();
      clearLiveThoughtLog();
      clearLiveToolLog();
      awaitingResponse=false;
      activeChatStream=false;
      if(data.error){
        appendMsg('error','Error: '+data.error);
        s.messages.push({role:'error',content:'Error: '+data.error});
        s.updated_at=Date.now();
        saveSessions();
        renderSessionList();
      }else{
        var content=data.content||'(empty response)';
        var toolCalls=Array.isArray(data.tool_calls)?data.tool_calls:[];
        var thinkingTrace=Array.isArray(data.thinking_trace)?data.thinking_trace:[];
        var model=(typeof data.model==='string')?data.model:'';
        appendAssistant(content,toolCalls,thinkingTrace,model);
        s.messages.push({role:'assistant',content:content,model:model,tool_calls:toolCalls,thinking_trace:thinkingTrace});
        if(s.messages.length>MAX)s.messages=s.messages.slice(s.messages.length-MAX);
        s.updated_at=Date.now();
        saveSessions();
        renderSessionList();
      }
    }catch(e){
      clearTyping();
      clearLiveThoughtLog();
      clearLiveToolLog();
      awaitingResponse=false;
      activeChatStream=false;
      appendMsg('error','Network error: '+e.message);
      s.messages.push({role:'error',content:'Network error: '+e.message});
      s.updated_at=Date.now();
      saveSessions();
      renderSessionList();
    }
    setDisabled(false);
  }

  document.getElementById('new-session-btn').addEventListener('click',function(){
    createSession('New session');
  });

  window.onSSEUpdate=function(d){
    if(!d){
      return;
    }

    if(Array.isArray(d.tool_events) && d.tool_events.length>0){
      var newestTool=lastToolEventIDSeen;
      for(var i=0;i<d.tool_events.length;i++){
        var ev=d.tool_events[i]||{};
        var id=Number(ev.id||0);
        if(id>newestTool){
          appendLiveToolEvent(ev);
          lastToolEventIDSeen=id;
        }
        return;
      }
    }

    if(Array.isArray(d.thought_events) && d.thought_events.length>0){
      var newestThought=lastThoughtEventIDSeen;
      for(var j=0;j<d.thought_events.length;j++){
        var tev=d.thought_events[j]||{};
        var tid=Number(tev.id||0);
        if(tid>newestThought){
          appendLiveThoughtEvent(tev);
          lastThoughtEventIDSeen=tid;
        }
        return;
      }
    }
  };

  loadSessions();
  renderSessionList();
  renderActiveSession();

  document.getElementById('chat-input').focus();
})();
</script>`

// canvasTmpl is the Canvas visual workspace page.
// It shows active agents/workers as cards with their live tool-call feed,
// and an ASCII-art agent graph that updates via SSE.
const canvasTmpl = `
<style>
#canvas-wrap{display:flex;flex-direction:column;gap:1.5rem;padding:1rem 0}
#canvas-header{display:flex;align-items:center;justify-content:space-between}
#canvas-header h2{margin:0;font-size:1.1rem;color:#e6edf3}
#canvas-stats{display:flex;gap:1rem;font-size:.85rem;color:#8b949e}
#canvas-grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(320px,1fr));gap:1rem}
.agent-card{background:#161b22;border:1px solid #30363d;border-radius:8px;padding:1rem;display:flex;flex-direction:column;gap:.5rem}
.agent-card-header{display:flex;align-items:center;justify-content:space-between}
.agent-card-title{font-size:.9rem;font-weight:600;color:#e6edf3;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;max-width:200px}
.agent-card-badge{font-size:.75rem;padding:.15rem .5rem;border-radius:12px;background:#21262d;color:#8b949e}
.agent-card-badge.running{background:#1f4b2e;color:#3fb950}
.agent-card-badge.idle{background:#21262d;color:#8b949e}
.tool-feed{font-size:.8rem;color:#8b949e;background:#0d1117;border-radius:4px;padding:.5rem;min-height:60px;max-height:140px;overflow-y:auto;font-family:monospace}
.tool-feed .tf-entry{padding:.15rem 0;border-bottom:1px dotted #21262d}
.tool-feed .tf-entry:last-child{border-bottom:none}
.tool-feed .tf-tool{color:#58a6ff}
.tool-feed .tf-result{color:#3fb950}
.tool-feed .tf-err{color:#f85149}
.graph-section{background:#161b22;border:1px solid #30363d;border-radius:8px;padding:1rem}
.graph-section h3{margin:0 0 .75rem;font-size:.9rem;color:#e6edf3}
#canvas-graph{font-family:monospace;font-size:.8rem;color:#8b949e;white-space:pre;line-height:1.6;min-height:80px}
#canvas-log{background:#0d1117;border:1px solid #30363d;border-radius:8px;padding:.75rem;font-size:.8rem;font-family:monospace;color:#8b949e;max-height:160px;overflow-y:auto}
.cl-entry{padding:.1rem 0}
.cl-ts{color:#6e7681}
.cl-name{color:#58a6ff}
.cl-tool{color:#d2a8ff}
.cl-ok{color:#3fb950}
.cl-err{color:#f85149}
.empty-state{color:#6e7681;font-size:.85rem;text-align:center;padding:2rem 0}
</style>
<div id="canvas-wrap">
  <div id="canvas-header">
    <h2>&#127981; Canvas — Live Workspace</h2>
    <div id="canvas-stats">
      <span id="cs-agents">Agents</span>
      <span id="cs-vms">VMs</span>
      <span id="cs-skills">Skills</span>
    </div>
  </div>

  <div id="canvas-grid"></div>

  <div class="graph-section">
    <h3>Agent Interaction Graph</h3>
    <div id="canvas-graph">Loading…</div>
  </div>

  <div class="graph-section">
    <h3>Live Tool-Call Log</h3>
    <div id="canvas-log"></div>
  </div>
</div>
<script>
(function(){
  // Seed initial data from server-side render.
  var initialWorkers = {{if .Workers}}{{.Workers | toJSON}}{{else}}[]{{end}};
  var initialSkills  = {{if .Skills}}{{.Skills | toJSON}}{{else}}[]{{end}};

  // Agent state: map of agentId → {id, name, status, tools:[]}
  var agents = {};

  function agentID(w){
    if(typeof w === 'object' && w !== null){
      return w.id || w.worker_id || w.name || JSON.stringify(w);
    }
    return String(w);
  }

  function agentName(w){
    if(typeof w === 'object' && w !== null){
      return w.name || w.task_description || w.id || 'Agent';
    }
    return String(w);
  }

  function agentStatus(w){
    if(typeof w === 'object' && w !== null){
      return w.status || w.state || 'running';
    }
    return 'running';
  }

  // Initialise from server data.
  (Array.isArray(initialWorkers)?initialWorkers:[]).forEach(function(w){
    var id=agentID(w);
    agents[id]={id:id,name:agentName(w),status:agentStatus(w),tools:[]};
  });

  function renderGrid(){
    var grid=document.getElementById('canvas-grid');
    var keys=Object.keys(agents);
    if(keys.length===0){
      grid.innerHTML='<p class="empty-state">No active agents. Start a chat or spawn a worker to see live activity here.</p>';
      return;
    }
    grid.innerHTML='';
    keys.forEach(function(id){
      var a=agents[id];
      var card=document.createElement('div');
      card.className='agent-card';
      card.id='ac-'+id.replace(/[^a-z0-9]/gi,'_');
      var status=a.status||'idle';
      card.innerHTML=
        '<div class="agent-card-header">'+
          '<span class="agent-card-title" title="'+escH(a.name)+'">'+escH(a.name)+'</span>'+
          '<span class="agent-card-badge '+(status==='running'?'running':'idle')+'">'+escH(status)+'</span>'+
        '</div>'+
        '<div class="tool-feed" id="tf-'+escH(id.replace(/[^a-z0-9]/gi,'_'))+'">'+
          (a.tools.length===0?'<span style="color:#6e7681">No tool calls yet…</span>':
            a.tools.slice(-6).map(function(t){
              return '<div class="tf-entry">'+
                '<span class="tf-tool">'+escH(t.tool)+'</span>'+
                (t.ok!==undefined?
                  (t.ok?'<span class="tf-result"> ✓</span>':'<span class="tf-err"> ✗ '+escH(t.err||'')+'</span>')
                :'')+
              '</div>';
            }).join('')
          )+
        '</div>';
      grid.appendChild(card);
    });
  }

  function renderGraph(){
    var el=document.getElementById('canvas-graph');
    var keys=Object.keys(agents);
    if(keys.length===0){el.textContent='(no active agents)';return;}
    var lines=['[host daemon]'];
    keys.forEach(function(id){
      var a=agents[id];
      lines.push('  └─ '+a.name+' ('+( a.status||'idle')+')');
    });
    el.textContent=lines.join('\n');
  }

  function appendLog(ts,name,tool,ok,err){
    var log=document.getElementById('canvas-log');
    var d=document.createElement('div');
    d.className='cl-entry';
    d.innerHTML=
      '<span class="cl-ts">'+escH(ts)+'</span> '+
      '<span class="cl-name">'+escH(name)+'</span>'+
      ' → <span class="cl-tool">'+escH(tool)+'</span> '+
      (ok?'<span class="cl-ok">✓</span>':'<span class="cl-err">✗ '+escH(err||'')+'</span>');
    log.appendChild(d);
    log.scrollTop=log.scrollHeight;
    // Keep log at most 120 entries.
    while(log.children.length>120){log.removeChild(log.firstChild);}
  }

  function escH(s){
    return String(s||'').replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;');
  }

  // ── SSE listener ──────────────────────────────────────────────────────────
  // The /events stream emits {type, data} objects.  We react to:
  //   type=tool_start  — an agent started a tool call
  //   type=tool_end    — an agent completed a tool call
  //   type=worker_*    — worker lifecycle events
  window.onSSEUpdate = function(msg){
    var d=msg.data||{};
    var ts=new Date().toLocaleTimeString();

    if(msg.type==='tool_start'){
      var id=d.agent_id||d.session_id||'host';
      if(!agents[id]){agents[id]={id:id,name:d.agent_name||id,status:'running',tools:[]};}
      agents[id].status='running';
      agents[id].tools.push({tool:d.tool||d.name||'?',ok:undefined});
      renderGrid();
      renderGraph();
      document.getElementById('cs-agents').textContent='Agents: '+Object.keys(agents).length;
    }
    else if(msg.type==='tool_end'){
      var id=d.agent_id||d.session_id||'host';
      if(agents[id]){
        var last=agents[id].tools[agents[id].tools.length-1];
        if(last&&last.tool===(d.tool||d.name||'?')){
          last.ok=!d.error;
          last.err=d.error||'';
        }
        agents[id].status='idle';
        renderGrid();
        renderGraph();
      }
      appendLog(ts,d.agent_name||id,d.tool||d.name||'?',!d.error,d.error||'');
    }
    else if(msg.type==='worker_start'){
      var id=d.worker_id||d.id||'worker';
      agents[id]={id:id,name:d.name||d.task_description||'Worker',status:'running',tools:[]};
      renderGrid();renderGraph();
      document.getElementById('cs-agents').textContent='Agents: '+Object.keys(agents).length;
    }
    else if(msg.type==='worker_end'||msg.type==='worker_stop'){
      var id=d.worker_id||d.id||'worker';
      if(agents[id]){agents[id].status='stopped';}
      renderGrid();renderGraph();
    }
    else if(msg.type==='update'){
      // Periodic batch tick — seed agents from tool_events for initial load.
      var tevs=Array.isArray(msg.data&&msg.data.tool_events)?msg.data.tool_events:(Array.isArray(msg.tool_events)?msg.tool_events:[]);
      tevs.forEach(function(ev){
        var id=ev.session_id||ev.agent_id||'host';
        if(!agents[id]){agents[id]={id:id,name:id,status:'idle',tools:[]};}
        var found=false;
        for(var i=0;i<agents[id].tools.length;i++){
          if(agents[id].tools[i]._evid===ev.id){found=true;break;}
        }
        if(!found&&ev.tool){
          agents[id].tools.push({_evid:ev.id,tool:ev.tool,ok:ev.status!=='error',err:ev.error||''});
          if(agents[id].tools.length>20)agents[id].tools.shift();
        }
      });
      // Update stats bar with live counts from update payload.
      var wkrs=Array.isArray(msg.active_workers)?msg.active_workers:[];
      document.getElementById('cs-agents').textContent='Agents: '+(Object.keys(agents).length||wkrs.length);
      if(wkrs.length>0){
        wkrs.forEach(function(w){
          var wid=w.id||w.worker_id||w.name||JSON.stringify(w);
          if(!agents[wid]){agents[wid]={id:wid,name:w.name||w.task_description||'Worker',status:w.status||'running',tools:[]};}
        });
      }
      renderGrid();renderGraph();
    }
  };

  renderGrid();
  renderGraph();
  if(Object.keys(agents).length===0){
    document.getElementById('canvas-log').innerHTML='<span style="color:#6e7681">Waiting for tool-call events…</span>';
  }
})();
</script>`

const sourceTmpl = `
<style>
  .file-tree{background:#0d1117;border:1px solid #30363d;border-radius:6px;padding:1rem;min-height:400px}
  .tree-item{padding:.3rem .5rem;cursor:pointer;border-radius:4px}
  .tree-item:hover{background:#161b22}
  .tree-item.folder{font-weight:600}
  .code-viewer{background:#0d1117;border:1px solid #30363d;border-radius:6px;padding:1rem;margin-top:1rem;min-height:300px}
  .code-viewer pre{margin:0;white-space:pre-wrap;font-family:monospace;font-size:.85rem}
  .line-numbers{color:#6e7681;padding-right:1rem;border-right:1px solid #30363d;margin-right:1rem;user-select:none}
</style>
<h1>{{.Title}}</h1>
    
{{if .Branches}}
<div class="section">
  <div class="section-header">Branches</div>
  <div style="padding:1rem">
    {{$branches := .Branches}}
    {{if $branches.branches}}
      {{range $branches.branches}}
        <div class="badge">{{.}}</div>
      {{end}}
      <div class="muted" style="margin-top:.5rem">Current: {{$branches.current_branch}}</div>
    {{else}}
      <div class="empty">No branches found</div>
    {{end}}
  </div>
</div>
{{end}}

<div class="file-tree" id="file-tree">
  <div class="empty">Select a repository to browse</div>
</div>

<div class="code-viewer" id="code-viewer" style="display:none">
  <pre id="code-content"></pre>
</div>`

const workspaceTmpl = `
<style>
  .workspace-files{display:grid;gap:1rem;margin-bottom:1rem}
  .file-card{background:#161b22;border:1px solid #30363d;border-radius:6px;padding:1rem}
  .file-header{display:flex;justify-content:space-between;align-items:center;margin-bottom:.5rem}
  .file-name{font-weight:600;color:#e6edf3}
  .editor-area{background:#0d1117;border:1px solid #30363d;border-radius:6px;padding:1rem}
  .editor-area textarea{width:100%;min-height:400px;background:#0d1117;color:#e6edf3;border:1px solid #30363d;border-radius:4px;padding:.5rem;font-family:monospace;font-size:.85rem}
</style>
<h1>{{.Title}}</h1>
<p class="muted">Edit your workspace configuration files (SOUL.md, AGENTS.md, TOOLS.md, *.SKILL.md)</p>

<div class="section">
  <div class="section-header">Core Workspace Files</div>
  <div style="padding:1rem">
    <div class="workspace-files">
      <div class="file-card">
        <div class="file-header">
          <span class="file-name">SOUL.md</span>
          <button onclick="editFile('SOUL.md')">Edit</button>
        </div>
        <div class="muted">Your personal agent configuration</div>
      </div>
      
      <div class="file-card">
        <div class="file-header">
          <span class="file-name">AGENTS.md</span>
          <button onclick="editFile('AGENTS.md')">Edit</button>
        </div>
        <div class="muted">Multi-agent system configuration</div>
      </div>
      
      <div class="file-card">
        <div class="file-header">
          <span class="file-name">TOOLS.md</span>
          <button onclick="editFile('TOOLS.md')">Edit</button>
        </div>
        <div class="muted">Custom tool definitions</div>
      </div>
    </div>
  </div>
</div>

{{if .Files}}
{{$files := .Files}}
{{if $files.files}}
<div class="section">
  <div class="section-header">All Workspace Files</div>
  <div style="padding:1rem">
    <table>
      <thead>
        <tr>
          <th>File</th>
          <th>Size</th>
          <th>Modified</th>
          <th>Actions</th>
        </tr>
      </thead>
      <tbody>
        {{range $files.files}}
        <tr>
          <td>{{.name}}</td>
          <td>{{.size}} bytes</td>
          <td class="muted">{{fmtTime .mod_time}}</td>
          <td><button onclick="editFile('{{.name}}')">Edit</button></td>
        </tr>
        {{end}}
      </tbody>
    </table>
  </div>
</div>
{{end}}
{{end}}

<div id="editor-modal" style="display:none;position:fixed;top:0;left:0;right:0;bottom:0;background:rgba(0,0,0,0.8);z-index:100">
  <div style="max-width:900px;margin:2rem auto;background:#0d1117;border:1px solid #30363d;border-radius:6px">
    <div style="padding:1rem;border-bottom:1px solid #30363d;display:flex;justify-content:space-between">
      <h3 id="editor-title">Edit File</h3>
      <button onclick="closeEditor()">Close</button>
    </div>
    <form id="editor-form" action="/workspace/edit" method="post">
      <input type="hidden" name="filename" id="editor-filename">
      <div class="editor-area">
        <textarea name="content" id="editor-content"></textarea>
      </div>
      <div style="padding:1rem;border-top:1px solid #30363d;display:flex;gap:.5rem">
        <button type="submit" class="approve">Save Changes</button>
        <button type="button" onclick="closeEditor()">Cancel</button>
      </div>
    </form>
  </div>
</div>
<script>
  async function editFile(filename) {
    document.getElementById('editor-title').textContent = 'Edit ' + filename;
    document.getElementById('editor-filename').value = filename;
    
    try {
      const resp = await fetch('/api/workspace/read', {
        method: 'POST',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify({filename: filename})
      });
      const data = await resp.json();
      if (data.success && data.data) {
        const content = typeof data.data === 'string' ? JSON.parse(data.data) : data.data;
        document.getElementById('editor-content').value = content.content || '';
      }
    } catch (e) {
      console.error('Failed to load file:', e);
      document.getElementById('editor-content').value = '';
    }
    
    document.getElementById('editor-modal').style.display = 'block';
  }
  
  function closeEditor() {
    document.getElementById('editor-modal').style.display = 'none';
  }
</script>`

const gitHistoryTmpl = `
<style>
  .commit-list{background:#0d1117;border:1px solid #30363d;border-radius:6px;overflow:hidden}
  .commit-item{padding:.75rem 1rem;border-bottom:1px solid #21262d;display:flex;align-items:flex-start;gap:1rem}
  .commit-item:last-child{border-bottom:none}
  .commit-item:hover{background:#161b22}
  .commit-hash{font-family:monospace;color:#58a6ff;font-size:.85rem}
  .commit-message{color:#e6edf3;font-weight:500;margin-bottom:.25rem}
  .commit-meta{color:#8b949e;font-size:.82rem}
</style>
<h1>{{.Title}}</h1>

{{if .Branches}}
<div class="section">
  <div class="section-header">Branches</div>
  <div style="padding:1rem">
    {{$branches := .Branches}}
    {{if $branches.branches}}
      <div style="display:flex;gap:.5rem;flex-wrap:wrap">
      {{range $branches.branches}}
        <div class="badge">{{.}}</div>
      {{end}}
      </div>
      <div class="muted" style="margin-top:.75rem">Current branch: <strong>{{$branches.current_branch}}</strong></div>
    {{else}}
      <div class="empty">No branches found</div>
    {{end}}
  </div>
</div>
{{end}}

{{if .ProposalID}}
<div class="section">
  <div class="section-header">Commits for proposal-{{.ProposalID}}</div>
  {{if .Commits}}
    {{$commits := .Commits}}
    {{if $commits.commits}}
    <div class="commit-list">
      {{range $commits.commits}}
      <div class="commit-item">
        <div style="flex:1">
          <div class="commit-message">{{.Message}}</div>
          <div class="commit-meta">
            <span class="commit-hash">{{truncate .Hash 12}}</span> &mdash;
            by {{.Author}} &mdash;
            {{fmtTime .Timestamp}}
          </div>
        </div>
        <div>
          <a href="/git/diff?proposal={{$.ProposalID}}" class="nav-link">View Diff</a>
        </div>
      </div>
      {{end}}
    </div>
    {{else}}
      <div style="padding:1rem" class="empty">No commits found for this proposal</div>
    {{end}}
  {{else}}
    <div style="padding:1rem" class="empty">No commits found</div>
  {{end}}
</div>
{{else}}
<div class="section">
  <div class="section-header">Proposal Branches</div>
  <div style="padding:1rem">
    {{if .Branches}}
    {{$branches := .Branches}}
    {{if $branches.branches}}
      <p class="muted">Select a proposal branch to view its commit history:</p>
      <div style="display:flex;flex-direction:column;gap:.5rem;margin-top:1rem">
      {{range $branches.branches}}
        {{if ne . "main"}}
        <div>
          <a href="/git?proposal={{substr . 9}}" class="nav-link">{{.}}</a>
        </div>
        {{end}}
      {{end}}
      </div>
    {{else}}
      <div class="empty">No proposal branches found</div>
    {{end}}
    {{else}}
      <div class="empty">No branches found</div>
    {{end}}
  </div>
</div>
{{end}}`

const gitDiffTmpl = `
<style>
  .diff-container{background:#0d1117;border:1px solid #30363d;border-radius:6px;padding:1rem;font-family:monospace;font-size:.85rem;overflow-x:auto}
  .diff-line{white-space:pre;line-height:1.5}
  .diff-add{background:#1a3a1a;color:#3fb950}
  .diff-del{background:#3a1a1a;color:#f85149}
  .diff-header{color:#58a6ff;font-weight:600}
  .diff-meta{color:#8b949e}
</style>
<h1>{{.Title}}</h1>

<div style="margin-bottom:1rem">
  <a href="/git?proposal={{.ProposalID}}" class="nav-link">← Back to Commit History</a>
</div>

{{if .Error}}
<div class="section">
  <div style="padding:1rem;color:#f85149">Error: {{.Error}}</div>
</div>
{{else if .Diff}}
<div class="section">
  <div class="section-header">Changes (main → proposal-{{.ProposalID}})</div>
  {{$diff := .Diff}}
  {{if $diff.diff}}
  <div class="diff-container">
    <pre id="diff-content">{{$diff.diff}}</pre>
  </div>
  {{else}}
    <div style="padding:1rem" class="empty">No changes found</div>
  {{end}}
</div>
{{else}}
<div class="section">
  <div style="padding:1rem" class="empty">No diff available</div>
</div>
{{end}}
<script>
  // Syntax highlighting for diff
  const diffContent = document.getElementById('diff-content');
  if (diffContent) {
    const lines = diffContent.textContent.split('\n');
    const highlighted = lines.map(line => {
      if (line.startsWith('+')) {
        return '<span class="diff-line diff-add">' + escapeHtml(line) + '</span>';
      } else if (line.startsWith('-')) {
        return '<span class="diff-line diff-del">' + escapeHtml(line) + '</span>';
      } else if (line.startsWith('@@')) {
        return '<span class="diff-line diff-meta">' + escapeHtml(line) + '</span>';
      } else if (line.startsWith('diff ') || line.startsWith('index ') || line.startsWith('---') || line.startsWith('+++')) {
        return '<span class="diff-line diff-header">' + escapeHtml(line) + '</span>';
      } else {
        return '<span class="diff-line">' + escapeHtml(line) + '</span>';
      }
    }).join('\n');
    diffContent.innerHTML = highlighted;
  }
  
  function escapeHtml(text) {
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
  }
</script>`

// handlePRList renders the Pull Requests overview page.
func (s *Server) handlePRList(w http.ResponseWriter, r *http.Request) {
	s.renderTemplate(w, "Pull Requests", prListTmpl, nil)
}

// handlePRDetail renders a single PR detail page.
func (s *Server) handlePRDetail(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Redirect(w, r, "/pullrequests", http.StatusSeeOther)
		return
	}
	data := map[string]interface{}{"ID": id}
	s.renderTemplate(w, "PR Detail", prDetailTmpl, data)
}

// prListTmpl is the HTMX-powered PR list page.
const prListTmpl = `
<div class="section">
  <h2>PR Stats</h2>
  <div hx-get="/api/dashboard/pr/stats" hx-trigger="load" hx-swap="innerHTML" class="stats-grid">
    Loading stats...
  </div>
</div>
<div class="section">
  <div class="section-header">
    Open PRs
  </div>
  <div id="open-prs" hx-get="/api/dashboard/pr/list?status=open" hx-trigger="load, every 10s" hx-swap="innerHTML">
    Loading open PRs...
  </div>
</div>
<div class="section">
  <div class="section-header">
    All PRs
  </div>
  <div id="all-prs" hx-get="/api/dashboard/pr/list" hx-trigger="load" hx-swap="innerHTML">
    Loading all PRs...
  </div>
</div>
`

// prDetailTmpl is the HTMX-powered PR detail page.
const prDetailTmpl = `
<div hx-get="/api/dashboard/pr/detail?id={{.ID}}" hx-trigger="load" hx-swap="innerHTML">
  Loading PR details...
</div>
`
