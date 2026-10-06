package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"os"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"AegisClaw/internal/bootargs"
	"AegisClaw/internal/boundarycrypto"
	"AegisClaw/internal/channeldata"
	"AegisClaw/internal/channelfacilitator"
	"AegisClaw/internal/chatstore"
	"AegisClaw/internal/collab"
	"AegisClaw/internal/timing"
	"AegisClaw/internal/transport/hubclient"

	"github.com/mdlayher/vsock"
	"github.com/spf13/cobra"
)

// Phase 4 (Real Encrypted Secrets):
// The Store VM is the sole producer of encrypted secret blobs (per
// secret-management.md §Architecture + §Key Guarantees and
// network-boundary.md).
//
// We import boundarycrypto here to produce AES-256-GCM encrypted blobs
// signed for the Boundary. This replaces all legacy file/dir/env secret
// distribution.
//
// See internal/boundarycrypto/encrypt.go (BuildEncryptedSecretsUpdatePayload,
// EncryptSecretsBlob, Zero* helpers) for the implementation with full
// citations.

type Message struct {
	Source      string      `json:"source"`
	Destination string      `json:"destination"`
	Command     string      `json:"command"`
	Payload     interface{} `json:"payload"`
	Timestamp   string      `json:"timestamp"`
	Signature   string      `json:"signature"`
}

var hubSocket = "~/.aegis/hub.sock"

// revocations holds active Court enforcement actions (revoked scopes, terminations).
// In a fuller Store VM this would be durable + queryable (store-vm.md).
var revocations = make(map[string]interface{})

func init() {
	if env := os.Getenv("AEGIS_HUB_SOCKET"); env != "" {
		hubSocket = env
	}
}

func expandPath(path string) string {
	if path[:2] == "~/" {
		home, _ := os.UserHomeDir()
		return home + path[1:]
	}
	return path
}

func loadFromFile(filename string) map[string]interface{} {
	data := make(map[string]interface{})
	file, err := os.Open(filename)
	if err != nil {
		return data
	}
	defer file.Close()
	json.NewDecoder(file).Decode(&data)
	return data
}

func saveToFile(filename string, data interface{}) {
	bytes, _ := json.Marshal(data)
	ioutil.WriteFile(filename, bytes, 0644)
}

func intFromPayload(v interface{}) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}

// channelIDFromPayload reads id, then channel_id.
// A missing key and JSON null are empty. A non-string is an error.
// A nil payload is an error (indexing it would panic).
func channelIDFromPayload(payload map[string]interface{}) (string, error) {
	if payload == nil {
		return "", fmt.Errorf("payload must be an object")
	}
	id, err := optString(payload, "id")
	if err != nil || id != "" {
		return id, err
	}
	return optString(payload, "channel_id")
}

func prepareChannelRecord(ch map[string]interface{}) {
	channeldata.BackfillMessageSeqs(ch)
	for _, m := range channeldata.MembersSlice(ch) {
		channeldata.EnsureMemberDefaults(m)
	}
}

func membersToInterface(members []map[string]interface{}) []interface{} {
	out := make([]interface{}, len(members))
	for i, m := range members {
		out[i] = m
	}
	return out
}

func messageMatchesFilter(m map[string]interface{}, filter map[string]interface{}) bool {
	if filter == nil {
		return true
	}
	if author, ok := filter["author"].(string); ok && author != "" {
		if channeldata.MessageFrom(m) != author {
			return false
		}
	}
	if kwRaw, ok := filter["keywords"].([]interface{}); ok && len(kwRaw) > 0 {
		content := strings.ToLower(channeldata.MessageContent(m))
		for _, k := range kwRaw {
			kw, _ := k.(string)
			if kw != "" && !strings.Contains(content, strings.ToLower(kw)) {
				return false
			}
		}
	}
	return true
}

