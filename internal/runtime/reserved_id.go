package runtime

import (
	"errors"
	"fmt"
	"strings"

	"AegisClaw/internal/channelid"
	"AegisClaw/internal/sandbox"
)

// ErrReservedRoleID is returned when ensure_role would launch a VM whose id
// the caller chose from a reserved namespace. The guest's component id equals
// that VM id, so the name must be refused before GetVMStatus or StartVM.
// Reserved names must never fall back to agent.img.
var ErrReservedRoleID = errors.New("reserved role id")

// ErrRoleNotAllowed is returned when ensure_role's role is not an exact
// allowlist entry. "coder-1" is not the role "coder".
var ErrRoleNotAllowed = errors.New("role not allowed")

// ErrInvalidVMID is sandbox.ErrInvalidVMID. CheckRoleAgentID and StartVM
// return it for ids that fail ValidateVMID.
var ErrInvalidVMID = sandbox.ErrInvalidVMID

// maxVMIDBytes is ReservedVMIDReason's length cap. ensure_role and StartVM
// use ValidateVMID, which caps ids at 64 bytes.
const maxVMIDBytes = 128

// reservedVMIDPrefixes are matched on a dash boundary after lowercasing:
// the id equals P, or it starts with P+"-".
//
// store, court (court-scribe and court-persona-*; personas are started by
// StartCourtSystem — EnsureCourtPersona currently has no callers), network-boundary, web-portal, and builder are base
// components. memory-<session> and agent-<session> belong to
// StartPairedAgentAndMemory. The bare roleType "agent" or "" uses that paired
// path and does not compose an id, so CheckRoleAgentID does not apply this
// list to it.
//
// The host-only set is a superset of cmd/aegishub isReservedHubID
// (hub-perm-fetch and hub-perm-fetch-*). This tree has no isReservedVsockID.
// "hub" is dash-bounded here, so it also covers hub-perm-fetch*.
// guestBridgeHostOnlyID uses HostOnlyVMID, which is the narrower
// case-sensitive subset (it must not reject hub-perm-fetcher).
var reservedVMIDPrefixes = []string{
	"store",
	"court",
	"network-boundary",
	"web-portal",
	"builder",
	"memory",
	"agent",
	"hub",
	"aegishub",
	"hub-perm-fetch",
	"daemon",
	"aegis-cli-internal",
	"channel-facilitator",
}

// ReservedVMIDReason reports whether id must not be chosen as a VM id.
// reserved is true for a reserved prefix, a character outside [A-Za-z0-9._-],
// an empty dash-segment (including the composed form of an empty roleType
// plus a channel hint, "-<hint>"), an empty id, or an id longer than 128 bytes.
// Comparison is case-insensitive. "storefront" does not match "store";
// "store-x" does. reason is empty when reserved is false.
func ReservedVMIDReason(id string) (reserved bool, reason string) {
	if id == "" {
		return true, "empty id"
	}
	if len(id) > maxVMIDBytes {
		return true, "id longer than 128 bytes"
	}
	if !vmIDCharsOK(id) {
		return true, "invalid characters"
	}
	if hasEmptyIDSegment(id) {
		return true, "empty id segment"
	}
	if p, ok := reservedPrefix(id); ok {
		return true, "reserved prefix " + p
	}
	return false, ""
}

// reservedPrefix matches P or P+"-" case-insensitively, longest prefix wins.
func reservedPrefix(id string) (string, bool) {
	lower := strings.ToLower(id)
	matched := ""
	for _, p := range reservedVMIDPrefixes {
		if lower == p || strings.HasPrefix(lower, p+"-") {
			if len(p) > len(matched) {
				matched = p
			}
		}
	}
	if matched == "" {
		return "", false
	}
	return matched, true
}

