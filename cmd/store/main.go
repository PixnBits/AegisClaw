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
	"os/exec"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
)

// NOTE (7.1 real secrets): When the Store grows commands that push secret
// material (e.g. "secrets.push" or rotation), import
// "AegisClaw/internal/boundarycrypto" and use
// boundarycrypto.BuildEncryptedSecretsUpdatePayload(...) to create the
// encrypted payload, then sign the Message exactly like other privileged
// commands. The Boundary will decrypt + zeroize on receipt.
// See internal/boundarycrypto/encrypt.go for the AES-256-GCM implementation.

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

func loadAuditFromFile(filename string) []interface{} {
	var data []interface{}
	file, err := os.Open(filename)
	if err != nil {
		return data
	}
	defer file.Close()
	json.NewDecoder(file).Decode(&data)
	return data
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

func saveAuditToFile(filename string, data []interface{}) {
	bytes, _ := json.Marshal(data)
	ioutil.WriteFile(filename, bytes, 0644)
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
	// Generate keys
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	pubStr := base64.StdEncoding.EncodeToString(pub)

	socket := expandPath(hubSocket)
	conn, err := net.Dial("unix", socket)
	if err != nil {
		log.Fatal("Failed to connect to AegisHub:", err)
	}
	defer conn.Close()

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

	// Simple storage with persistence
	proposals := loadFromFile("proposals.json")
	skills := loadFromFile("skills.json")
	auditLog := loadAuditFromFile("audit.json")
	memories := loadFromFile("memories.json")
	prs := loadFromFile("prs.json")
	teams := loadFromFile("teams.json")

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
			Timestamp:   "2026-05-09T19:40:01Z",
			Signature:   "",
		}

		mu.Lock()
		switch msg.Command {
		// Phase 2.1a: Reconciliation is now real and authoritative in Store VM
		case "reconcile.expired_grants":
			expiredAutonomy := ReconcileExpiredAutonomy()
			expiredBackground := ReconcileExpiredBackgroundWork()

			// Also reconcile general scheduled timers (Phase 2 timer infrastructure)
			expiredTimers := reconcileExpiredTimers()

			response.Command = "reconcile.done"
			response.Payload = map[string]interface{}{
				"autonomy_expired":   expiredAutonomy,
				"background_expired": expiredBackground,
				"timers_expired":     expiredTimers,
				"note":               "authoritative reconciliation from Store VM (Phase 2)",
			}

		case "timer.schedule":
			payload := msg.Payload.(map[string]interface{})
			id := payload["id"].(string)
			// Store full metadata (session_id, preset, expires, signature, etc.)
			ScheduleTimer(id, payload)
			response.Command = "timer.scheduled"
			response.Payload = map[string]interface{}{"id": id}

		case "timer.cancel":
			payload := msg.Payload.(map[string]interface{})
			id := payload["id"].(string)
			CancelTimer(id)
			response.Command = "timer.cancelled"
			response.Payload = map[string]interface{}{"id": id}

		case "timer.list":
			// Phase 2.6 enhancement: return full timer records (not just IDs) so
			// callers (CLI surfaces, future components) can see session_id, expires,
			// preset etc. without extra roundtrips. Backward-compatible in spirit
			// (previous []string callers can be updated; we control the main ones).
			timers := loadTimers()
			list := []interface{}{}
			for id, t := range timers {
				if tm, ok := t.(map[string]interface{}); ok {
					tmCopy := make(map[string]interface{})
					for k, v := range tm {
						tmCopy[k] = v
					}
					tmCopy["id"] = id // ensure id is present
					list = append(list, tmCopy)
				}
			}
			response.Command = "timer.list"
			response.Payload = list

		// Phase 2: Record an autonomy grant in the Store (source of truth for durable grants)
		// Per store-vm.md durable state ownership + event-system.md persistent timers.
		case "autonomy.grant":
			payload := msg.Payload.(map[string]interface{})
			sessionID := payload["session_id"].(string)
			grants := loadGrants()
			grantRecord := map[string]interface{}{
				"session_id": sessionID,
				"preset":     payload["preset"],
				"expires":    payload["expires"],
				"granted_at": response.Timestamp,
			}
			if scopes, ok := payload["scopes"]; ok {
				grantRecord["scopes"] = scopes
			}
			grants[sessionID] = grantRecord
			saveGrants(grants)
			response.Command = "autonomy.granted"
			response.Payload = map[string]interface{}{"session_id": sessionID}

		// Phase 2.6: Read commands so CLI surfaces can source authoritative current
		// grant state from the Store instead of (or in addition to) local sessions.json.
		// This is the key step that allows progressive removal of thin local grant
		// display + expiration logic. Citations: store-vm.md (Store owns durable
		// structured data), event-system.md (Store as source for persistent timer/grant state).
		case "grant.list":
			grants := loadGrants()
			list := []interface{}{}
			for _, g := range grants {
				list = append(list, g)
			}
			response.Command = "grant.list"
			response.Payload = list

		case "grant.get":
			payload := msg.Payload.(map[string]interface{})
			sessionID := payload["session_id"].(string)
			grants := loadGrants()
			response.Command = "grant.get"
			if g, ok := grants[sessionID]; ok {
				response.Payload = g
			} else {
				response.Payload = nil
			}
		case "proposal.create":
			payload := msg.Payload.(map[string]interface{})
			id := payload["id"].(string)
			payload["state"] = "pending"
			payload["reviews"] = make(map[string]string)
			proposals[id] = payload
			saveToFile("proposals.json", proposals)
			// Notify scribe
			scribeMsg := Message{
				Source:      "store",
				Destination: "court-scribe",
				Command:     "scribe.notify_review",
				Payload:     map[string]interface{}{"proposal_id": id},
				Timestamp:   response.Timestamp,
				Signature:   "",
			}
			signMessage(&scribeMsg, priv)
			encoder.Encode(scribeMsg)
			response.Command = "proposal.created"
			response.Payload = "ok"
		case "proposal.get":
			payload := msg.Payload.(map[string]interface{})
			id := payload["id"].(string)
			response.Command = "proposal.data"
			response.Payload = proposals[id]
		case "proposal.list":
			list := []interface{}{}
			for _, p := range proposals {
				list = append(list, p)
			}
			response.Command = "proposal.list"
			response.Payload = list
		case "proposal.update":
			payload := msg.Payload.(map[string]interface{})
			id := payload["id"].(string)
			if p, ok := proposals[id].(map[string]interface{}); ok {
				for k, v := range payload {
					if k != "id" {
						p[k] = v
					}
				}
				proposals[id] = p
				saveToFile("proposals.json", proposals)
				response.Command = "proposal.updated"
				response.Payload = "ok"
			} else {
				response.Command = "error"
				response.Payload = "proposal not found"
			}
		case "court.review_complete":
			payload := msg.Payload.(map[string]interface{})
			id := payload["proposal_id"].(string)
			votes := payload["votes"].(map[string]interface{})
			if p, ok := proposals[id].(map[string]interface{}); ok {
				p["reviews"] = votes

				// Phase 3: Persist the full tamper-evident signed decision from Scribe
				// (includes decision_merkle + decision_sig per court-scribe.md + governance-court.md)
				if _, hasMerkle := payload["decision_merkle"]; hasMerkle {
					p["court_decision"] = payload // the complete signed record
				}

				approved := false
				if a, ok := payload["approved"].(bool); ok {
					approved = a
				} else {
					approved = true // fallback for old payloads
				}
				if approved {
					p["state"] = "approved"
					// Wire to Builder: trigger implementation after Court approval (per builder-vm.md + Phase 4 plan)
					builderMsg := Message{
						Source:      "store",
						Destination: "builder",
						Command:     "builder.build_proposal",
						Payload:     map[string]interface{}{"proposal_id": id},
						Timestamp:   response.Timestamp,
						Signature:   "",
					}
					signMessage(&builderMsg, priv)
					encoder.Encode(builderMsg)
				} else {
					p["state"] = "rejected"
				}
				proposals[id] = p
				saveToFile("proposals.json", proposals)
				response.Command = "court.review_recorded"
				response.Payload = "ok"
			}
		case "court.get_reviews":
			payload := msg.Payload.(map[string]interface{})
			id := payload["id"].(string)
			if p, ok := proposals[id].(map[string]interface{}); ok {
				response.Command = "court.reviews"
				// Phase 3: Return the full signed decision (Merkle + sig) when available for real audit/exposure
				if cd, has := p["court_decision"]; has && cd != nil {
					response.Payload = cd
				} else {
					response.Payload = p["reviews"]
				}
			} else {
				response.Command = "error"
				response.Payload = "proposal not found"
			}

		// Phase 3: Record enforcement actions coming from Court decisions (revoke scopes, terminate agents).
		// This is the Store as the single source of truth for active revocations (store-vm.md).
		case "court.record_enforcement":
			payload := msg.Payload.(map[string]interface{})
			proposalID := ""
			if p, ok := payload["proposal_id"].(string); ok {
				proposalID = p
			}
			action := fmt.Sprintf("%v", payload["action"])
			scopes, _ := payload["revoked_scopes"].([]interface{})
			agentID, _ := payload["agent_id"].(string)

			enforcement := map[string]interface{}{
				"proposal_id":    proposalID,
				"action":         action,
				"revoked_scopes": scopes,
				"agent_id":       agentID,
				"timestamp":      response.Timestamp,
			}

			// Store under a simple revocations key for now (real impl would have a dedicated revocations collection)
			if revocations == nil {
				revocations = make(map[string]interface{})
			}
			revocations[proposalID+"-"+action] = enforcement

			// If this is a termination, the orchestrator/daemon is expected to act on "court.terminate" events.
			response.Command = "court.enforcement_recorded"
			response.Payload = enforcement
		case "git.clone":
			payload := msg.Payload.(map[string]interface{})
			repo := payload["repo"].(string)
			path := "repos/" + repo
			os.MkdirAll("repos", 0755)
			cmd := exec.Command("git", "init", "--bare", path)
			err := cmd.Run()
			if err != nil {
				response.Command = "error"
				response.Payload = err.Error()
			} else {
				response.Command = "git.cloned"
				response.Payload = "ok"
			}
		case "git.push":
			// For push, assume it's handled by git, stub success
			response.Command = "git.pushed"
			response.Payload = "ok"
		case "pr.create":
			payload := msg.Payload.(map[string]interface{})
			id := payload["id"].(string)
			prs[id] = payload
			saveToFile("prs.json", prs)
			response.Command = "pr.created"
			response.Payload = "ok"
		case "pr.update":
			payload := msg.Payload.(map[string]interface{})
			id := payload["id"].(string)
			if p, ok := prs[id].(map[string]interface{}); ok {
				for k, v := range payload {
					if k != "id" {
						p[k] = v
					}
				}
				prs[id] = p
				saveToFile("prs.json", prs)
				response.Command = "pr.updated"
				response.Payload = "ok"
			} else {
				response.Command = "error"
				response.Payload = "pr not found"
			}
		case "pr.get":
			payload := msg.Payload.(map[string]interface{})
			id := payload["id"].(string)
			response.Command = "pr.data"
			response.Payload = prs[id]

		// === Teams (minimal stub for Phase 5 Teams plan slice) ===
		case "team.create":
			payload := msg.Payload.(map[string]interface{})
			id := payload["id"].(string)
			if _, ok := payload["created_at"]; !ok {
				payload["created_at"] = response.Timestamp
			}
			payload["members"] = payload["members"] // may be nil
			payload["messages"] = []interface{}{}
			teams[id] = payload
			saveToFile("teams.json", teams)
			response.Command = "team.created"
			response.Payload = map[string]interface{}{"id": id}
		case "team.list":
			list := []interface{}{}
			for _, t := range teams {
				list = append(list, t)
			}
			response.Command = "team.list"
			response.Payload = list
		case "team.get":
			payload := msg.Payload.(map[string]interface{})
			id := payload["id"].(string)
			response.Command = "team.data"
			response.Payload = teams[id]
		case "team.message":
			payload := msg.Payload.(map[string]interface{})
			teamID := payload["team_id"].(string)
			if t, ok := teams[teamID].(map[string]interface{}); ok {
				if msgs, ok := t["messages"].([]interface{}); ok {
					msgEntry := map[string]interface{}{
						"ts":      response.Timestamp,
						"from":    payload["from"],
						"to":      payload["to"], // role or "broadcast"
						"content": payload["content"],
				}
				t["messages"] = append(msgs, msgEntry)
				}
				teams[teamID] = t
				saveToFile("teams.json", teams)
			}
			response.Command = "team.message.sent"
			response.Payload = "ok"
		case "skill.register":
			payload := msg.Payload.(map[string]interface{})
			id := payload["id"].(string)
			skills[id] = payload
			saveToFile("skills.json", skills)
			response.Command = "skill.registered"
			response.Payload = "ok"
		case "skill.get":
			payload := msg.Payload.(map[string]interface{})
			id := payload["id"].(string)
			response.Command = "skill.data"
			response.Payload = skills[id]

		case "build.complete":
			payload := msg.Payload.(map[string]interface{})
			id := payload["proposal_id"].(string)
			if p, ok := proposals[id].(map[string]interface{}); ok {
				p["state"] = "built"
				p["build_status"] = "success"
				proposals[id] = p
				saveToFile("proposals.json", proposals)
				// On success, register the skill (closing the loop from Builder)
				skill := map[string]interface{}{
					"id":          id,
					"name":        "Skill from " + id,
					"description": p["description"],
				}
				skills[id] = skill
				saveToFile("skills.json", skills)
			}
			response.Command = "build.recorded"
			response.Payload = "ok"
		case "build.failed":
			payload := msg.Payload.(map[string]interface{})
			id := payload["proposal_id"].(string)
			report := payload["report"]
			if p, ok := proposals[id].(map[string]interface{}); ok {
				p["state"] = "build_failed"
				p["build_report"] = report // non-leaking
				proposals[id] = p
				saveToFile("proposals.json", proposals)
			}
			response.Command = "build.recorded"
			response.Payload = "ok"
		case "skill.list":
			list := []interface{}{}
			for _, s := range skills {
				list = append(list, s)
			}
			response.Command = "skill.list"
			response.Payload = list
		case "memory.store":
			payload := msg.Payload.(map[string]interface{})
			memories[payload["content"].(string)] = payload
			saveToFile("memories.json", memories)
			response.Command = "memory.stored"
			response.Payload = "ok"
		case "memory.query":
			// Stub
			response.Command = "memory.results"
			response.Payload = []interface{}{}
		case "audit.append":
			auditLog = append(auditLog, msg.Payload)
			saveAuditToFile("audit.json", auditLog)
			response.Command = "audit.appended"
			response.Payload = "ok"
		case "audit.get_root":
			root := computeMerkleRoot(auditLog)
			response.Command = "audit.root"
			response.Payload = root
		case "audit.list":
			response.Command = "audit.list"
			response.Payload = auditLog
		case "tool.list":
			response.Command = "tool.list"
			response.Payload = skills
		case "ping":
			response.Command = "pong"
			response.Payload = "ok"
		case "version", "get-version":
			if msg.Command == "get-version" {
				// For get-version from hub, send proper Message response back
				response.Command = "version"
				response.Source = "store"
				response.Destination = msg.Source
				response.Payload = map[string]string{"version": getBuildVersion()}
				// Don't continue - let normal flow sign and send
			} else {
				response.Command = "version"
				response.Payload = map[string]string{"version": getBuildVersion()}
			}
		default:
			response.Command = "error"
			response.Payload = "unknown command"
		}
		mu.Unlock()

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

		// Phase 2 enhancement: every Store response is signed with its private key
		// so AegisHub can verify it (consistent with per-VM key model).
		signMessage(&response, priv)

		// Tamper-evident Merkle audit log: record state changes.
		// In a full impl this would be the canonical Store-owned audit trail.
		if strings.HasPrefix(msg.Command, "proposal.") ||
			msg.Command == "court.review_complete" ||
			msg.Command == "pr.create" ||
			msg.Command == "skill.register" ||
			msg.Command == "memory.store" {
			entry := map[string]interface{}{
				"ts":      response.Timestamp,
				"command": msg.Command,
				"source":  msg.Source,
			}
			auditLog = append(auditLog, entry)
			root := computeMerkleRoot(auditLog)
			// Attach latest root so clients (Court, Web Portal) can see it
			if m, ok := response.Payload.(map[string]interface{}); ok {
				m["merkle_root"] = root
			} else {
				response.Payload = map[string]interface{}{
					"result":       response.Payload,
					"merkle_root":  root,
				}
			}
			saveAuditToFile("audit.json", auditLog)
		}

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
