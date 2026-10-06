package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"AegisClaw/internal/channeldata"
	"AegisClaw/internal/channelfacilitator"
	"AegisClaw/internal/chatstore"
	"AegisClaw/internal/collab"
)

// storeWorld is the in-memory Store the hub loop mutates for one process.
type storeWorld struct {
	proposals    map[string]interface{}
	skills       map[string]interface{}
	memories     map[string]interface{}
	prs          map[string]interface{}
	teams        map[string]interface{}
	channels     map[string]interface{}
	auditLog     *[]interface{}
	chatSessions *chatstore.Store
	encoder      *json.Encoder
	priv         ed25519.PrivateKey
}

// dispatchStoreCommand handles one decoded peer message.
// skipReply is true for hub correlation frames and empty commands: the loop
// must not sign or encode those. Checked payload decoding lives in the cases.
// A panic here is not recovered; the hub loop's per-message recover is the backstop.
func dispatchStoreCommand(msg Message, response *Message, w *storeWorld) (skipReply bool) {
	proposals := w.proposals
	skills := w.skills
	memories := w.memories
	prs := w.prs
	teams := w.teams
	channels := w.channels
	chatSessions := w.chatSessions
	encoder := w.encoder
	priv := w.priv
	auditLog := *w.auditLog
	defer func() { *w.auditLog = auditLog }()

	switch msg.Command {
	case "response", "ack", "error":
		// Hub RPC correlation frames (e.g. after store→daemon relay). Not store commands.
		return true
	case "":
		return true
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
		payload, ok := mustPayload(msg, response)
		if !ok {
			break
		}
		id, ok := mustString(msg, response, payload, "id")
		if !ok {
			break
		}
		// Store full metadata (session_id, preset, expires, signature, etc.)
		ScheduleTimer(id, payload)
		response.Command = "timer.scheduled"
		response.Payload = map[string]interface{}{"id": id}

	case "timer.cancel":
		payload, ok := mustPayload(msg, response)
		if !ok {
			break
		}
		id, ok := mustString(msg, response, payload, "id")
		if !ok {
			break
		}
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
		payload, ok := mustPayload(msg, response)
		if !ok {
			break
		}
		sessionID, ok := mustString(msg, response, payload, "session_id")
		if !ok {
			break
		}
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
		payload, ok := mustPayload(msg, response)
		if !ok {
			break
		}
		sessionID, ok := mustString(msg, response, payload, "session_id")
		if !ok {
			break
		}
		grants := loadGrants()
		response.Command = "grant.get"
		if g, ok := grants[sessionID]; ok {
			response.Payload = g
		} else {
			response.Payload = nil
		}
	case "proposal.create":
		// Delegate to the single orchestrator (contains full case logic + audit append).
		// This makes the proposal.create path one entrypoint for the loop and for table-driven tests.
		handled := handleProposalCreate(msg, proposals, encoder, priv, &auditLog, response.Timestamp)
		response.Command = handled.Command
		response.Payload = handled.Payload
	case "proposal.get":
		payload, ok := msg.Payload.(map[string]interface{})
		if !ok || payload == nil {
			response.Command = "error"
			response.Payload = "ERR_BAD_PAYLOAD"
			break
		}
		id, err := optString(payload, "id")
		if err != nil {
			response.Command = "error"
			response.Payload = invalidPayload(msg.Command, err)
			break
		}
		if id == "" {
			response.Command = "error"
			response.Payload = "ERR_BAD_PAYLOAD: proposal.get requires non-empty id"
			break
		}
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
		payload, ok := mustPayload(msg, response)
		if !ok {
			break
		}
		id, ok := mustString(msg, response, payload, "id")
		if !ok {
			break
		}
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
		payload, ok := mustPayload(msg, response)
		if !ok {
			break
		}
		id, ok := mustString(msg, response, payload, "proposal_id")
		if !ok {
			break
		}
		votes, err := reqObject(payload, "votes")
		if err != nil {
			response.Command = "error"
			response.Payload = invalidPayload(msg.Command, err)
			break
		}
		if p, ok := proposals[id].(map[string]interface{}); ok {
			p["reviews"] = votes

			// Phase 3: Persist the full tamper-evident signed decision from Scribe
			// (includes decision_merkle + decision_sig per court-scribe.md + governance-court.md)
			if courtDecisionSigned(payload) {
				p["court_decision"] = payload
			}

			// Omitted approved must not mark the proposal approved/mergeable (no Court skip).
			// approved:true without merkle+sig is not a Court decision (T12).
			if a, ok := payload["approved"].(bool); ok {
				if a {
					if courtDecisionSigned(payload) {
						p["state"] = "approved"
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
					}
				} else {
					p["state"] = "rejected"
				}
			}
			proposals[id] = p
			saveToFile("proposals.json", proposals)
			response.Command = "court.review_recorded"
			response.Payload = "ok"
		}
	case "court.get_reviews":
		payload, ok := mustPayload(msg, response)
		if !ok {
			break
		}
		id, ok := mustString(msg, response, payload, "id")
		if !ok {
			break
		}
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
		payload, ok := mustPayload(msg, response)
		if !ok {
			break
		}
		proposalID, ok := mustOptString(msg, response, payload, "proposal_id")
		if !ok {
			break
		}
		agentID, ok := mustOptString(msg, response, payload, "agent_id")
		if !ok {
			break
		}
		scopes, _, err := optArray(payload, "revoked_scopes")
		if err != nil {
			response.Command = "error"
			response.Payload = invalidPayload(msg.Command, err)
			break
		}
		action := fmt.Sprintf("%v", payload["action"])

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
		response.Command, response.Payload = handleGitCloneRPC(msg.Source, msg.Payload)
	case "git.create":
		response.Command, response.Payload = handleGitCreateRPC(msg.Source, msg.Payload)
	case "git.push":
		response.Command, response.Payload = handleGitPushRPC(msg.Source, msg.Payload)
	case "skill.create":
		response.Command, response.Payload = handleSkillCreateRPC(msg.Source, msg.Payload, skills)
	case "pr.merge":
		response.Command, response.Payload = handlePRMergeRPC(msg.Source, msg.Payload, prs, proposals)
	case "pr.rollback":
		response.Command, response.Payload = handlePRRollbackRPC(msg.Payload, prs)
	case "builder.destroy", "builder.destroyed", "destroy.builder", "builder.wipe", "vm.destroy", "sandbox.destroy":
		wipeBuilderLeftovers()
		requestOrchestratorStopVM(encoder, priv, response.Timestamp, msg.Payload)
		response.Command = "builder.destroyed"
		response.Payload = "ok"
	case "pr.create":
		payload, ok := mustPayload(msg, response)
		if !ok {
			break
		}
		id, ok := mustString(msg, response, payload, "id")
		if !ok {
			break
		}
		prs[id] = payload
		saveToFile("prs.json", prs)
		response.Command = "pr.created"
		response.Payload = "ok"
	case "pr.update":
		payload, ok := mustPayload(msg, response)
		if !ok {
			break
		}
		id, ok := mustString(msg, response, payload, "id")
		if !ok {
			break
		}
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
		payload, ok := mustPayload(msg, response)
		if !ok {
			break
		}
		id, ok := mustString(msg, response, payload, "id")
		if !ok {
			break
		}
		response.Command = "pr.data"
		response.Payload = prs[id]

	// Phase 4: secrets.push — Store produces and sends encrypted secret blobs to the Network Boundary.
	// SPEC: secret-management.md §Key Guarantees (Store is the sole producer of encrypted blobs)
	//       + network-boundary.md (encrypted blobs over Hub, decryption + zeroization only inside Boundary).
	// This is the production path that replaces all file/dir/env secret distribution.
	case "secrets.push":
		payload, ok := mustPayload(msg, response)
		if !ok {
			break
		}
		secretsMap, _, err := optObject(payload, "secrets")
		if err != nil {
			response.Command = "error"
			response.Payload = invalidPayload(msg.Command, err)
			break
		}

		// Convert to map[string]string for the crypto helper
		secrets := make(map[string]string)
		for k, v := range secretsMap {
			if s, ok := v.(string); ok {
				secrets[k] = s
			}
		}

		// Load symmetric key (same env convention as the Boundary for Phase 4)
		symKeyB64 := strings.TrimSpace(os.Getenv("AEGIS_SECRETS_SYMMETRIC_KEY"))
		symKey, _ := base64.StdEncoding.DecodeString(symKeyB64)
		if len(symKey) != 32 {
			response.Command = "error"
			response.Payload = "AEGIS_SECRETS_SYMMETRIC_KEY missing or invalid (must be 32-byte base64)"
			break
		}

		blobPayload, err := createEncryptedSecretBlobPayload(secrets, symKey, map[string]interface{}{
			"source": "secrets.push",
		})
		if err != nil {
			response.Command = "error"
			response.Payload = err.Error()
			break
		}

		// Send signed message to the Network Boundary
		updateMsg := Message{
			Source:      "store",
			Destination: "network-boundary",
			Command:     "secrets.update", // or "secrets.push" — boundary accepts either in current wiring
			Payload:     blobPayload,
			Timestamp:   time.Now().UTC().Format(time.RFC3339),
			Signature:   "",
		}
		signMessage(&updateMsg, priv)
		// Send to Boundary (no extra mutex needed here — follows the same pattern as court-scribe notifications)
		encoder.Encode(updateMsg)

		response.Command = "secrets.pushed"
		response.Payload = map[string]interface{}{"status": "encrypted blob sent", "skills": len(secrets)}

	// === Teams (minimal stub for Phase 5 Teams plan slice) ===
	case "team.create":
		payload, ok := mustPayload(msg, response)
		if !ok {
			break
		}
		id, ok := mustString(msg, response, payload, "id")
		if !ok {
			break
		}
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
		payload, ok := mustPayload(msg, response)
		if !ok {
			break
		}
		id, ok := mustString(msg, response, payload, "id")
		if !ok {
			break
		}
		response.Command = "team.data"
		response.Payload = teams[id]
	case "team.message":
		payload, ok := mustPayload(msg, response)
		if !ok {
			break
		}
		teamID, ok := mustString(msg, response, payload, "team_id")
		if !ok {
			break
		}
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

	// === Channels (collaboration model) ===
	// Minimal primitives for the Slack-inspired model: named persistent spaces
	// with role/agent membership and message history. Store is the source of truth
	// (per store-vm.md + collaboration-model.md). History/artifacts/proposals can
	// be annotated by channel. Later: PM will use these for delegation; UI for roster.
	// Messages here are the channel log (separate from per-agent chat turns).
	case "channel.create":
		handled := handleChannelCreate(msg.Payload, channels, response.Timestamp)
		response.Command = handled.Command
		response.Payload = handled.Payload
	case "channel.list":
		list := []interface{}{}
		for _, c := range channels {
			list = append(list, c)
		}
		response.Command = "channel.list"
		response.Payload = list
	case "channel.get":
		payload, ok := mustPayload(msg, response)
		if !ok {
			break
		}
		id, ok := mustChannelID(msg, response, payload)
		if !ok {
			break
		}
		if ch, ok := channels[id].(map[string]interface{}); ok {
			prepareChannelRecord(ch)
			channels[id] = ch
		}
		response.Command = "channel.data"
		response.Payload = channels[id]
	case "channel.join":
		payload, ok := mustPayload(msg, response)
		if !ok {
			break
		}
		chID, ok := mustString(msg, response, payload, "channel_id")
		if !ok {
			break
		}
		member := map[string]interface{}{
			"role":      payload["role"],
			"agent_id":  payload["agent_id"], // optional for role-based
			"joined_at": response.Timestamp,
		}
		channeldata.EnsureMemberDefaults(member)
		if ch, ok := channels[chID].(map[string]interface{}); ok {
			members, ok := mustStoredArray(response, ch, "members")
			if !ok {
				break
			}
			members = append(members, member)
			ch["members"] = members
			channels[chID] = ch
			saveToFile("channels.json", channels)
		}
		response.Command = "channel.joined"
		response.Payload = map[string]interface{}{"channel_id": chID}
	case "channel.post":
		payload, ok := mustPayload(msg, response)
		if !ok {
			break
		}
		chID, ok := mustString(msg, response, payload, "channel_id")
		if !ok {
			break
		}
		collab.Tracef("store", "channel.post", "ch=%s from=%v", chID, payload["from"])

		posted := false
		msgSeq := 0
		if ch, ok := channels[chID].(map[string]interface{}); ok {
			msgs, ok := mustStoredArray(response, ch, "messages")
			if !ok {
				break
			}
			// prepareChannelRecord and NextChannelSeq change ch in place (seq
			// backfill, member defaults, next_seq) before the append below.
			// Everything after them is map writes, append and saveToFile, none
			// of which panics on input that got this far, so a recovered panic
			// can't leave half a post in memory. If that ever changes, build
			// the new record first and assign it to channels[chID] once.
			prepareChannelRecord(ch)
			msgSeq = channeldata.NextChannelSeq(ch)
			entry := map[string]interface{}{
				"ts":      response.Timestamp,
				"seq":     msgSeq,
				"from":    payload["from"], // user, pm, @role, agent-id etc.
				"content": payload["content"],
			}
			msgs = append(msgs, entry)
			ch["messages"] = msgs
			channels[chID] = ch
			saveToFile("channels.json", channels)
			posted = true
		}
		if posted {
			from, _ := payload["from"].(string)
			content := channeldata.MessageContent(map[string]interface{}{"content": payload["content"]})
			if content == "" {
				if s, ok := payload["content"].(string); ok {
					content = s
				}
			}
			emitChannelUpdated(encoder, priv, response.Timestamp, chID, from, content, msgSeq)
		}
		response.Command = "channel.posted"
		response.Payload = "ok"

	case "channel.archive":
		payload, ok := mustPayload(msg, response)
		if !ok {
			break
		}
		chID, ok := mustChannelID(msg, response, payload)
		if !ok {
			break
		}
		if ch, ok := channels[chID].(map[string]interface{}); ok {
			ch["archived"] = true
			ch["archived_at"] = response.Timestamp
			channels[chID] = ch
			saveToFile("channels.json", channels)
		}
		response.Command = "channel.archived"
		response.Payload = map[string]interface{}{"channel_id": chID}

	case "channel.add_member":
		payload, ok := mustPayload(msg, response)
		if !ok {
			break
		}
		chID, ok := mustChannelID(msg, response, payload)
		if !ok {
			break
		}
		role, ok := mustOptString(msg, response, payload, "role")
		if !ok {
			break
		}
		role = collab.NormalizeMemberRole(role)
		member := map[string]interface{}{
			"role":     role,
			"agent_id": payload["agent_id"],
			"added_at": response.Timestamp,
		}
		channeldata.EnsureMemberDefaults(member)
		if ch, ok := channels[chID].(map[string]interface{}); ok {
			members, ok := mustStoredArray(response, ch, "members")
			if !ok {
				break
			}
			duplicate := false
			for _, item := range members {
				if m, ok := item.(map[string]interface{}); ok && channeldata.MemberRole(m) == role {
					duplicate = true
					break
				}
			}
			if !duplicate {
				members = append(members, member)
				ch["members"] = members
				channels[chID] = ch
				saveToFile("channels.json", channels)
			}
		}
		response.Command = "channel.member_added"
		response.Payload = map[string]interface{}{"channel_id": chID}

	case "channel.remove_member":
		payload, ok := mustPayload(msg, response)
		if !ok {
			break
		}
		chID, ok := mustChannelID(msg, response, payload)
		if !ok {
			break
		}
		roleToRemove, ok := mustOptString(msg, response, payload, "role")
		if !ok {
			break
		}
		if ch, ok := channels[chID].(map[string]interface{}); ok {
			members, ok := mustStoredArray(response, ch, "members")
			if !ok {
				break
			}
			newMembers := []interface{}{}
			for _, m := range members {
				if mm, ok := m.(map[string]interface{}); ok {
					if r, ok := mm["role"].(string); ok && r == roleToRemove {
						continue
					}
				}
				newMembers = append(newMembers, m)
			}
			ch["members"] = newMembers
			channels[chID] = ch
			saveToFile("channels.json", channels)
		}
		response.Command = "channel.member_removed"
		response.Payload = map[string]interface{}{"channel_id": chID}

	case channelfacilitator.CmdMemberTurnUpdate:
		payload, ok := mustPayload(msg, response)
		if !ok {
			break
		}
		chID, ok := mustChannelID(msg, response, payload)
		if !ok {
			break
		}
		role, ok := mustOptString(msg, response, payload, "role")
		if !ok {
			break
		}
		if ch, ok := channels[chID].(map[string]interface{}); ok {
			if _, ok := mustStoredArray(response, ch, "members"); !ok {
				break
			}
			if v, ok := payload["round_robin_index"]; ok {
				ch["round_robin_index"] = intFromPayload(v)
			}
			members := channeldata.MembersSlice(ch)
			for _, m := range members {
				if channeldata.MemberRole(m) != role {
					continue
				}
				if v, ok := payload["last_seen_seq"]; ok {
					m["last_seen_seq"] = intFromPayload(v)
				}
				if v, ok := payload["cycles_since_turn"]; ok {
					m["cycles_since_turn"] = intFromPayload(v)
				}
				if v, ok := payload["mention_boosts_left"]; ok {
					m["mention_boosts_left"] = intFromPayload(v)
				}
				if v, ok := payload["last_outcome"]; ok {
					m["last_outcome"] = v
				}
				if v, ok := payload["last_error"]; ok {
					m["last_error"] = v
				}
				if v, ok := payload["last_activity"]; ok {
					m["last_activity"] = v
				}
				if v, ok := payload["pending"]; ok {
					m["pending"] = v
				}
				break
			}
			ch["members"] = membersToInterface(members)
			channels[chID] = ch
			saveToFile("channels.json", channels)
		}
		response.Command = "channel.member_turn_updated"
		response.Payload = map[string]interface{}{"channel_id": chID, "role": role}

	case channelfacilitator.CmdTurnState:
		payload, ok := mustPayload(msg, response)
		if !ok {
			break
		}
		chID, ok := mustChannelID(msg, response, payload)
		if !ok {
			break
		}
		out := map[string]interface{}{"channel_id": chID}
		if ch, ok := channels[chID].(map[string]interface{}); ok {
			prepareChannelRecord(ch)
			out["round_robin_index"] = ch["round_robin_index"]
			out["turn_settings"] = channeldata.TurnSettingsAsMap(channeldata.EffectiveTurnSettings(ch))
			membersOut := []interface{}{}
			for _, m := range channeldata.MembersSlice(ch) {
				membersOut = append(membersOut, map[string]interface{}{
					"role":                channeldata.MemberRole(m),
					"last_seen_seq":       channeldata.MemberLastSeenSeq(m),
					"cycles_since_turn":   m["cycles_since_turn"],
					"mention_boosts_left": m["mention_boosts_left"],
					"last_outcome":        m["last_outcome"],
					"last_error":          m["last_error"],
					"last_activity":       m["last_activity"],
					"pending":             m["pending"],
				})
			}
			out["members"] = membersOut
		}
		response.Command = channelfacilitator.CmdTurnStateData
		response.Payload = out

	case channelfacilitator.CmdGetMessages:
		payload, ok := mustPayload(msg, response)
		if !ok {
			break
		}
		chID, ok := mustChannelID(msg, response, payload)
		if !ok {
			break
		}
		sinceSeq := 0
		if v, ok := payload["since_seq"]; ok {
			sinceSeq = intFromPayload(v)
		}
		limit := 50
		if v, ok := payload["limit"]; ok {
			if n := intFromPayload(v); n > 0 {
				limit = n
			}
		}
		filter, _, err := optObject(payload, "filter")
		if err != nil {
			response.Command = "error"
			response.Payload = invalidPayload(msg.Command, err)
			break
		}
		var result []interface{}
		if ch, ok := channels[chID].(map[string]interface{}); ok {
			prepareChannelRecord(ch)
			for _, m := range channeldata.MessagesSlice(ch) {
				seq := channeldata.MessageSeq(m)
				if seq <= sinceSeq {
					continue
				}
				if !messageMatchesFilter(m, filter) {
					continue
				}
				result = append(result, m)
				if len(result) >= limit {
					break
				}
			}
		}
		response.Command = channelfacilitator.CmdGetMessages + ".data"
		response.Payload = map[string]interface{}{
			"channel_id": chID,
			"since_seq":  sinceSeq,
			"messages":   result,
		}

	case channelfacilitator.CmdGetRelevantSince:
		payload, ok := mustPayload(msg, response)
		if !ok {
			break
		}
		chID, ok := mustChannelID(msg, response, payload)
		if !ok {
			break
		}
		sinceSeq := 0
		if v, ok := payload["since_seq"]; ok {
			sinceSeq = intFromPayload(v)
		}
		anchorSet := map[int]struct{}{}
		rawAnchors, _, err := optArray(payload, "anchor_seqs")
		if err != nil {
			response.Command = "error"
			response.Payload = invalidPayload(msg.Command, err)
			break
		}
		for _, a := range rawAnchors {
			anchorSet[intFromPayload(a)] = struct{}{}
		}
		var batch []interface{}
		var anchors []interface{}
		if ch, ok := channels[chID].(map[string]interface{}); ok {
			prepareChannelRecord(ch)
			bySeq := map[int]map[string]interface{}{}
			for _, m := range channeldata.MessagesSlice(ch) {
				bySeq[channeldata.MessageSeq(m)] = m
			}
			for seq := range anchorSet {
				if m, ok := bySeq[seq]; ok {
					anchors = append(anchors, m)
				}
			}
			for _, m := range channeldata.MessagesSlice(ch) {
				seq := channeldata.MessageSeq(m)
				if seq <= sinceSeq {
					continue
				}
				batch = append(batch, m)
			}
		}
		response.Command = channelfacilitator.CmdGetRelevantSince + ".data"
		response.Payload = map[string]interface{}{
			"channel_id":   chID,
			"since_seq":    sinceSeq,
			"new_messages": batch,
			"anchors":      anchors,
		}

	// default PM in create if missing
	// (handled in create above by caller, but ensure here too for robustness)
	// Web-portal chat session registry (store-vm.md: Store owns durable structured data).
	// Message turns are handled by the agent chat system; the portal persists the
	// session thread here after each exchange via sessions.save / sessions.history.
	case "sessions.list":
		list, err := chatSessions.ListSummaries()
		if err != nil {
			response.Command = "error"
			response.Payload = err.Error()
		} else {
			if list == nil {
				list = []chatstore.Summary{}
			}
			response.Command = "sessions.list"
			response.Payload = list
		}
	case "sessions.create":
		payload, ok := mustPayload(msg, response)
		if !ok {
			break
		}
		title, ok := mustOptString(msg, response, payload, "title")
		if !ok {
			break
		}
		sess, err := chatSessions.Create(title)
		if err != nil {
			response.Command = "error"
			response.Payload = err.Error()
		} else {
			response.Command = "sessions.created"
			response.Payload = map[string]interface{}{"session": sess}
		}
	case "sessions.history", "sessions.get":
		payload, ok := mustPayload(msg, response)
		if !ok {
			break
		}
		id, ok := mustOptString(msg, response, payload, "session_id")
		if !ok {
			break
		}
		if id == "" {
			id, ok = mustOptString(msg, response, payload, "id")
			if !ok {
				break
			}
		}
		if id == "" {
			response.Command = "error"
			response.Payload = "session_id required"
		} else if sess, ok, err := chatSessions.Get(id); err != nil {
			response.Command = "error"
			response.Payload = err.Error()
		} else if !ok {
			response.Command = "error"
			response.Payload = "session not found"
		} else {
			response.Command = "sessions.history"
			response.Payload = map[string]interface{}{"session": sess}
		}
	case "sessions.save":
		payload, ok := mustPayload(msg, response)
		if !ok {
			break
		}
		id, ok := mustOptString(msg, response, payload, "id")
		if !ok {
			break
		}
		if id == "" {
			response.Command = "error"
			response.Payload = "id required"
		} else {
			sess := chatstore.Session{ID: id}
			title, ok := mustOptString(msg, response, payload, "title")
			if !ok {
				break
			}
			sess.Title = title
			rawMsgs, present, err := optArray(payload, "messages")
			if err != nil {
				response.Command = "error"
				response.Payload = invalidPayload(msg.Command, err)
				break
			}
			if present {
				sess.Messages = decodeChatMessages(rawMsgs)
			}
			if err := chatSessions.Save(sess); err != nil {
				response.Command = "error"
				response.Payload = err.Error()
			} else if updated, ok, err := chatSessions.Get(id); err != nil || !ok {
				response.Command = "error"
				response.Payload = "failed to reload session"
			} else {
				response.Command = "sessions.saved"
				response.Payload = map[string]interface{}{"session": updated}
			}
		}
	case "skill.register":
		payload, ok := mustPayload(msg, response)
		if !ok {
			break
		}
		id, ok := mustString(msg, response, payload, "id")
		if !ok {
			break
		}
		skills[id] = payload
		saveToFile("skills.json", skills)
		response.Command = "skill.registered"
		response.Payload = "ok"
	case "skill.get":
		payload, ok := mustPayload(msg, response)
		if !ok {
			break
		}
		id, ok := mustString(msg, response, payload, "id")
		if !ok {
			break
		}
		response.Command = "skill.data"
		response.Payload = skills[id]

	case "build.complete":
		payload, ok := mustPayload(msg, response)
		if !ok {
			break
		}
		id, ok := mustString(msg, response, payload, "proposal_id")
		if !ok {
			break
		}
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
		payload, ok := mustPayload(msg, response)
		if !ok {
			break
		}
		id, ok := mustString(msg, response, payload, "proposal_id")
		if !ok {
			break
		}
		report := payload["report"]
		if p, ok := proposals[id].(map[string]interface{}); ok {
			p["state"] = "build_failed"
			p["build_report"] = report // non-leaking
			proposals[id] = p
			saveToFile("proposals.json", proposals)
		}
		response.Command = "build.recorded"
		response.Payload = "ok"
	case "llm.usage.record":
		// One-way hub push (isOneWayHubPush). No reply for an accept or a
		// rejection: llm.usage.recorded is not granted, and an error frame
		// would be stray traffic. Rejections are logged in the handler.
		handleLLMUsageRecord(msg)
		return true
	case "llm.usage.summary":
		handled := handleLLMUsageSummary(msg)
		response.Command = handled.Command
		response.Payload = handled.Payload
	case "llm.usage.recent":
		handled := handleLLMUsageRecent(msg)
		response.Command = handled.Command
		response.Payload = handled.Payload
	case "skill.list":
		list := []interface{}{}
		for _, s := range skills {
			list = append(list, s)
		}
		response.Command = "skill.list"
		response.Payload = list
	case "memory.store":
		payload, ok := mustPayload(msg, response)
		if !ok {
			break
		}
		content, ok := mustString(msg, response, payload, "content")
		if !ok {
			break
		}
		memories[content] = payload
		saveToFile("memories.json", memories)
		response.Command = "memory.stored"
		response.Payload = "ok"
	case "memory.query":
		// Stub
		response.Command = "memory.results"
		response.Payload = []interface{}{}
	case "audit.append":
		// The entry is wrapped with the hub-verified source and a Store
		// timestamp, its fields are capped, and identical blocked_request
		// events are coalesced (audit_log.go).
		if entry, ok := auditAppendEntry(storeAuditCoalescer, msg, time.Now()); ok {
			auditLog = append(auditLog, entry)
			persistAudit(auditLog, msg)
		}
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
	case storeSecurityStatsCommand:
		handleStoreSecurityStats(msg, response, storePanicDedup)
	case "ping":
		response.Command = "pong"
		response.Payload = "ok"
	case "version", "get-version":
		if msg.Command == "get-version" {
			response.Command = "version"
			response.Source = "store"
			response.Destination = msg.Source
			response.Payload = map[string]string{"version": getBuildVersion()}
		} else {
			response.Command = "version"
			response.Payload = map[string]string{"version": getBuildVersion()}
		}
	default:
		if handled, cmd, payload := handlePermissionCommand(msg, response, skills, &auditLog); handled {
			response.Command = cmd
			response.Payload = payload
			break
		}
		if ok, errMsg := permissionCheckAtStore(msg.Source, msg.Command, skills); !ok {
			response.Command = "error"
			response.Payload = errMsg
			break
		}
		response.Command = "error"
		response.Payload = "unknown command"
	}
	return false
}
