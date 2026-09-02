package hublease

import (
	"strings"
	"sync"
)

// CID lease is in-memory. git-connect never writes it. Helper/git-connect hangup
// must not UnleaseCID. Production unleases on VM Hub vsock session close
// (cmd/aegishub handleConnection, source != git-remote-hub) and on orchestrator
// StopVM. UnleaseCID poisons leftover AEGIS_GIT_CID_KEYS rows for the same
// CID+pub until overwritten with a different pub or the row is removed.

var (
	lease  sync.Map // uint32 CID -> base64 pubkey
	closed sync.Map // uint32 CID -> pubkey poisoned by UnleaseCID
)

func Reset() {
	lease = sync.Map{}
	closed = sync.Map{}
}

func StoreLease(cid uint32, pub string) {
	pub = strings.TrimSpace(pub)
	if pub == "" {
		return
	}
	lease.Store(cid, pub)
	closed.Delete(cid)
}

func LoadLease(cid uint32) (string, bool) {
	v, ok := lease.Load(cid)
	if !ok {
		return "", false
	}
	pub, _ := v.(string)
	pub = strings.TrimSpace(pub)
	return pub, pub != ""
}

func ClosedPub(cid uint32) (string, bool) {
	v, ok := closed.Load(cid)
	if !ok {
		return "", false
	}
	pub, _ := v.(string)
	return pub, true
}

func ClearClosed(cid uint32) {
	closed.Delete(cid)
}

// UnleaseCID is VM death: drop the in-memory lease and poison leftover file
// rows for the same CID+pub. Git-connect close must not call this.
func UnleaseCID(cid uint32) {
	if v, ok := lease.Load(cid); ok {
		closed.Store(cid, v)
	}
	lease.Delete(cid)
}
