package channelfacilitator

import (
	"strings"

	"AegisClaw/internal/channeldata"
	"AegisClaw/internal/collab"
)

// SelectNextRecipient picks the next turn recipient using round-robin, mention boosts, and starvation.
func SelectNextRecipient(members []map[string]interface{}, rrIndex int, latestContent string, settings channeldata.TurnSettings) (recipient string, newIndex int, boosts map[string]interface{}) {
	if len(members) == 0 {
		return "", rrIndex, nil
	}
	roles := dedupeMemberRoles(members)
	if len(roles) == 0 {
		return "", rrIndex, nil
	}
	boosts = map[string]interface{}{}
	mentionBoost := settings.MentionBoostPositions
	if mentionBoost <= 0 {
		mentionBoost = channeldata.DefaultTurnSettings.MentionBoostPositions
	}
	maxBoosts := settings.MaxMentionBoostsPerCycle
	if maxBoosts <= 0 {
		maxBoosts = channeldata.DefaultTurnSettings.MaxMentionBoostsPerCycle
	}
	starvation := settings.StarvationCycles
	if starvation <= 0 {
		starvation = channeldata.DefaultTurnSettings.StarvationCycles
	}

	order := make([]string, len(roles))
	copy(order, roles)
	if rrIndex < 0 || rrIndex >= len(order) {
		rrIndex = 0
	}
	// Rotate so rrIndex is head.
	rotated := append(append([]string{}, order[rrIndex:]...), order[:rrIndex]...)

	// Apply mention boosts to mentioned members still under per-cycle cap.
	for i, role := range rotated {
		for _, m := range members {
			if channeldata.MemberRole(m) != role {
				continue
			}
			left, _ := m["mention_boosts_left"].(float64)
			if int(left) >= maxBoosts {
				continue
			}
			if collab.IsMentioned(role, latestContent) {
				target := i - mentionBoost
				if target < 0 {
					target = 0
				}
				if target != i {
					rotated[i], rotated[target] = rotated[target], rotated[i]
					boosts[role] = map[string]interface{}{
						"applied":  true,
						"positions": mentionBoost,
					}
				}
			}
			break
		}
	}

	// Starvation: force member with highest cycles_since_turn to front.
	bestRole := ""
	bestCycles := -1
	for _, m := range members {
		cycles := intFromMember(m["cycles_since_turn"])
		if cycles >= starvation && cycles > bestCycles {
			bestCycles = cycles
			bestRole = channeldata.MemberRole(m)
		}
	}
	if bestRole != "" {
		for i, role := range rotated {
			if role == bestRole && i > 0 {
				copy(rotated[1:i+1], rotated[0:i])
				rotated[0] = bestRole
				boosts[bestRole] = map[string]interface{}{"starvation": true, "cycles": bestCycles}
				break
			}
		}
	}

	recipient = rotated[0]
	for i, role := range roles {
		if role == recipient {
			newIndex = (i + 1) % len(roles)
			break
		}
	}
	return recipient, newIndex, boosts
}

// dedupeMemberRoles collapses duplicate membership rows (re-invites) to one slot per role.
func dedupeMemberRoles(members []map[string]interface{}) []string {
	seen := make(map[string]bool)
	roles := make([]string, 0, len(members))
	for _, m := range members {
		r := channeldata.MemberRole(m)
		if r == "" {
			continue
		}
		key := r
		if r == "project-manager" || strings.HasPrefix(r, "project-manager-") {
			key = "project-manager"
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		roles = append(roles, r)
	}
	return roles
}

func intFromMember(v interface{}) int {
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