func emitChannelUpdated(encoder *json.Encoder, priv ed25519.PrivateKey, ts, chID, from, content string, seq int) {
	payload := map[string]interface{}{
		"channel_id": chID,
		"from":       from,
		"content":    content,
		"seq":        seq,
	}
	for _, dest := range []string{"daemon-orchestrator", channelfacilitator.ComponentID} {
		collab.Tracef("store", "channel.updated", "ch=%s from=%s dest=%s seq=%d", chID, from, dest, seq)
		updateMsg := Message{
			Source:      "store",
			Destination: dest,
			Command:     channelfacilitator.CmdUpdated,
			Payload:     payload,
			Timestamp:   ts,
			Signature:   "",
		}
		signMessage(&updateMsg, priv)
		_ = encoder.Encode(updateMsg)
	}
}

func decodeChatMessages(raw []interface{}) []chatstore.Message {
	out := make([]chatstore.Message, 0, len(raw))
	for _, item := range raw {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		msg := chatstore.Message{}
		if role, ok := m["role"].(string); ok {
			msg.Role = role
		}
		if content, ok := m["content"].(string); ok {
			msg.Content = content
		}
		if model, ok := m["model"].(string); ok {
			msg.Model = model
		}
		if tc, ok := m["tool_calls"]; ok {
			msg.ToolCalls, _ = json.Marshal(tc)
		}
		if tt, ok := m["thinking_trace"]; ok {
			msg.ThinkingTrace, _ = json.Marshal(tt)
		}
		out = append(out, msg)
	}
	return out
}

// === Phase 2.1a: Durable autonomy & background grant storage (0600) ===
// These will become the authoritative source for timer reconciliation.

func loadGrants() map[string]interface{} {
	data := make(map[string]interface{})
	file, err := os.Open("grants.json")
	if err != nil {
		return data
	}
	defer file.Close()
	json.NewDecoder(file).Decode(&data)
	return data
}

func saveGrants(data interface{}) {
	bytes, _ := json.MarshalIndent(data, "", "  ")
	os.WriteFile("grants.json", bytes, 0600)
}

// canCreateProposal implements minimal permission gate for proposal.create.
// Satisfies: "Respect current permission grants and visibility policies", "a microVM or low-privilege
// subject must not be able to create proposals without the proper grant", "no self-grant".
// Host/daemon/CLI/test "client" sources are privileged. Others require grant entry in grants.json
// allowing "proposal.create" (or broad "*").
func canCreateProposal(source string, _ map[string]interface{}) error {
	if source == "" {
		return fmt.Errorf("ERR_PERMISSION_DENIED: empty source")
	}
	// privileged host/cli/daemon/portal and test harness clients (preserve existing /api/proposals and web-portal paths)
	if source == "daemon-internal" ||
		strings.HasPrefix(source, "aegis-cli-internal") ||
		source == "client" ||
		source == "store" ||
		source == "web-portal" ||
		strings.HasPrefix(source, "web-portal") ||
		strings.HasPrefix(source, "daemon-orchestrator") ||
		strings.HasPrefix(source, "aegis-daemon") {
		return nil
	}
	// low-priv (e.g. agent-*, builder-*, microvm sources) must have explicit grant
	grants := loadGrants()
	candidates := []string{source, "proposal", "skills.propose", "proposals", "*"}
	for _, key := range candidates {
		if g, ok := grants[key]; ok && g != nil {
			if gm, ok := g.(map[string]interface{}); ok {
				if scopesIface, has := gm["scopes"]; has {
					if scopes, ok := scopesIface.([]interface{}); ok {
						for _, s := range scopes {
							if sv, ok := s.(string); ok {
								if sv == "proposal.create" || sv == "skills.propose" || sv == "*" || sv == "proposals" {
									return nil
								}
							}
						}
					}
				}
				if allowed, ok := gm["allowed"].(string); ok && (strings.Contains(allowed, "proposal") || strings.Contains(allowed, "*")) {
					return nil
				}
			}
			// no return here: if grant record but no explicit proposal.* scope/allowed, fall out of if and try next candidate or ERR
		}
	}
	return fmt.Errorf("ERR_PERMISSION_DENIED: source %s lacks grant for proposal.create (microVMs/agents cannot self-grant)", source)
}

