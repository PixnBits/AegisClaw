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
	memories, _ := s.fetchRaw(r.Context(), "memory.list", map[string]interface{}{"limit": 1, "count_only": true})

	workerCount := countItems(workers)
	approvalCount := countItems(approvals)
	timerCount := countItems(timers)

	var memCount int
	if m, ok := memories.(map[string]interface{}); ok {
		if c, ok := m["total"].(float64); ok {
			memCount = int(c)
		}
	}

	s.renderTemplate(w, "Overview", overviewTmpl, map[string]interface{}{
		"WorkerCount":   workerCount,
		"ApprovalCount": approvalCount,
		"TimerCount":    timerCount,
		"MemoryCount":   memCount,
		"Workers":       workers,
		"Approvals":     approvals,
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
	if query != "" {
		memories, _ = s.fetchRaw(r.Context(), "memory.search", map[string]interface{}{"query": query, "k": 20})
	} else {
		memories, _ = s.fetchRaw(r.Context(), "memory.list", map[string]interface{}{"limit": 50})
	}
	s.renderTemplate(w, "Memory Vault", memoryTmpl, map[string]interface{}{
		"Memories": memories,
		"Query":    query,
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
	ticker := time.NewTicker(5 * time.Second)
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
			payload, _ := json.Marshal(map[string]interface{}{
				"type":              "update",
				"active_workers":    workers,
				"pending_approvals": approvals,
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
	if err != nil || resp == nil || !resp.Success {
		return nil, err
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
  <span id="sse-status">&#9679;</span>
</nav>`

const dashboardSSEScript = `
<script>
(function(){
  const s=document.getElementById('sse-status');
  try{
    const es=new EventSource('/events');
    es.onopen=()=>{s.textContent='&#9679; live';s.style.color='#3fb950'};
    es.onerror=()=>{s.textContent='&#9679; disconnected';s.style.color='#f85149'};
    es.onmessage=(e)=>{const d=JSON.parse(e.data);if(d.type==='update'&&window.onSSEUpdate)window.onSSEUpdate(d)};
  }catch(e){s.textContent='&#9679; no sse'}
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
  {{if .Memories}}
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
</div>

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
