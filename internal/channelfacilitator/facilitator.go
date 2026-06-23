package channelfacilitator

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"AegisClaw/internal/channeldata"
	"AegisClaw/internal/collab"
	"AegisClaw/internal/transport/hubclient"
)

// Hub is the minimal hub surface the facilitator needs for outbound RPCs.
type Hub interface {
	Send(ctx context.Context, msg hubclient.Message) (hubclient.Message, error)
	Fire(ctx context.Context, msg hubclient.Message) error
}

// Facilitator schedules batched turns per channel.
type Facilitator struct {
	hub    Hub
	mu     sync.Mutex
	actors map[string]*ChannelActor
}

func (f *Facilitator) actorFor(chID string) *ChannelActor {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.actors[chID]
	if !ok {
		a = &ChannelActor{chID: chID, mu: make(chan struct{}, 1)}
		f.actors[chID] = a
	}
	return a
}

// HandleTurnResult records agent outcome after processing a turn (e.g. "posted" or "no_reply").
// Used to distinguish intentional NO_REPLY from errors for observability (spec §8.4).
func (f *Facilitator) HandleTurnResult(ctx context.Context, payload map[string]interface{}) {
	chID, _ := payload["channel_id"].(string)
	role, _ := payload["from"].(string)
	if role == "" {
		role, _ = payload["role"].(string)
	}
	if role == "" {
		role, _ = payload["recipient"].(string)
	}
	outcome, _ := payload["outcome"].(string)
	if chID == "" || role == "" {
		return
	}
	role = collab.NormalizeMemberRole(role)
	now := time.Now().UTC().Format(time.RFC3339)
	fields := map[string]interface{}{
		"last_outcome":  outcome,
		"last_activity": now,
		"pending":       false,
	}
	if errStr, ok := payload["error"].(string); ok && errStr != "" {
		fields["last_error"] = errStr
		fields["last_outcome"] = "error"
	}
	_ = f.updateMemberState(ctx, chID, role, fields)
	collab.Tracef(ComponentID, "turn.result", "ch=%s role=%s outcome=%s", chID, role, outcome)
}

// HandleUpdated schedules turn delivery for a channel update (single actor per channel).
func (f *Facilitator) HandleUpdated(ctx context.Context, payload map[string]interface{}) {
	chID, _ := payload["channel_id"].(string)
	if chID == "" {
		return
	}
	actor := f.actorFor(chID)
	select {
	case actor.mu <- struct{}{}:
		defer func() { <-actor.mu }()
		f.processUpdate(ctx, chID, payload)
	default:
		// Another update is being processed; queue by acquiring token.
		actor.mu <- struct{}{}
		defer func() { <-actor.mu }()
		f.processUpdate(ctx, chID, payload)
	}
}