// performProposalCreate holds the real post-permission-check logic for creating a proposal
// (state, save, scribe.notify_review, response payload). The main switch and tests both call it
// so tests drive the shipped handler code + side effects (saveToFile + encoder.Encode for scribe).
func performProposalCreate(id string, payload map[string]interface{}, proposals map[string]interface{}, encoder *json.Encoder, priv ed25519.PrivateKey, ts string) (respPayload interface{}, scribeSent bool) {
	payload["state"] = "pending"
	payload["reviews"] = make(map[string]string)
	proposals[id] = payload
	saveToFile("proposals.json", proposals)

	scribeMsg := Message{
		Source:      "store",
		Destination: "court-scribe",
		Command:     "scribe.notify_review",
		Payload:     map[string]interface{}{"proposal_id": id},
		Timestamp:   ts,
		Signature:   "",
	}
	signMessage(&scribeMsg, priv)
	if encoder != nil {
		_ = encoder.Encode(scribeMsg)
		scribeSent = true
	}
	return map[string]interface{}{"proposal_id": id}, scribeSent
}

// isPermissionAuditReadCommand reports store read RPCs that must not mutate response
// payloads with merkle audit wrappers (breaks Hub signature verify + Portal parsing).
func isPermissionAuditReadCommand(command string) bool {
	switch command {
	case "permission.list", "permission.snapshot", "permission.panel", "permission.check",
		"permission.requests.list", "visibility.list", "visibility.get",
		"ciso.delegation.get", "tool.registry.discover", "audit.list":
		return true
	default:
		return false
	}
}

// appendAuditForStateChangeIfNeeded is the *exact* post-switch audit block logic (shipped code).
// It is called from the main loop after every message and can be called directly by tests
// to exercise the real append + save + merkle attachment for proposal.* (including denied cases where
// response.Command=="error" but msg.Command still starts with "proposal.").
func appendAuditForStateChangeIfNeeded(msg Message, response *Message, auditLog *[]interface{}) {
	if isPermissionAuditReadCommand(msg.Command) {
		return
	}
	if strings.HasPrefix(msg.Command, "proposal.") ||
		msg.Command == "court.review_complete" ||
		msg.Command == "pr.create" ||
		msg.Command == "skill.register" ||
		msg.Command == "memory.store" ||
		strings.HasPrefix(msg.Command, "permission.") ||
		strings.HasPrefix(msg.Command, "visibility.") {
		entry := map[string]interface{}{
			"ts":      response.Timestamp,
			"command": msg.Command,
			"source":  msg.Source,
		}
		*auditLog = append(*auditLog, entry)
		root := computeMerkleRoot(*auditLog)
		// Attach latest root so clients (Court, Web Portal) can see it
		if m, ok := response.Payload.(map[string]interface{}); ok {
			m["merkle_root"] = root
		} else if msg.Command != "proposal.list" && msg.Command != "proposal.get" {
			response.Payload = map[string]interface{}{
				"result":      response.Payload,
				"merkle_root": root,
			}
		}
		persistAudit(*auditLog, msg)
	}
}

// handleProposalCreate is the single orchestrator containing the *entire* proposal.create
// case logic (safe payload parse, canCreateProposal gate with fixed grant check,
// perform or ERR response).
// NOTE: it does NOT append audit; the caller (post-switch in the loop or tests) does via
// appendAuditForStateChangeIfNeeded. The switch case now delegates to it; unit tests call
// it directly with in-memory state (no subprocesses). This ensures tests drive the exact
// shipped handler path for happy/denied.
func handleProposalCreate(msg Message, proposals map[string]interface{}, encoder *json.Encoder, priv ed25519.PrivateKey, auditLog *[]interface{}, ts string) Message {
	resp := Message{
		Command:   "",
		Payload:   nil,
		Timestamp: ts,
	}

	// Safe parse (same as current case)
	payload, ok := msg.Payload.(map[string]interface{})
	if !ok || payload == nil {
		resp.Command = "error"
		resp.Payload = "ERR_BAD_PAYLOAD: proposal.create expects object payload with id"
		// NOTE: do NOT append here; the post-switch appendAuditForStateChangeIfNeeded in the loop will do single append for proposal.* msgs
		return resp
	}

	// Gate (uses the fixed canCreate)
	if err := canCreateProposal(msg.Source, payload); err != nil {
		resp.Command = "error"
		resp.Payload = err.Error()
		return resp
	}

	idIface, hasID := payload["id"]
	id, idOK := idIface.(string)
	if !hasID || !idOK || id == "" {
		resp.Command = "error"
		resp.Payload = "ERR_BAD_PAYLOAD: proposal.create requires non-empty string id"
		return resp
	}

	// Perform the create (save + scribe)
	respPayload, _ := performProposalCreate(id, payload, proposals, encoder, priv, ts)
	resp.Command = "proposal.created"
	resp.Payload = respPayload

	// NOTE: audit append is done once by the post-switch code in the main loop for all proposal.* (including this response)
	// We deliberately do not call appendAuditForStateChangeIfNeeded here to avoid double entries + double merkle.

	return resp
}

