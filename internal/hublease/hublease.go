package hublease

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// CID lease is in-memory. git-connect never writes it. Helper/git-connect hangup
// and VM guest hangup must not UnleaseCID. leasePubForCID must not reload
// AEGIS_GIT_CID_KEYS on miss (file fail-open). Handshake/StartVM StoreLease
// fills the map; a reused CID stays empty until a new Store.
//
// VM destroy (orchestrator StopVM) sends cid.unlease {cid, public_key} over the
// persistent daemon Hub connection. CAS: unlease only if lease[cid]==expectedPub
// else no-op. Hub then deletes that CID row from AEGIS_GIT_CID_KEYS.

var (
	lease  sync.Map // uint32 CID -> base64 pubkey
	closed sync.Map // uint32 CID -> pubkey poisoned by successful CAS UnleaseCID
	fileMu sync.Mutex
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

// UnleaseCID is VM death CAS: drop the in-memory lease only if it still holds
// expectedPub. Git-connect close and guest hangup must not call this.
func UnleaseCID(cid uint32, expectedPub string) bool {
	expectedPub = strings.TrimSpace(expectedPub)
	if expectedPub == "" {
		return false
	}
	v, ok := lease.Load(cid)
	if !ok {
		return false
	}
	pub, _ := v.(string)
	if strings.TrimSpace(pub) != expectedPub {
		return false
	}
	closed.Store(cid, expectedPub)
	lease.Delete(cid)
	return true
}

// MergeCIDKey writes cid (decimal) → pub into the JSON CID keys file.
func MergeCIDKey(path string, cid uint32, pub string) {
	pub = strings.TrimSpace(pub)
	if path == "" || cid == 0 || pub == "" {
		return
	}
	fileMu.Lock()
	defer fileMu.Unlock()
	m := map[string]string{}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &m)
	}
	m[strconv.FormatUint(uint64(cid), 10)] = pub
	b, err := json.Marshal(m)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return
	}
	_ = os.WriteFile(path, b, 0600)
}

// DeleteCIDKeyIf removes the CID row only if it still holds expectedPub.
func DeleteCIDKeyIf(path string, cid uint32, expectedPub string) bool {
	expectedPub = strings.TrimSpace(expectedPub)
	if path == "" || cid == 0 || expectedPub == "" {
		return false
	}
	fileMu.Lock()
	defer fileMu.Unlock()
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	m := map[string]string{}
	if json.Unmarshal(b, &m) != nil {
		return false
	}
	key := strconv.FormatUint(uint64(cid), 10)
	if strings.TrimSpace(m[key]) != expectedPub {
		return false
	}
	delete(m, key)
	out, err := json.Marshal(m)
	if err != nil {
		return false
	}
	return os.WriteFile(path, out, 0600) == nil
}
