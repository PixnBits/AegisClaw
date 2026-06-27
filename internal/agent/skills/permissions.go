package skills

import "AegisClaw/internal/permissions"

// FilterFromSnapshot converts a Hub permission snapshot into a local PermissionFilter.
func FilterFromSnapshot(snap permissions.Snapshot) PermissionFilter {
	return PermissionFilter{
		AllowedTools:        snap.AllowedTools,
		VisibleTools:        snap.VisibleTools,
		RequestableTools:    snap.RequestableTools,
		CanDiscoverRegistry: snap.CanDiscover,
		Enforce:             true,
	}
}

// SnapshotFromMap decodes a hub wire payload into a Snapshot.
func SnapshotFromMap(m map[string]interface{}) permissions.Snapshot {
	snap := permissions.Snapshot{}
	if v, ok := m["subject"].(string); ok {
		snap.Subject = v
	}
	if v, ok := m["allowed_tools"].(map[string]interface{}); ok {
		snap.AllowedTools = boolMapFromInterface(v)
	}
	if v, ok := m["visible_tools"].(map[string]interface{}); ok {
		snap.VisibleTools = boolMapFromInterface(v)
	}
	if v, ok := m["requestable_tools"].(map[string]interface{}); ok {
		snap.RequestableTools = boolMapFromInterface(v)
	}
	if v, ok := m["can_discover_registry"].(bool); ok {
		snap.CanDiscover = v
	}
	if v, ok := m["version"].(float64); ok {
		snap.Version = int64(v)
	}
	if v, ok := m["timestamp"].(string); ok {
		snap.Timestamp = v
	}
	return snap
}

func boolMapFromInterface(m map[string]interface{}) map[string]bool {
	out := make(map[string]bool, len(m))
	for k, v := range m {
		if b, ok := v.(bool); ok {
			out[k] = b
		}
	}
	return out
}

// FilterFromPermissions builds a PermissionFilter from durable Store state.
func FilterFromPermissions(state *permissions.State, subjectID string, allCapabilities []string) PermissionFilter {
	f := permissions.BuildFilter(state, subjectID, allCapabilities)
	return PermissionFilter{
		AllowedTools:        f.AllowedTools,
		VisibleTools:        f.VisibleTools,
		RequestableTools:    f.RequestableTools,
		CanDiscoverRegistry: f.CanDiscover,
		Enforce:             f.Enforce,
	}
}