func loadBackgroundWork() map[string]interface{} {
	data := make(map[string]interface{})
	file, err := os.Open("background.json")
	if err != nil {
		return data
	}
	defer file.Close()
	json.NewDecoder(file).Decode(&data)
	return data
}

func saveBackgroundWork(data interface{}) {
	bytes, _ := json.MarshalIndent(data, "", "  ")
	os.WriteFile("background.json", bytes, 0600)
}

// === Phase 2: General-purpose durable timers (store-vm.md + event-system.md) ===

func loadTimers() map[string]interface{} {
	data := make(map[string]interface{})
	file, err := os.Open("timers.json")
	if err != nil {
		return data
	}
	defer file.Close()
	json.NewDecoder(file).Decode(&data)
	return data
}

func saveTimers(data interface{}) {
	bytes, _ := json.MarshalIndent(data, "", "  ")
	os.WriteFile("timers.json", bytes, 0600)
}

// ScheduleTimer stores a durable timer record.
// Metadata includes session_id, preset/scope, expiration (RFC3339), and signature for auditability.
func ScheduleTimer(id string, metadata map[string]interface{}) error {
	timers := loadTimers()
	if metadata == nil {
		metadata = make(map[string]interface{})
	}
	metadata["scheduled_at"] = time.Now().UTC().Format(time.RFC3339)
	timers[id] = metadata
	saveTimers(timers)
	return nil
}

func CancelTimer(id string) {
	timers := loadTimers()
	delete(timers, id)
	saveTimers(timers)
}

func ListActiveTimers() []string {
	timers := loadTimers()
	ids := make([]string, 0, len(timers))
	for id := range timers {
		ids = append(ids, id)
	}
	return ids
}

func computeMerkleRoot(log []interface{}) string {
	if len(log) == 0 {
		return ""
	}
	data, _ := json.Marshal(log)
	hash := sha256.Sum256(data)
	return base64.StdEncoding.EncodeToString(hash[:])
}

func signMessage(msg *Message, priv ed25519.PrivateKey) {
	msgCopy := *msg
	msgCopy.Signature = ""
	data, _ := json.Marshal(msgCopy)
	signature := ed25519.Sign(priv, data)
	msg.Signature = base64.StdEncoding.EncodeToString(signature)
}

// createEncryptedSecretBlobPayload is the Phase 4 Store-side helper (Group 1).
// It uses boundarycrypto.BuildEncryptedSecretsUpdatePayload (AES-256-GCM +
// the required timestamp/nonce structure) to produce the payload for a
// signed "secrets.push" or "secrets.update" message.
//
// SPEC: secret-management.md §Key Guarantees (Store produces blobs; Boundary
// is the only decryptor) + network-boundary.md (encrypted blobs over Hub).
//
// The caller (future secrets.push handler) is responsible for signing the
// full Message and sending it to the Boundary via the Hub.
//
// symKey is the 32-byte shared secret between Store and this Boundary instance.
// In production this will come from secure out-of-band distribution or
// future attested registration.
func createEncryptedSecretBlobPayload(secrets map[string]string, symKey []byte, extra map[string]interface{}) (map[string]interface{}, error) {
	if len(symKey) != 32 {
		return nil, fmt.Errorf("store: secrets symmetric key must be 32 bytes for AES-256-GCM (Phase 4)")
	}
	return boundarycrypto.BuildEncryptedSecretsUpdatePayload(secrets, symKey, extra)
}

func getBuildVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		version := info.Main.Version
		if version == "" || version == "(devel)" {
			// Use commit hash if available
			for _, setting := range info.Settings {
				if setting.Key == "vcs.revision" && len(setting.Value) >= 7 {
					return setting.Value[:7] // Short commit hash
				}
			}
			return "dev"
		}
		return version
	}
	return "unknown"
}

// ReconcileExpiredAutonomy is the authoritative implementation now living in the Store VM
// (per store-vm.md + event-system.md). It operates on durable grants.json (0600).
func ReconcileExpiredAutonomy() []string {
	grants := loadGrants()
	var expired []string
	now := time.Now().UTC().Format(time.RFC3339)

	for id, v := range grants {
		if g, ok := v.(map[string]interface{}); ok {
			if exp, has := g["expires"]; has {
				if expStr, ok := exp.(string); ok {
					if expStr < now {
						expired = append(expired, id)
						delete(grants, id)
					}
				}
			}
		}
	}

	if len(expired) > 0 {
		saveGrants(grants)
	}
	return expired
}

// ReconcileExpiredBackgroundWork is the second authoritative implementation in Store.
func ReconcileExpiredBackgroundWork() []string {
	bg := loadBackgroundWork()
	var expired []string
	now := time.Now().UTC().Format(time.RFC3339)

	for id, v := range bg {
		if b, ok := v.(map[string]interface{}); ok {
			if exp, has := b["expires"]; has {
				if expStr, ok := exp.(string); ok {
					if expStr < now {
						expired = append(expired, id)
						delete(bg, id)
					}
				}
			}
		}
	}

	if len(expired) > 0 {
		saveBackgroundWork(bg)
	}
	return expired
}

// reconcileExpiredTimers handles general scheduled timers stored via ScheduleTimer.
func reconcileExpiredTimers() []string {
	timers := loadTimers()
	var expired []string
	now := time.Now().UTC().Format(time.RFC3339)

	for id, v := range timers {
		if t, ok := v.(map[string]interface{}); ok {
			if exp, has := t["expires"]; has {
				if expStr, ok := exp.(string); ok {
					if expStr < now {
						expired = append(expired, id)
						delete(timers, id)
					}
				}
			}
		}
	}

	if len(expired) > 0 {
		saveTimers(timers)
	}
	return expired
}

// publishExpirationEvent sends a signed event.publish message to the Hub for
// Store-owned timer/expiration events. This fulfills event-system.md:
// "Persistent timers are stored in Store VM" and "Persistent timers (cron-like)
// are managed by Store VM + Event System". Examples: autonomy.expired,
// background.expired, timer.fired.<id>.
// Called from both the Hub command path and the autonomous ticker loop.
func publishExpirationEvent(encoder *json.Encoder, priv ed25519.PrivateKey, timestamp string, eventName string, payload map[string]interface{}) {
	eventMsg := Message{
		Source:      "store",
		Destination: "hub",
		Command:     "event.publish",
		Payload: map[string]interface{}{
			"event":   eventName,
			"payload": payload,
		},
		Timestamp: timestamp,
		Signature: "",
	}
	signMessage(&eventMsg, priv)
	// Best-effort; ignore encode error in autonomous path (consistent with existing pattern)
	_ = encoder.Encode(eventMsg)
}

