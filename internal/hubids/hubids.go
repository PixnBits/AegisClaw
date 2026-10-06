// Package hubids holds hub client id families that more than one component
// must agree on. The hub, the daemon's guest hub bridge and VM id validation
// all read these lists, so a new family is reserved everywhere at once.
package hubids

import "strings"

// EphemeralClientFamilies are the host client families the hub serves with
// its one-shot RPC loop. They are host processes only: never a guest
// registration, never a VM id.
var EphemeralClientFamilies = []string{
	"aegis-daemon-temp",
	"daemon-temp",
	"aegis-cli-internal",
}

// HostClientFamilies are every host-only client family, including the
// ephemeral ones.
var HostClientFamilies = append([]string{
	"daemon",
	"daemon-internal",
	"channel-facilitator",
}, EphemeralClientFamilies...)

// InFamily reports whether id is fam or fam-<suffix>.
func InFamily(id, fam string) bool {
	return id == fam || (strings.HasPrefix(id, fam+"-") && len(id) > len(fam)+1)
}

// IsEphemeralClient reports whether id belongs to an ephemeral client family.
func IsEphemeralClient(id string) bool {
	for _, fam := range EphemeralClientFamilies {
		if InFamily(id, fam) {
			return true
		}
	}
	return false
}

// IsHostClient reports whether id belongs to a host-only client family.
func IsHostClient(id string) bool {
	for _, fam := range HostClientFamilies {
		if InFamily(id, fam) {
			return true
		}
	}
	return false
}
