package hublease

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// CID lease is in-memory only. Only fill: vsock handshake after verify+roster
// via CASFillLease (empty-or-same, never overwrite different pub).
// git-connect never writes. Hangup must not UnleaseCID. cid.lease RPC is
// ERR_UNAUTHORIZED. StopVM CAS UnleaseCID (persistent daemon) deletes the
// live lease. No file ingest.

var (
	lease  sync.Map // uint32 CID -> base64 pubkey
	fileMu sync.Mutex
)

func Reset() {
	lease = sync.Map{}
}

func StoreLease(cid uint32, pub string) {
	pub = strings.TrimSpace(pub)
	if pub == "" {
		return
	}
	lease.Store(cid, pub)
}

// StoreLeaseIfAbsentOrSame is handshake CAS fill: if lease empty, Store;
// if lease==pub, no-op; if lease holds a different pub, do not overwrite.
func StoreLeaseIfAbsentOrSame(cid uint32, pub string) bool {
	pub = strings.TrimSpace(pub)
	if cid == 0 || pub == "" {
		return false
	}
	actual, loaded := lease.LoadOrStore(cid, pub)
	if !loaded {
		return true
	}
	cur, _ := actual.(string)
	return strings.TrimSpace(cur) == pub
}

// StoreLeaseCAS is StoreLeaseIfAbsentOrSame. Daemon cid.lease RPC is not fill.
func StoreLeaseCAS(cid uint32, pub string) bool {
	return StoreLeaseIfAbsentOrSame(cid, pub)
}

func CASFillLease(cid uint32, pub string) bool {
	return StoreLeaseIfAbsentOrSame(cid, pub)
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