func runStore(cmd *cobra.Command, args []string) {
	timing.RecordPhase("main_entry")

	// Generate keys
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	pubStr := base64.StdEncoding.EncodeToString(pub)
	timing.RecordPhase("key_generated_dev") // store currently always gens; Load path not used yet

	socket := expandPath(hubSocket)
	conn, err := net.Dial("unix", socket)
	if err != nil {
		if bootargs.UseHubVsock() {
			fmt.Printf("store: waiting for host hub bridge on vsock :%d (Firecracker inverted path)\n", hubclient.GuestHubBridgePort)
			conn, err = hubclient.AcceptVsockHubBridgeConn(hubclient.GuestHubBridgePort)
		} else if vconn, verr := vsock.Dial(vsock.Host, hubclient.HubVsockPort, nil); verr == nil {
			conn = vconn
			err = nil
		} else {
			log.Fatal("Failed to connect to AegisHub (unix and vsock):", err, verr)
		}
	}
	if err != nil {
		log.Fatal("Failed to connect to AegisHub:", err)
	}
	defer conn.Close()
	timing.RecordPhase("hub_dialed")

	encoder := json.NewEncoder(conn)
	decoder := json.NewDecoder(conn)

	// Register
	regMsg := Message{
		Source:      "store",
		Destination: "hub",
		Command:     "register",
		Payload: map[string]string{
			"public_key": pubStr,
			"version":    getBuildVersion(),
		},
		Timestamp: "2026-05-09T19:40:00Z",
		Signature: "dummy",
	}
	err = encoder.Encode(regMsg)
	if err != nil {
		log.Fatal("Failed to register:", err)
	}

	// Consume response
	var resp map[string]interface{}
	err = decoder.Decode(&resp)
	if err != nil {
		log.Fatal("Failed to decode register response:", err)
	}
	if error, ok := resp["error"]; ok {
		log.Fatal("Registration failed:", error)
	}
	fmt.Println("Store VM registered")
	timing.RecordPhase("register_complete")
	timing.WriteComponentReadySentinel()

	// Simple storage with persistence
	proposals := loadFromFile("proposals.json")
	skills := loadFromFile("skills.json")
	auditLog := loadAuditLog()
	memories := loadFromFile("memories.json")
	prs := loadFromFile("prs.json")
	teams := loadFromFile("teams.json")
	chatSessions := chatstore.New("chat-sessions.json")
	channels := loadFromFile("channels.json")
	initPermissionState()

	// Phase 2.1a + 2.3 recovery (store-vm.md + event-system.md):
	// Explicitly load ALL durable timer/ grant state at startup.
	// "Persistent timers are stored in Store VM" (event-system.md).
	// We perform an immediate catch-up reconciliation of anything that
	// expired while the Store VM was down. This is the concrete implementation
	// of "Timers survive daemon and Store VM restarts" (Phase 2 DoD).
	// Because reconciliation is a full scan of the 0600 JSON files on every
	// 30s ticker signal (or on-demand via reconcile.expired_grants), there is
	// no separate "re-arm heap" — loading + one boot-time reconcile + the
	// running ticker is the recovery model. Any non-expired timers remain in
	// timers.json and will be caught on the next post-restart signal.
	grants := loadGrants()
	background := loadBackgroundWork()
	timers := loadTimers()
	_ = grants
	_ = background
	_ = timers

	// Immediate boot-time catch-up (before entering the message loop).
	// Any expirations missed during downtime are processed and their events
	// published exactly as during normal autonomous operation.
	expiredBootA := ReconcileExpiredAutonomy()
	expiredBootB := ReconcileExpiredBackgroundWork()
	expiredBootT := reconcileExpiredTimers()
	for _, sid := range expiredBootA {
		publishExpirationEvent(encoder, priv, "2026-05-27T00:00:00Z", "autonomy.expired", map[string]interface{}{
			"session_id": sid,
			"reason":     "store_startup_recovery",
		})
	}
	for _, sid := range expiredBootB {
		publishExpirationEvent(encoder, priv, "2026-05-27T00:00:00Z", "background.expired", map[string]interface{}{
			"session_id": sid,
			"reason":     "store_startup_recovery",
		})
	}
	for _, id := range expiredBootT {
		publishExpirationEvent(encoder, priv, "2026-05-27T00:00:00Z", "timer.fired", map[string]interface{}{
			"timer_id": id,
			"reason":   "store_startup_recovery",
		})
	}
	if len(expiredBootA)+len(expiredBootB)+len(expiredBootT) > 0 {
		fmt.Printf("Store startup recovery: processed %d autonomy, %d background, %d timers that expired while offline\n",
			len(expiredBootA), len(expiredBootB), len(expiredBootT))
	}

	var mu sync.Mutex

	// Phase 2.1c: Channel used by the internal timer goroutine to signal
	// that periodic reconciliation should run. The main loop drains it
	// non-blockingly so we never block on timer events.
	reconcileCh := make(chan struct{}, 1)

	// Hard-coded autonomous timer loop inside the Store VM (as specified
	// in phase-2.md 2.1 and store-vm.md for persistent timers).
	// This makes the Store the true owner of timer reconciliation,
	// independent of any daemon in-process EventBus ticks.
	go func() {
		ticker := time.NewTicker(30 * time.Second) // simple hard-coded interval for this phase
		defer ticker.Stop()
		for range ticker.C {
			select {
			case reconcileCh <- struct{}{}:
			default:
				// A reconciliation is already pending; skip this tick
			}
		}
	}()

	gitSrv, gitErr := startStoreGitServer()
	if gitErr != nil {
		log.Printf("store git remote socket: %v", gitErr)
	} else {
		defer gitSrv.Close()
	}

	// Store loop
	for {
		var msg Message
		err := decoder.Decode(&msg)
		if err != nil {
			log.Println("Decode error:", err)
			continue
		}

		fmt.Println("Store received:", msg.Command)

		response := Message{
			Source:      "store",
			Destination: msg.Source,
			Timestamp:   time.Now().UTC().Format(time.RFC3339),
			Signature:   "",
		}

		w := &storeWorld{
			proposals:    proposals,
			skills:       skills,
			memories:     memories,
			prs:          prs,
			teams:        teams,
			channels:     channels,
			auditLog:     &auditLog,
			chatSessions: chatSessions,
			encoder:      encoder,
			priv:         priv,
		}
		mu.Lock()
		// Checked decoding is the fix. dispatchWithPanicGuard only matters if
		// a handler still panics: it logs a SECURITY event, sets an error
		// reply, and returns so mu.Unlock and the sign/encode path still run.
		skipReply := dispatchWithPanicGuard(msg, &response, w)
		mu.Unlock()
		if skipReply {
			continue
		}

		// Phase 2.1c: Drain any pending autonomous reconciliation signal from
		// the internal Store timer goroutine. This is the key step that gives
		// the Store VM independent ownership of persistent timers.
		select {
		case <-reconcileCh:
			expiredA := ReconcileExpiredAutonomy()
			expiredB := ReconcileExpiredBackgroundWork()
			expiredT := reconcileExpiredTimers()
			if len(expiredA) > 0 || len(expiredB) > 0 || len(expiredT) > 0 {
				fmt.Printf("Store timer: auto-reconciled expirations - autonomy=%v background=%v timers=%v\n", expiredA, expiredB, expiredT)

				// Phase 2: Publish expiration events via the Hub so downstream
				// components (Agent Runtimes, etc.) can react without relying on
				// the daemon-local EventBus. This is the Store-driven event path
				// per event-system.md §"Persistent timers are stored in Store VM"
				// and "Persistent timers (cron-like) are managed by Store VM + Event System".
				// timer.fired events use the general form (id in payload) so callers
				// can distinguish scheduled vs grant timers.
				for _, sid := range expiredA {
					publishExpirationEvent(encoder, priv, response.Timestamp, "autonomy.expired", map[string]interface{}{
						"session_id": sid,
						"reason":     "store_timer",
					})
				}
				for _, sid := range expiredB {
					publishExpirationEvent(encoder, priv, response.Timestamp, "background.expired", map[string]interface{}{
						"session_id": sid,
						"reason":     "store_timer",
					})
				}
				for _, id := range expiredT {
					publishExpirationEvent(encoder, priv, response.Timestamp, "timer.fired", map[string]interface{}{
						"timer_id": id,
						"reason":   "store_timer",
					})
				}
			}
		default:
			// No timer signal this cycle
		}

		// Tamper-evident Merkle audit log: record state changes (before signing).
		// In a full impl this would be the canonical Store-owned audit trail.
		// Extracted so unit tests can drive the *exact same block* for denied attempts (proposal.create with error response).
		appendAuditForStateChangeIfNeeded(msg, &response, &auditLog)

		// Phase 2 enhancement: sign after all payload mutations so hub verification succeeds.
		signMessage(&response, priv)

		err = encoder.Encode(response)
		if err != nil {
			log.Println("Failed to send response:", err)
		}
	}
}

func main() {
	var rootCmd = &cobra.Command{
		Use:   "store",
		Short: "Store VM",
		Run:   runStore,
	}

	rootCmd.Execute()
}
