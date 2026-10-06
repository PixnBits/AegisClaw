// Package channelid is the channel id rule shared by the Store, the portal,
// and ensure_role. It does not import the orchestrator or sandbox, so the
// Store guest binary can use it.
package channelid

import (
	"fmt"
	"regexp"
	"strings"
)

// vmIDMaxLen is sandbox.ValidateVMID's cap (maxStrictVMIDBytes).
// role+"-"+channel must fit in it for every allowlisted role.
const vmIDMaxLen = 64

// allowedEnsureRoles are the only roleType values ensure_role may compose
// into a VM id. The match is exact: no prefix and no case fold.
//
//	coder, tester, ciso, security-architect, architect, efficiency,
//	user-advocate — cmd/project-manager extractRolesFromText
//	project-manager — aegis pm goal and portal goal.submit
//	researcher — collab.NormalizeMemberRole, portal harness plan text,
//	and the hub permission subject researcher*
//
// "agent" and "" are the paired agent+memory path, not composed roles.
// general and sdlc-* are only named in a comment. analyst and critic are
// team-CLI labels. senior-coder is a Court persona slug; the member label
// normalizes to coder.
//
// MaxChannelIDLen is 64 minus the longest name minus one byte for the
// joining hyphen. The longest name is "security-architect" (18), so the cap
// is 45. "agent-" and "memory-" are shorter than that role, so a channel of
// this length still composes a valid paired VM id.
var allowedEnsureRoles = map[string]struct{}{
	"coder":              {},
	"tester":             {},
	"ciso":               {},
	"security-architect": {},
	"architect":          {},
	"efficiency":         {},
	"user-advocate":      {},
	"project-manager":    {},
	"researcher":         {},
}

// MaxChannelIDLen is the longest channel id that still passes ValidateVMID
// when composed as role+"-"+id for every role in allowedEnsureRoles.
var MaxChannelIDLen = func() int {
	longest := 0
	for role := range allowedEnsureRoles {
		if len(role) > longest {
			longest = len(role)
		}
	}
	return vmIDMaxLen - longest - 1
}()

// channelIDPattern is the ValidateVMID charset. Uppercase is refused, not
// lowercased. "--" and a trailing "-" are rejected separately; the class
// itself allows a single hyphen.
var channelIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// ValidateChannelID reports whether id may be stored as a channel id.
// Charset matches sandbox.ValidateVMID (^[a-z][a-z0-9-]*$, no "--", no
// trailing "-"). Length is MaxChannelIDLen so role+"-"+id stays within the
// 64-byte VM id cap. "main" and generated "plan-*" ids pass. Callers must
// not rewrite or slugify the caller's id.
func ValidateChannelID(id string) error {
	if id == "" || len(id) > MaxChannelIDLen ||
		!channelIDPattern.MatchString(id) ||
		strings.HasSuffix(id, "-") || strings.Contains(id, "--") {
		return fmt.Errorf("invalid channel id: must match ^[a-z][a-z0-9-]*$ and be <= %d chars", MaxChannelIDLen)
	}
	return nil
}

// RoleAllowed reports whether roleType is an exact ensure_role allowlist entry.
func RoleAllowed(roleType string) bool {
	_, ok := allowedEnsureRoles[roleType]
	return ok
}

// Roles returns the ensure_role allowlist. Order is not significant.
func Roles() []string {
	out := make([]string, 0, len(allowedEnsureRoles))
	for role := range allowedEnsureRoles {
		out = append(out, role)
	}
	return out
}
