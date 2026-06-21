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

// Hub is the minimal hub surface the facilitator needs.
type Hub interface {
	Send(ctx context.Context, msg hubclient.Message) (hubclient.Message, error)
	AssignedID() string
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
	if recipient == "" {
		return
	}
	sinceSeq := 0
	for _, m := range members {
		if channeldata.MemberRole(m) == recipient {
			sinceSeq = channeldata.MemberLastSeenSeq(m)
			break
		}
	}
	allMsgs := channeldata.MessagesSlice(chMap)
	var batch []map[string]interface{}
	maxSeq := sinceSeq
	for _, m := range allMsgs {
		seq := channeldata.MessageSeq(m)
		if seq <= sinceSeq {
			continue
		}
		batch = append(batch, m)
		if seq > maxSeq {
			maxSeq = seq
		}
	}
	if len(batch) == 0 && !collab.IsHumanPoster(from) {
		collab.Tracef(ComponentID, "turn.skip", "ch=%s recipient=%s reason=no_new_messages", chID, recipient)
		return
	}
	window := allMsgs
	anchors := ComputeRelevanceAnchors(recipient, sinceSeq, batch, window, settings)
	newMsgs := make([]interface{}, len(batch))
	for i, m := range batch {
		newMsgs[i] = m
	}
	turn := map[string]interface{}{
		"channel_id":         chID,
		"recipient":          recipient,
		"since_seq":          sinceSeq,
		"new_messages":       newMsgs,
		"relevance_anchors":  anchors,
		"mention_boosts":     mentionBoosts,
		"generated_at":       time.Now().UTC().Format(time.RFC3339),
	}
	collab.Tracef(ComponentID, "turn.deliver", "ch=%s recipient=%s since=%d new=%d anchors=%v", chID, recipient, sinceSeq, len(batch), anchors)

	_ = f.ensureRole(ctx, recipient, chID)
	if err := f.deliverTurn(ctx, chID, recipient, turn); err != nil {
		collab.Tracef(ComponentID, "turn.error", "ch=%s recipient=%s err=%v", chID, recipient, err)
		return
	}

	// Persist last_seen_seq and advance scheduler state.
	for _, m := range members {
		role := channeldata.MemberRole(m)
		cycles := intFromMember(m["cycles_since_turn"])
		if role == recipient {
			m["last_seen_seq"] = maxSeq
			m["cycles_since_turn"] = 0
			if collab.IsMentioned(role, content) {
				left := intFromMember(m["mention_boosts_left"])
				m["mention_boosts_left"] = left + 1
			}
		} else {
			m["cycles_since_turn"] = cycles + 1
		}
	}
	_ = f.updateMemberState(ctx, chID, recipient, map[string]interface{}{
		"last_seen_seq":       maxSeq,
		"cycles_since_turn":   0,
		"round_robin_index":   newRR,
		"mention_boosts_left": mentionBoostsUsed(members, recipient, content),
	})
	_ = f.persistCycles(ctx, chID, members, recipient)
	_ = newRR
}

func mentionBoostsUsed(members []map[string]interface{}, recipient, content string) int {
	for _, m := range members {
		if channeldata.MemberRole(m) == recipient {
			left := intFromMember(m["mention_boosts_left"])
			if collab.IsMentioned(recipient, content) {
				return left + 1
			}
			return left
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
	resp, err := f.hub.Send(ctx, hubclient.Message{
		Source:      f.hub.AssignedID(),
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
	_, err := f.hub.Send(ctx, hubclient.Message{
		Source:      f.hub.AssignedID(),
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
	_, err := f.hub.Send(ctx, hubclient.Message{
		Source:      f.hub.AssignedID(),
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
			_, err := f.hub.Send(ctx, hubclient.Message{
				Source:      f.hub.AssignedID(),
				Destination: dest,
				Command:     CmdTurn,
				Payload:     turn,
				Timestamp:   time.Now().UTC().Format(time.RFC3339),
			})
			if err == nil {
				collab.Tracef(ComponentID, "turn.delivered", "ch=%s dest=%s", chID, dest)
				return nil
			}
			lastErr = err
		}
		time.Sleep(500 * time.Millisecond)
	}
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("no destination for role %q on channel %q", role, chID)
}

func turnDestinations(role, chID string) []string {
	if role == "project-manager" && chID != "" {
		return []string{"project-manager-" + chID, "project-manager"}
	}
	if chID != "" && !strings.HasPrefix(role, "court-persona-") {
		return []string{role + "-" + chID, role}
	}
	return []string{role}
}