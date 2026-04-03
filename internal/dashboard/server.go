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
	s.mux.HandleFunc("/settings", s.handleSettings)
	s.mux.HandleFunc("/chat", s.handleChat)
	s.mux.HandleFunc("/chat/send", s.handleChatSend)
	s.mux.HandleFunc("/events", s.handleSSE)
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

	workerCount := countItems(workers)
	approvalCount := countItems(approvals)
	timerCount := countItems(timers)
	runningVMCount := countItems(sandboxes)
	runningVMVCPUs, runningVMMemoryMB := sandboxResourceTotals(sandboxes)

	var memCount int
	if m, ok := memories.(map[string]interface{}); ok {
		if c, ok := m["total"].(float64); ok {
			memCount = int(c)
		}
	}

	s.renderTemplate(w, "Overview", overviewTmpl, map[string]interface{}{
		"WorkerCount":       workerCount,
		"ApprovalCount":     approvalCount,
		"TimerCount":        timerCount,
		"MemoryCount":       memCount,
		"RunningVMCount":    runningVMCount,
		"RunningVMVCPUs":    runningVMVCPUs,
		"RunningVMMemoryMB": runningVMMemoryMB,
		"RunningVMs":        sandboxes,
		"Workers":           workers,
		"Approvals":         approvals,
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
	proposals, _ := s.fetchRaw(r.Context(), "list_proposals", nil)
	skills, _ := s.fetchRaw(r.Context(), "list_skills", nil)
	s.renderTemplate(w, "Skills & Proposals", skillsTmpl, map[string]interface{}{
		"Proposals": proposals,
		"Skills":    skills,
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

func (s *Server) handleChatSend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 512<<10) // 512 KB limit
	var req struct {
		Input   string `json:"input"`
		History []struct {
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
	payload := mustMarshal(map[string]interface{}{
		"input":   req.Input,
		"history": req.History,
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

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			workers, _ := s.fetchRaw(ctx, "worker.list", map[string]bool{"active_only": true})
			approvals, _ := s.fetchRaw(ctx, "event.approvals.list", map[string]bool{"pending_only": true})
			toolEvents, _ := s.fetchRaw(ctx, "chat.tool_events", map[string]int{"limit": 40})
			payload, _ := json.Marshal(map[string]interface{}{
				"type":              "update",
				"active_workers":    workers,
				"pending_approvals": approvals,
				"tool_events":       toolEvents,
				"ts":                time.Now().UTC().Format(time.RFC3339),
			})
			fmt.Fprintf(w, "data: %s\n\n", payload)
			flusher.Flush()
		}
	}
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

func sandboxResourceTotals(v interface{}) (vcpus int64, memoryMB int64) {
	if v == nil {
		return 0, 0
	}
	list, ok := v.([]interface{})
	if !ok {
		return 0, 0
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
	}
	return vcpus, memoryMB
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
.badge-fired,.badge-cancelled{background:#21262d;color:#8b949e}
.empty{color:#8b949e;font-style:italic;padding:2rem;text-align:center}
.section{background:#161b22;border:1px solid #30363d;border-radius:6px;margin-bottom:1.5rem;overflow:hidden}
.section-header{padding:.75rem 1rem;border-bottom:1px solid #30363d;font-weight:600;font-size:.9rem;color:#e6edf3}
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
.typing .bubble{color:#8b949e;font-style:italic}
.assistant-stack{display:flex;flex-direction:column;gap:.45rem;max-width:80%}
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
  <a href="/agents">Agents</a>
  <a href="/skills">Skills</a>
  <a href="/async">Async Hub</a>
  <a href="/memory">Memory</a>
  <a href="/approvals">Approvals</a>
  <a href="/audit">Audit</a>
  <a href="/settings">Settings</a>
  <a href="/chat">Chat</a>
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
    <div style="font-size:.85rem;color:#8b949e;margin-top:.25rem">Allocated VM Memory</div>
  </div>
</div>

{{if .RunningVMs}}
<div class="section">
  <div class="section-header">Running MicroVMs</div>
  <table>
    <thead><tr><th>Name</th><th>ID</th><th>State</th><th>vCPUs</th><th>Memory</th></tr></thead>
    <tbody>
    {{range .RunningVMs}}
    <tr>
      <td><strong>{{index . "name"}}</strong></td>
      <td><code>{{truncate (index . "id") 12}}</code></td>
      <td><span class="badge badge-running">{{index . "state"}}</span></td>
      <td>{{index . "vcpus"}}</td>
      <td>{{index . "memory_mb"}} MB</td>
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
  <div class="section-header">Active Skills</div>
  {{if .Skills}}
  <table>
    <thead><tr><th>Name</th><th>Version</th><th>Status</th></tr></thead>
    <tbody>
    {{range .Skills}}
    <tr>
      <td><strong>{{index . "name"}}</strong></td>
      <td>{{index . "version"}}</td>
      <td><span class="badge badge-active">active</span></td>
    </tr>
    {{end}}
    </tbody>
  </table>
  {{else}}
  <p class="empty">No skills activated yet. Use <code>aegisclaw skill add</code> to create one.</p>
  {{end}}
</div>
<div class="section">
  <div class="section-header">Proposals</div>
  {{if .Proposals}}
  <table>
    <thead><tr><th>ID</th><th>Title</th><th>Status</th><th>Category</th></tr></thead>
    <tbody>
    {{range .Proposals}}
    <tr>
      <td><code>{{truncate (index . "id") 8}}</code></td>
      <td>{{truncate (index . "title") 60}}</td>
      <td><span class="badge badge-{{index . "status"}}">{{index . "status"}}</span></td>
      <td>{{index . "category"}}</td>
    </tr>
    {{end}}
    </tbody>
  </table>
  {{else}}
  <p class="empty">No proposals yet. Submit a skill proposal via <code>aegisclaw skill add</code>.</p>
  {{end}}
</div>`

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

  function appendAssistant(content,toolCalls){
    var msgs=document.getElementById('chat-msgs');
    var row=document.createElement('div');
    row.className='msg msg-assistant';

    var stack=document.createElement('div');
    stack.className='assistant-stack';

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
    bubble.className='bubble';
    bubble.textContent=safeText(content);
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
      else if(msg.role==='assistant')appendAssistant(msg.content,msg.tool_calls||[]);
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
  var liveToolRow=null;
  var liveToolLog=null;
  var awaitingResponse=false;
  var lastToolEventIDSeen=0;

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
    ensureLiveToolLog();
    showTyping();
    try{
      var res=await fetch('/chat/send',{
        method:'POST',
        headers:{'Content-Type':'application/json'},
        body:JSON.stringify({input:input,history:snapshot})
      });
      var data=await res.json();
      clearTyping();
      clearLiveToolLog();
      awaitingResponse=false;
      if(data.error){
        appendMsg('error','Error: '+data.error);
        s.messages.push({role:'error',content:'Error: '+data.error});
        s.updated_at=Date.now();
        saveSessions();
        renderSessionList();
      }else{
        var content=data.content||'(empty response)';
        var toolCalls=Array.isArray(data.tool_calls)?data.tool_calls:[];
        appendAssistant(content,toolCalls);
        s.messages.push({role:'assistant',content:content,tool_calls:toolCalls});
        if(s.messages.length>MAX)s.messages=s.messages.slice(s.messages.length-MAX);
        s.updated_at=Date.now();
        saveSessions();
        renderSessionList();
      }
    }catch(e){
      clearTyping();
      clearLiveToolLog();
      awaitingResponse=false;
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
    if(!d || !Array.isArray(d.tool_events) || d.tool_events.length===0){
      return;
    }

    var newest=lastToolEventIDSeen;
    for(var i=0;i<d.tool_events.length;i++){
      var ev=d.tool_events[i]||{};
      var id=Number(ev.id||0);
      if(id>newest)newest=id;
      if(!awaitingResponse)continue;
      if(id<=lastToolEventIDSeen)continue;
      appendLiveToolEvent(ev);
    }
    lastToolEventIDSeen=newest;
  };

  loadSessions();
  renderSessionList();
  renderActiveSession();

  document.getElementById('chat-input').focus();
})();
</script>`