func (f *Facilitator) processUpdate(ctx context.Context, chID string, update map[string]interface{}) {
	from, _ := update["from"].(string)
	content := collab.PayloadContentString(update["content"])
	collab.Tracef(ComponentID, "turn.schedule", "ch=%s from=%s", chID, from)

	chData, err := f.fetchChannel(ctx, chID)
	if err != nil {
		collab.Tracef(ComponentID, "turn.error", "ch=%s stage=fetch err=%v", chID, err)
		return
	}
	chMap, ok := chData.(map[string]interface{})
	if !ok {
		return
	}
	settings := channeldata.EffectiveTurnSettings(chMap)
	members := channeldata.MembersSlice(chMap)
	if len(members) == 0 {
		return
	}
	rrIndex := intFromMember(chMap["round_robin_index"])
	recipient, newRR, mentionBoosts := SelectNextRecipient(members, rrIndex, content, settings)
	recipient = collab.NormalizeMemberRole(recipient)
	if recipient == "" {
		return
	}

	// Multi-recipient + fairness/catch-up for mention-heavy/assignment posts (spec §3.2, §3.3).
	// Trigger on signals (PM plans use @mentions/assign language even if !human from).
	// Prioritize mentioned for assignment posts (ensures coder etc get turn even in long rosters)
	recipients := []string{}
	if hasStrongMentions(content) || isAssignmentLike(content) {
		mentioned := collectMentionedRoles(members, content)
		starved := collectStarvedRoles(members, settings.StarvationCycles)
		for _, r := range mentioned {
			rn := collab.NormalizeMemberRole(r)
			if !containsRole(recipients, rn) && len(recipients) < 3 {
				recipients = append(recipients, rn)
			}
		}
		for _, r := range starved {
			rn := collab.NormalizeMemberRole(r)
			if !containsRole(recipients, rn) && len(recipients) < 3 {
				recipients = append(recipients, rn)
			}
		}
	}
	if len(recipients) == 0 {
		recipients = []string{recipient}
	}
	allMsgs := channeldata.MessagesSlice(chMap)

	// Deliver (possibly to multiple for fairness/multi) and update state for each.
	deliveredAny := false
	for _, rec := range recipients {
		rec = collab.NormalizeMemberRole(rec)
		if rec == "" {
			continue
		}
		recSince := 0
		for _, m := range members {
			if channeldata.MemberRole(m) == rec {
				recSince = channeldata.MemberLastSeenSeq(m)
				break
			}
		}
		var recBatch []map[string]interface{}
		recMax := recSince
		for _, m := range allMsgs {
			seq := channeldata.MessageSeq(m)
			if seq <= recSince {
				continue
			}
			recBatch = append(recBatch, m)
			if seq > recMax {
				recMax = seq
			}
		}
		if len(recBatch) == 0 && !collab.IsHumanPoster(from) {
			collab.Tracef(ComponentID, "turn.skip", "ch=%s recipient=%s reason=no_new_messages", chID, rec)
			continue
		}
		recAnchors := ComputeRelevanceAnchors(rec, recSince, recBatch, allMsgs, settings)
		recNewMsgs := make([]interface{}, len(recBatch))
		for i, m := range recBatch {
			recNewMsgs[i] = m
		}
		recTurn := map[string]interface{}{
			"channel_id":         chID,
			"recipient":          rec,
			"since_seq":          recSince,
			"new_messages":       recNewMsgs,
			"relevance_anchors":  recAnchors,
			"mention_boosts":     mentionBoosts,
			"generated_at":       time.Now().UTC().Format(time.RFC3339),
		}
		collab.Tracef(ComponentID, "turn.deliver", "ch=%s recipient=%s since=%d new=%d anchors=%v", chID, rec, recSince, len(recBatch), recAnchors)

		_ = f.ensureRole(ctx, rec, chID)
		now := time.Now().UTC().Format(time.RFC3339)
		if err := f.deliverTurn(ctx, chID, rec, recTurn); err != nil {
			collab.Tracef(ComponentID, "turn.error", "ch=%s recipient=%s err=%v", chID, rec, err)
			_ = f.updateMemberState(ctx, chID, rec, map[string]interface{}{
				"last_outcome":  "error",
				"last_error":    err.Error(),
				"last_activity": now,
				"pending":       false,
			})
			_, _ = f.hub.Send(ctx, hubclient.Message{
				Destination: "store",
				Command:     "channel.post",
				Payload: map[string]interface{}{
					"channel_id": chID,
					"from":       "system",
					"content":    fmt.Sprintf("[turn error] delivery to %s failed: %v", rec, err),
				},
				Timestamp: now,
			})
			continue
		}
		// update last_seen for this rec (cycles normalized in final pass)
		for _, m := range members {
			role := channeldata.MemberRole(m)
			if role == rec {
				m["last_seen_seq"] = recMax
				m["cycles_since_turn"] = 0
			}
		}
		_ = f.updateMemberState(ctx, chID, rec, map[string]interface{}{
			"last_seen_seq":       recMax,
			"cycles_since_turn":   0,
			"round_robin_index":   newRR,
			"mention_boosts_left": mentionBoostsUsed(members, rec, content),
			"last_outcome":        "delivered",
			"last_error":          "",
			"last_activity":       now,
			"pending":             true,
		})
		deliveredAny = true
	}
	// Single cycle update pass for the schedule (non-recipients +1 once, even on multi).
	if deliveredAny {
		for _, m := range members {
			role := channeldata.MemberRole(m)
			if !containsRole(recipients, role) {
				cyc := intFromMember(m["cycles_since_turn"])
				m["cycles_since_turn"] = cyc + 1
			}
		}
		_ = f.persistCycles(ctx, chID, members, recipients[0])
		collab.Tracef(ComponentID, "turn.last_seen", "ch=%s recipients=%v last_rr=%d", chID, recipients, newRR)
		// Post compact status only for significant assignment posts (human or @mentions/plan).
		// This avoids flooding the channel with a status per internal PM update or routine post.
		// The UI will show only the latest status line by default + expand for history.
		// Only surface status for human posts or primary PM *plan-like* posts (containing plan/goal/task structure).
		// This prevents status spam from routine PM monitoring, agent responses, or follow-ups (even with @).
		lowerContent := strings.ToLower(content)
		isPlanLike := strings.Contains(lowerContent, "plan") || strings.Contains(lowerContent, "##") || strings.Contains(lowerContent, "goal") || strings.Contains(lowerContent, "task 1")
		isSignificant := collab.IsHumanPoster(from) || (strings.Contains(strings.ToLower(from), "project-manager") && isPlanLike)
		if isSignificant {
			statusContent := fmt.Sprintf("status: turns delivered to %v (rr=%d) — batch complete, no more pending turns this cycle", recipients, newRR)
			_, _ = f.hub.Send(ctx, hubclient.Message{
				Destination: "store",
				Command:     "channel.post",
				Payload: map[string]interface{}{
					"channel_id": chID,
					"from":       "system",
					"content":    statusContent,
				},
				Timestamp: time.Now().UTC().Format(time.RFC3339),
			})
		}
	}
	_ = newRR
}

