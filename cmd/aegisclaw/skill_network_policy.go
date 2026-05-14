package main

import (
	"github.com/PixnBits/AegisClaw/internal/sandbox"
)

func skillNetworkPolicy(skillName string, env *runtimeEnv) sandbox.NetworkPolicy {
	safeDefault := sandbox.NetworkPolicy{
		NoNetwork:   true,
		DefaultDeny: true,
	}
	if env == nil || env.ProposalStore == nil {
		return safeDefault
	}

	summaries, err := env.ProposalStore.List()
	if err != nil {
		return safeDefault
	}
	for _, s := range summaries {
		p, err := env.ProposalStore.Get(s.ID)
		if err != nil || p == nil {
			continue
		}
		if p.TargetSkill != skillName || !p.IsApproved() {
			continue
		}

		if p.Capabilities == nil || !p.Capabilities.Network {
			return safeDefault
		}

		np := sandbox.NetworkPolicy{DefaultDeny: true}
		if p.NetworkPolicy != nil {
			np.AllowedHosts = append([]string(nil), p.NetworkPolicy.AllowedHosts...)
			np.AllowedPorts = append([]uint16(nil), p.NetworkPolicy.AllowedPorts...)
		}
		return np
	}

	return safeDefault
}
