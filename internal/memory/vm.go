// Package memory provides the core Memory VM implementation.
//
// This is the real (non-surface) Memory VM for Phase 1.2.
//
// It handles the explicit commands from agent-runtime.md / memory-vm.md
// while enforcing ACLs and performing zeroization on eviction.

package memory

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"AegisClaw/internal/transport/hubclient"
)

// VM is the real Memory VM instance (1:1 with one Agent Runtime).
type VM struct {
	acl          *ACL
	shortTerm    *ShortTermContext
	longTerm     *LongTermMemory
	hub          hubclient.Client
	mu           sync.Mutex
	registeredID string
}

// NewVM creates a new Memory VM with the spec-mandated behaviors.
func NewVM(ttl time.Duration) *VM {
	return &VM{
		acl:       NewACL(""),
		shortTerm: NewShortTermContext(),
		longTerm:  NewLongTermMemory(ttl),
	}
}

// SetHubClient wires the authenticated hubclient (called after Dial + Register).
func (v *VM) SetHubClient(c hubclient.Client) {
	v.hub = c
}

// BindAgent binds this Memory VM to a specific agent ID (from register success).
func (v *VM) BindAgent(agentID string) {
	v.acl.Bind(agentID)
	v.registeredID = agentID
}

// Handle processes an incoming command from the Hub (after ACL check).
// Returns the response payload (or error).
func (v *VM) Handle(ctx context.Context, msg hubclient.Message) (interface{}, error) {
	if !v.acl.Allow(msg.Source) {
		return nil, fmt.Errorf("ERR_ACL_VIOLATION: source %s not paired with this memory", msg.Source)
	}

	v.mu.Lock()
	defer v.mu.Unlock()

	switch msg.Command {
	case "memory.get_context":
		// Per memory-vm.md §1: called automatically at start of every agent turn.
		short := v.shortTerm.GetRecent(10)
		long := v.longTerm.Search("general context", 5) // simple for skeleton

		// Zero sensitive data after use in response construction? Handled by caller.
		return map[string]interface{}{
			"short_term":  short,
			"long_term":   long,
			"token_count": v.shortTerm.tokenCount,
			"token_limit": v.shortTerm.limit,
		}, nil

	case "memory.store":
		payload, ok := msg.Payload.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid payload for store")
		}
		content, _ := payload["content"].(string)
		tagsIface, _ := payload["tags"].([]interface{})
		var tags []string
		for _, t := range tagsIface {
			if s, ok := t.(string); ok {
				tags = append(tags, s)
			}
		}
		imp, _ := payload["importance"].(float64)
		if imp == 0 {
			imp = 1.0
		}

		v.shortTerm.AddTurn(content) // also keep in short-term for context
		v.longTerm.Store(content, tags, imp)

		// Forward to Store VM (real path; skeleton logs intent)
		if v.hub != nil {
			storeMsg := hubclient.Message{
				Source:      v.registeredID,
				Destination: "store",
				Command:     "memory.store",
				Payload:     payload,
			}
			_, _ = v.hub.Send(ctx, storeMsg) // best effort in skeleton
		}
		return "ok", nil

	case "memory.search":
		payload, ok := msg.Payload.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid payload")
		}
		query, _ := payload["query"].(string)
		limit := 5
		if l, ok := payload["limit"].(float64); ok {
			limit = int(l)
		}
		results := v.longTerm.Search(query, limit)
		return results, nil

	case "memory.summarize":
		v.shortTerm.Summarize()
		return "ok", nil

	default:
		return nil, fmt.Errorf("unknown command: %s", msg.Command)
	}
}

// Close performs secure shutdown + zeroization.
func (v *VM) Close() {
	v.mu.Lock()
	defer v.mu.Unlock()

	// Paranoid zeroization of short-term history.
	for i := range v.shortTerm.history {
		zeroString(&v.shortTerm.history[i])
	}
	v.shortTerm.history = nil

	// Long-term purge with zeroization already done in LongTermMemory.
	log.Printf("memory: VM for %s closed with zeroization", v.registeredID)
}