func mentionBoostsUsed(members []map[string]interface{}, recipient, content string) int {
	for _, m := range members {
		if channeldata.MemberRole(m) == recipient {
			// caller already applied any mention increment to the member map
			return intFromMember(m["mention_boosts_left"])
		}
	}
	return 0
}

func (f *Facilitator) persistCycles(ctx context.Context, chID string, members []map[string]interface{}, skipRole string) error {
	for _, m := range members {
		role := channeldata.MemberRole(m)
		if role == skipRole {
			continue
		}
		_ = f.updateMemberState(ctx, chID, role, map[string]interface{}{
			"cycles_since_turn": intFromMember(m["cycles_since_turn"]),
		})
	}
	return nil
}

func (f *Facilitator) fetchChannel(ctx context.Context, chID string) (interface{}, error) {
	ctx, cancel := rpcTimeout(ctx)
	defer cancel()
	resp, err := f.hub.Send(ctx, hubclient.Message{
		Destination: "store",
		Command:     "channel.get",
		Payload:     map[string]interface{}{"channel_id": chID},
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		return nil, err
	}
	return resp.Payload, nil
}

func (f *Facilitator) updateMemberState(ctx context.Context, chID, role string, fields map[string]interface{}) error {
	payload := map[string]interface{}{"channel_id": chID, "role": role}
	for k, v := range fields {
		payload[k] = v
	}
	ctx, cancel := rpcTimeout(ctx)
	defer cancel()
	_, err := f.hub.Send(ctx, hubclient.Message{
		Destination: "store",
		Command:     CmdMemberTurnUpdate,
		Payload:     payload,
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	})
	return err
}

func (f *Facilitator) ensureRole(ctx context.Context, role, chID string) error {
	if role == "" || chID == "" {
		return nil
	}
	ctx, cancel := rpcTimeout(ctx)
	defer cancel()
	_, err := f.hub.Send(ctx, hubclient.Message{
		Destination: "daemon-orchestrator",
		Command:     "ensure.role",
		Payload: map[string]interface{}{
			"role":    role,
			"channel": chID,
		},
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
	return err
}

func (f *Facilitator) deliverTurn(ctx context.Context, chID, role string, turn map[string]interface{}) error {
	dests := turnDestinations(role, chID)
	var lastErr error
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		for _, dest := range dests {
			sendCtx, cancel := rpcTimeout(ctx)
			resp, err := f.hub.Send(sendCtx, hubclient.Message{
				Destination: dest,
				Command:     CmdTurn,
				Payload:     turn,
				Timestamp:   time.Now().UTC().Format(time.RFC3339),
			})
			cancel()
			if err == nil && resp.Command != "error" {
				collab.Tracef(ComponentID, "turn.delivered", "ch=%s dest=%s", chID, dest)
				return nil
			}
			if err != nil {
				lastErr = err
			} else {
				lastErr = fmt.Errorf("hub error delivering turn to %s: %v", dest, resp.Payload)
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("no destination for role %q on channel %q", role, chID)
}

func turnDestinations(role, chID string) []string {
	role = collab.NormalizeMemberRole(role)
	if role == "project-manager" && chID != "" {
		return []string{"project-manager-" + chID, "project-manager"}
	}
	if chID != "" && !strings.HasPrefix(role, "court-persona-") {
		return []string{role + "-" + chID, role}
	}
	return []string{role}
}

// hasStrongMentions / isAssignmentLike / collect* support multi-recipient + catch-up fairness (spec §3).
func hasStrongMentions(content string) bool {
	lower := strings.ToLower(content)
	count := 0
	for _, p := range []string{"@coder", "@tester", "@ciso", "@architect", "assign", "@pm"} {
		if strings.Contains(lower, p) {
			count++
		}
	}
	return count >= 2
}

func isAssignmentLike(content string) bool {
	lower := strings.ToLower(content)
	for _, p := range []string{"plan:", "assign", "your task", "please implement", "flag for", "review for"} {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

func collectMentionedRoles(members []map[string]interface{}, content string) []string {
	var out []string
	lower := strings.ToLower(content)
	for _, m := range members {
		r := channeldata.MemberRole(m)
		if r == "" {
			continue
		}
		if collab.IsMentioned(r, content) || strings.Contains(lower, "@"+strings.ToLower(r)) {
			out = append(out, r)
		}
	}
	return out
}

func collectStarvedRoles(members []map[string]interface{}, starvation int) []string {
	if starvation <= 0 {
		starvation = channeldata.DefaultTurnSettings.StarvationCycles
	}
	var out []string
	for _, m := range members {
		if intFromMember(m["cycles_since_turn"]) >= starvation {
			if r := channeldata.MemberRole(m); r != "" {
				out = append(out, r)
			}
		}
	}
	return out
}

func containsRole(list []string, r string) bool {
	for _, x := range list {
		if x == r {
			return true
		}
	}
	return false
}