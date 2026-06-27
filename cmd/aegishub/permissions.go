package main

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"AegisClaw/internal/permissions"
)

var (
	permSnapMu       sync.RWMutex
	permSnapshots    = make(map[string]permissions.Snapshot) // componentID -> snapshot
	permBootstrap    = permissions.DefaultBootstrap()       // fallback when snapshot fetch fails
)

// fetchPermissionSnapshotFromStore synchronously requests permission.snapshot from Store via Hub RPC.
// The second return value is true when Store returned a valid snapshot (including deny-all).
func fetchPermissionSnapshotFromStore(componentID string) (permissions.Snapshot, bool) {
	registeredMutex.RLock()
	storeComp, ok := registered["store"]
	registeredMutex.RUnlock()
	if !ok || storeComp.Encoders == nil {
		return permissions.Snapshot{Subject: componentID, Timestamp: permissions.NowRFC3339()}, false
	}

	msg := Message{
		Source:      "hub",
		Destination: "store",
		Command:     "permission.snapshot",
		Payload:     map[string]interface{}{"subject": componentID},
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}

	reply := hubStoreRPC(msg)
	if reply.Command == "permission.snapshot" {
		if snap, ok := decodeSnapshot(reply.Payload); ok {
			permSnapMu.Lock()
			permSnapshots[componentID] = snap
			permSnapMu.Unlock()
			return snap, true
		}
	}
	return permissions.Snapshot{Subject: componentID, Timestamp: permissions.NowRFC3339()}, false
}

func decodeSnapshot(payload interface{}) (permissions.Snapshot, bool) {
	b, err := json.Marshal(payload)
	if err != nil {
		return permissions.Snapshot{}, false
	}
	var snap permissions.Snapshot
	if err := json.Unmarshal(b, &snap); err != nil {
		return permissions.Snapshot{}, false
	}
	return snap, true
}

// hubStoreRPC sends a message to store and waits for reply via the store component's decoder.
// This is used during register before the component's read loop starts.
func hubStoreRPC(msg Message) Message {
	registeredMutex.RLock()
	storeComp, ok := registered["store"]
	registeredMutex.RUnlock()
	if !ok || storeComp.Encoders == nil {
		return Message{Command: "error", Payload: "store unavailable"}
	}

	waitID := fmt.Sprintf("hub-perm-fetch-%d", time.Now().UnixNano())
	waitCh := registerPendingRPC(waitID)
	defer clearPendingRPC(waitID)

	msg.Source = waitID
	storeComp.Encoders.Mutex.Lock()
	_ = storeComp.Encoders.Encoder.Encode(msg)
	storeComp.Encoders.Mutex.Unlock()

	select {
	case reply := <-waitCh:
		return reply
	case <-time.After(5 * time.Second):
		return Message{Command: "error", Payload: "ERR_RPC_TIMEOUT"}
	}
}

// pushPermissionSnapshot sends signed snapshot to a newly registered component.
func pushPermissionSnapshot(componentID string, encoders *ComponentEncoders, snap permissions.Snapshot) {
	if encoders == nil {
		return
	}
	push := Message{
		Source:      "hub",
		Destination: componentID,
		Command:     "permission.snapshot",
		Payload:     snap,
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}
	encoders.Mutex.Lock()
	_ = encoders.Encoder.Encode(push)
	encoders.Mutex.Unlock()
	log.Printf("Hub: pushed permission snapshot v%d to %s (%d allowed, %d visible)",
		snap.Version, componentID, len(snap.AllowedTools), len(snap.VisibleTools))
}

// invalidatePermissionSnapshot refetches and pushes updated snapshot after grant/visibility change.
func invalidatePermissionSnapshot(componentID string) {
	snap, _ := fetchPermissionSnapshotFromStore(componentID)
	registeredMutex.RLock()
	comp, ok := registered[componentID]
	registeredMutex.RUnlock()
	if ok {
		pushPermissionSnapshot(componentID, comp.Encoders, snap)
	}
}

// checkHubPermission enforces fine-grained grants at Hub routing layer.
// Only agent-like microVM sources are checked; host components (daemon-internal,
// web-portal, hub, store) remain governed by ACLs alone.
func checkHubPermission(source, command string) (allowed bool, deniedReason string) {
	if !permissions.IsCapabilityCommand(command) {
		return true, ""
	}
	if !shouldReceivePermissionSnapshot(source) {
		return true, ""
	}

	if hubPermissionAllowed(source, command) {
		return true, ""
	}

	go emitPermissionRequest(source, command, "denied at hub routing")
	return false, "ERR_PERMISSION_DENIED"
}

func hubPermissionAllowed(source, command string) bool {
	permSnapMu.RLock()
	snap, cached := permSnapshots[source]
	permSnapMu.RUnlock()

	if !cached {
		var ok bool
		snap, ok = fetchPermissionSnapshotFromStore(source)
		if !ok {
			return permissions.HasGrant(permBootstrap, source, command)
		}
	}
	return snap.AllowedTools[command]
}

func emitPermissionRequest(subject, capability, context string) {
	msg := Message{
		Source:      "hub",
		Destination: "store",
		Command:     "permission.request",
		Payload: map[string]interface{}{
			"subject":    subject,
			"capability": capability,
			"context":    context,
		},
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	registeredMutex.RLock()
	storeComp, ok := registered["store"]
	registeredMutex.RUnlock()
	if !ok || storeComp.Encoders == nil {
		return
	}
	storeComp.Encoders.Mutex.Lock()
	_ = storeComp.Encoders.Encoder.Encode(msg)
	storeComp.Encoders.Mutex.Unlock()
}

// maybeInvalidatePermissionsFromReply triggers snapshot refresh when Store acknowledges
// a grant/revoke/visibility mutation via RPC reply (Portal path does not re-enter hub read loop).
func maybeInvalidatePermissionsFromReply(reply Message) {
	switch reply.Command {
	case "permission.granted", "permission.revoked", "visibility.set":
		handlePermissionInvalidationEvent(reply)
	}
}

func handlePermissionInvalidationEvent(msg Message) {
	payload, ok := msg.Payload.(map[string]interface{})
	if !ok {
		return
	}
	subject, _ := payload["subject"].(string)
	if subject == "" {
		invalidateAllPermissionSnapshots()
		return
	}
	if strings.HasSuffix(subject, "*") {
		invalidateMatchingPermissionSnapshots(subject)
		return
	}
	invalidatePermissionSnapshot(subject)
}

func invalidateMatchingPermissionSnapshots(pattern string) {
	registeredMutex.RLock()
	ids := make([]string, 0)
	for id := range registered {
		if shouldReceivePermissionSnapshot(id) && permissions.SubjectMatches(id, pattern) {
			ids = append(ids, id)
		}
	}
	registeredMutex.RUnlock()
	for _, id := range ids {
		invalidatePermissionSnapshot(id)
	}
}

func invalidateAllPermissionSnapshots() {
	registeredMutex.RLock()
	ids := make([]string, 0)
	for id := range registered {
		if shouldReceivePermissionSnapshot(id) {
			ids = append(ids, id)
		}
	}
	registeredMutex.RUnlock()
	for _, id := range ids {
		invalidatePermissionSnapshot(id)
	}
}

func shouldReceivePermissionSnapshot(componentID string) bool {
	if permissions.IsMicroVMSourcePublic(componentID) {
		return true
	}
	for _, p := range []string{"project-manager", "coder", "tester", "agent"} {
		if componentID == p || (len(componentID) > len(p) && componentID[:len(p)+1] == p+"-") {
			return true
		}
	}
	return false
}