// HostOnlyVMID is the guest-bridge host-only subset: hub, hub-perm-fetch*,
// daemon*, aegis-cli-internal*, and channel-facilitator*. Match is
// case-sensitive. "hub" is not a dash-boundary here, so hub-perm-fetcher
// is not host-only. store, court, and the other reserved prefixes are not
// host-only; those guests register on their own bridge.
func HostOnlyVMID(id string) bool {
	switch id {
	case "hub", "hub-perm-fetch", "daemon", "aegis-cli-internal", "channel-facilitator":
		return true
	}
	return strings.HasPrefix(id, "hub-perm-fetch-") ||
		strings.HasPrefix(id, "daemon-") ||
		strings.HasPrefix(id, "aegis-cli-internal-") ||
		strings.HasPrefix(id, "channel-facilitator-")
}

// ValidateVMID is the strict id rule shared with sandbox path joins.
// See sandbox.ValidateVMID.
func ValidateVMID(id string) error {
	return sandbox.ValidateVMID(id)
}

// ValidateChannelID is the channel id rule. The allowlist and length cap
// live in channelid so the Store guest can enforce the same rule without
// importing this package.
func ValidateChannelID(id string) error {
	return channelid.ValidateChannelID(id)
}

// roleAllowed is channelid's ensure_role allowlist. The list lives there so
// MaxChannelIDLen is computed from the same set the Store validates against.
func roleAllowed(roleType string) bool {
	return channelid.RoleAllowed(roleType)
}

// EnsureRoleRefused reports a CheckRoleAgentID refusal (reserved, not on the
// allowlist, or ValidateVMID). Boot and image errors are not refusals.
func EnsureRoleRefused(err error) bool {
	return errors.Is(err, ErrReservedRoleID) ||
		errors.Is(err, ErrRoleNotAllowed) ||
		errors.Is(err, ErrInvalidVMID)
}

// ComposeRoleAgentID is the VM id EnsureRoleAgent assigns for a generic role.
// An empty channel hint yields roleType alone; otherwise roleType + "-" + hint.
// The paired path (roleType "agent" or "") does not use this id.
func ComposeRoleAgentID(roleType, channelHint string) string {
	if channelHint == "" {
		return roleType
	}
	return roleType + "-" + channelHint
}

// CheckRoleAgentID is the shared ensure_role gate. It returns the composed id
// when roleType is on the allowlist, the composed id passes ValidateVMID, and
// neither the id nor the role is reserved. roleType "agent" or "" returns
// ("", nil) after the paired ids (agent-<session>, memory-<session>) pass
// ValidateVMID; those calls use StartPairedAgentAndMemory.
// Callers must refuse a non-nil error before starting a VM, a guest bridge,
// or a channel membership. Reserved names never fall back to agent.img.
func CheckRoleAgentID(roleType, channelHint string) (string, error) {
	if roleType == "agent" || roleType == "" {
		sid := channelHint
		if sid == "" {
			sid = "temp-" + roleType
		}
		for _, id := range []string{"agent-" + sid, "memory-" + sid} {
			if err := ValidateVMID(id); err != nil {
				return id, err
			}
		}
		return "", nil
	}
	id := ComposeRoleAgentID(roleType, channelHint)
	// Prefix before the strict charset so "Agent" stays a reserved-id error
	// rather than an uppercase rejection. StartVM does not apply this list.
	if p, ok := reservedPrefix(id); ok {
		return id, fmt.Errorf("%w: reserved prefix %s", ErrReservedRoleID, p)
	}
	if p, ok := reservedPrefix(roleType); ok {
		return id, fmt.Errorf("%w: reserved prefix %s", ErrReservedRoleID, p)
	}
	if !roleAllowed(roleType) {
		return id, fmt.Errorf("%w: %q", ErrRoleNotAllowed, roleType)
	}
	if err := ValidateVMID(id); err != nil {
		return id, err
	}
	return id, nil
}

func vmIDCharsOK(id string) bool {
	for i := 0; i < len(id); i++ {
		c := id[i]
		switch {
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9':
		case c == '.' || c == '_' || c == '-':
		default:
			return false
		}
	}
	return true
}

func hasEmptyIDSegment(id string) bool {
	for _, seg := range strings.Split(id, "-") {
		if seg == "" {
			return true
		}
	}
	return false
}
