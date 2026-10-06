package main

import (
	"bufio"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"AegisClaw/internal/collab"
	"AegisClaw/internal/hubgit"
	"AegisClaw/internal/hubids"
	"AegisClaw/internal/hublease"
	"AegisClaw/internal/transport/hubclient"
	"AegisClaw/internal/unixsock"
	"github.com/mdlayher/vsock"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var hubSocketPath = "~/.aegis/hub.sock"

var registered = make(map[string]*RegisteredComponent)
var aclRules []ACLRule
var registeredMutex sync.RWMutex
var tempConnCounter int = 0
var tempConnMutex sync.Mutex

type pendingWaiter struct {
	dest    string
	command string
	ch      chan Message
}

// pendingRPC correlates synchronous hub RPC replies (daemon ephemeral → agent/store).
// Waiters are keyed by requester ID and only complete on a reply from the callee
// (source == dest). A channel.turn destined at the requester must not complete
// an in-flight llm.call (that race dropped guest Ollama responses).
var pendingRPC = struct {
	sync.Mutex
	ch map[string]*pendingWaiter
}{ch: make(map[string]*pendingWaiter)}

// isEphemeralHubClient reports the host clients served by ephemeralHubRPCLoop
// (daemon one-shot RPCs and the CLI). The families live in hubids so the
// guest bridge and VM id checks reserve the same set.
func isEphemeralHubClient(id string) bool {
	return hubids.IsEphemeralClient(id)
}

// isReservedHubID reports ids that are reserved on every transport.
// Host-only ids are reserved only on VM transports; see reservedIDReason.
func isReservedHubID(id string) bool {
	_, ok := reservedIDReason(id, false)
	return ok
}

// reservedIDReason reports why id must not register.
// Always reserved: "hub" and the hub's snapshot RPC waiter ids
// (hub-perm-fetch, hub-perm-fetch-*). Those waiters never register.
// Host-only ids are reserved only when vmTransport is set (RemoteAddr is
// *vsock.Addr). Production guests, including store, reach the hub through
// the daemon's guest hub bridge, which dials this UNIX socket, so they are
// not a VM transport. Host client ids (daemon*, aegis-daemon-temp*,
// aegis-cli-internal*, channel-facilitator*) and the base component ids
// (store, network-boundary, web-portal, aegishub) are reserved on vsock.
// Malformed members of the host client id families are reserved on every
// transport. Agent, memory, coder, tester, project-manager, and court ids
// are not reserved.
func reservedIDReason(id string, vmTransport bool) (string, bool) {
	switch {
	case id == "hub":
		return "hub", true
	case id == "hub-perm-fetch" || strings.HasPrefix(id, "hub-perm-fetch-"):
		return "hub-perm-fetch", true
	case isHostIDLookAlike(id):
		return "host id prefix", true
	}
	if !vmTransport {
		return "", false
	}
	switch id {
	case "store", "network-boundary", "web-portal", "aegishub":
		return "host-only", true
	}
	// Host process ids: every hubids host client family (which includes
	// everything isEphemeralHubClient serves), by bare prefix.
	for _, fam := range hubids.HostClientFamilies {
		if strings.HasPrefix(id, fam) {
			return "host-only", true
		}
	}
	return "", false
}

// isHostIDLookAlike reports an id that starts with a host client family
// name but is not that name or name-<suffix>. Refused on every transport.
// "daemon" itself is skipped: guest ids such as "daemonset-1" are not
// host clients off vsock.
func isHostIDLookAlike(id string) bool {
	for _, fam := range hubids.HostClientFamilies {
		if fam == "daemon" || !strings.HasPrefix(id, fam) {
			continue
		}
		if !hubids.InFamily(id, fam) {
			return true
		}
	}
	return false
}

// isVMHubTransport reports a raw AF_VSOCK peer. Those guests are bound by
// CID lease, not by host uid. The guest hub bridge is a UNIX connection.
func isVMHubTransport(conn net.Conn) bool {
	if conn == nil || conn.RemoteAddr() == nil {
		return false
	}
	_, ok := conn.RemoteAddr().(*vsock.Addr)
	return ok
}

// hubOriginalUser is the invoking user: SUDO_USER when the hub was started
// via sudo, otherwise the current user. Same rule as the daemon control socket.
func hubOriginalUser() (*user.User, error) {
	if sudoUser := os.Getenv("SUDO_USER"); sudoUser != "" {
		return user.Lookup(sudoUser)
	}
	return user.Current()
}

// hubOriginalUIDVal is filled once. The invoking user does not change for
// the life of the process; looking it up on every connection calls user.Lookup.
var (
	hubOriginalUIDOnce sync.Once
	hubOriginalUIDVal  = -1
)

func hubOriginalUID() int {
	hubOriginalUIDOnce.Do(func() {
		u, err := hubOriginalUser()
		if err != nil || u == nil {
			return
		}
		id, err := strconv.Atoi(u.Uid)
		if err != nil {
			return
		}
		hubOriginalUIDVal = id
	})
	return hubOriginalUIDVal
}

// getHubPeerUID returns the peer euid of a UNIX connection via SO_PEERCRED.
// Non-UNIX conns (vsock, net.Pipe) return ok=false; callers must not treat
// that as a UNIX peer. Do not use (*net.UnixConn).File: it clears O_NONBLOCK.
func getHubPeerUID(conn net.Conn) (int, bool) {
	return unixsock.PeerUID(conn)
}

// hubPeerUIDLookup is the SO_PEERCRED lookup handleConnection uses.
// Tests replace it to present a foreign peer and restore it with t.Cleanup.
// Production must leave it as getHubPeerUID.
var hubPeerUIDLookup = getHubPeerUID

// authorizeHubPeer allows uid 0, the hub's own euid, or the original user.
// !peerOK denies: a UNIX connection without SO_PEERCRED must not fail open.
func authorizeHubPeer(peerUID int, peerOK bool, selfUID, originalUID int) bool {
	if !peerOK {
		return false
	}
	if peerUID == 0 || peerUID == selfUID {
		return true
	}
	return originalUID >= 0 && peerUID == originalUID
}

// listenUnixSocket0600 listens so the socket inode is created at 0600.
// umask 0177 makes the kernel mode 0777&^0177 = 0600. There is no window
// where the socket is 0666 before a later chmod. The umask lock is
// process-wide and shared with the daemon (unixsock.ListenPrivate).
func listenUnixSocket0600(socket string) (net.Listener, error) {
	return unixsock.ListenPrivate("unix", socket)
}

// listenHubUnixSocket creates the hub UNIX socket at mode 0600 and chowns it
// to the original invoking user. Chown errors are ignored when not root
// (the socket is already owned by the current user).
func listenHubUnixSocket(socket string) (net.Listener, error) {
	if err := os.MkdirAll(filepath.Dir(socket), 0700); err != nil {
		return nil, err
	}
	_ = os.Remove(socket)
	ln, err := listenUnixSocket0600(socket)
	if err != nil {
		return nil, err
	}
	chownHubSocketToOriginalUser(socket)
	if err := os.Chmod(socket, 0600); err != nil {
		ln.Close()
		return nil, fmt.Errorf("chmod %s 0600: %w", socket, err)
	}
	return ln, nil
}

func chownHubSocketToOriginalUser(path string) {
	u, err := hubOriginalUser()
	if err != nil || u == nil {
		if os.Geteuid() == 0 {
			log.Printf("hub socket chown: original user: %v", err)
		}
		return
	}
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		if os.Geteuid() == 0 {
			log.Printf("hub socket chown: uid: %v", err)
		}
		return
	}
	gid := uid
	if g, gerr := strconv.Atoi(u.Gid); gerr == nil {
		gid = g
	}
	if err := os.Chown(path, uid, gid); err != nil && os.Geteuid() == 0 {
		log.Printf("hub socket chown %s: %v", path, err)
	}
}

func registerPendingRPC(requesterID, dest, command string) chan Message {
	w := &pendingWaiter{
		dest:    dest,
		command: command,
		ch:      make(chan Message, 1),
	}
	pendingRPC.Lock()
	pendingRPC.ch[requesterID] = w
	pendingRPC.Unlock()
	return w.ch
}

func clearPendingRPC(requesterID string) {
	pendingRPC.Lock()
	delete(pendingRPC.ch, requesterID)
	pendingRPC.Unlock()
}

func deliverPendingRPC(msg Message) bool {
	pendingRPC.Lock()
	w, ok := pendingRPC.ch[msg.Destination]
	pendingRPC.Unlock()
	if !ok || w == nil {
		return false
	}
	if w.dest != "" && msg.Source != w.dest && msg.Source != "hub" {
		return false
	}
	// Pushes such as channel.turn must not complete an unrelated waiter.
	// permission.snapshot is both a Hub→agent push and Store's RPC reply
	// command, so a waiter that requested that exact command still receives it.
	if hubclient.IsUnsolicitedCommand(msg.Command) {
		if w == nil || msg.Command != w.command {
			return false
		}
	}
	select {
	case w.ch <- msg:
		return true
	default:
		return false
	}
}

type ComponentEncoders struct {
	Encoder *json.Encoder
	Decoder *json.Decoder
	Mutex   sync.Mutex
}

type Message struct {
	Source      string      `json:"source"`
	Destination string      `json:"destination"`
	Command     string      `json:"command"`
	Payload     interface{} `json:"payload"`
	Timestamp   string      `json:"timestamp"`
	Signature   string      `json:"signature"`
}

// wireMessage preserves the original JSON payload bytes so Ed25519 verification
// survives decode/encode round-trips (nested map key reordering broke store replies
// for permission.* after appendAuditForStateChangeIfNeeded wrapped list payloads).
type wireMessage struct {
	Source      string          `json:"source"`
	Destination string          `json:"destination"`
	Command     string          `json:"command"`
	Payload     json.RawMessage `json:"payload"`
	Timestamp   string          `json:"timestamp"`
	Signature   string          `json:"signature"`
}

type RegisteredComponent struct {
	ID        string
	PublicKey ed25519.PublicKey
	Encoders  *ComponentEncoders
	Version   string
}

type ACLRule struct {
	Source      string   `yaml:"source"`
	Destination string   `yaml:"destination"`
	Commands    []string `yaml:"commands"`
}

type ACLConfig struct {
	Rules []ACLRule `yaml:"rules"`
}

func expandPath(path string) string {
	if path[:2] == "~/" {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	return path
}

var aclFilePath string
var lastACLModTime time.Time

func findACLFile() string {
	if p := os.Getenv("AEGIS_ACL_FILE"); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}

	candidates := []string{
		"config/acls.yaml",
		"./config/acls.yaml",
		filepath.Join(filepath.Dir(os.Args[0]), "config/acls.yaml"),
	}

	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(wd, "config/acls.yaml"))
	}

	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func loadACL() {
	path := findACLFile()
	if path == "" {
		log.Printf("No ACL file found, using default deny-all")
		aclRules = nil
		aclFilePath = ""
		return
	}
	aclFilePath = path

	file, err := os.Open(path)
	if err != nil {
		log.Printf("Failed to open ACL %s: %v", path, err)
		return
	}
	defer file.Close()

	decoder := yaml.NewDecoder(file)
	var config ACLConfig
	if err := decoder.Decode(&config); err != nil {
		log.Printf("Failed to decode ACL: %v", err)
		return
	}
	aclRules = config.Rules
	if fi, err := os.Stat(path); err == nil {
		lastACLModTime = fi.ModTime()
	}
	log.Printf("Loaded %d ACL rules from %s", len(aclRules), path)
}

func reloadACLIfChanged() {
	if aclFilePath == "" {
		return
	}
	fi, err := os.Stat(aclFilePath)
	if err != nil {
		return
	}
	if fi.ModTime().After(lastACLModTime) {
		log.Printf("ACL file changed, reloading...")
		loadACL() // re-use the loader (it will update modtime)
	}
}

func checkACL(source, dest, cmd string) bool {
	for _, rule := range aclRules {
		if !aclIDMatch(rule.Source, source) {
			continue
		}
		if !aclIDMatch(rule.Destination, dest) {
			continue
		}
		for _, c := range rule.Commands {
			if aclMatch(c, cmd) {
				return true
			}
		}
	}
	return false
}

// aclMatch supports exact match, "*" wildcard, and suffix "*" prefix-match (e.g. "memory.*" matches "memory.get_context"; "court-persona-*" matches "court-persona-ciso").
// For commands without trailing *, exact match only (stricter than prior loose HasPrefix).
// Source and destination IDs use aclIDMatch; command patterns keep this raw prefix.
func aclMatch(pattern, value string) bool {
	if pattern == "*" || pattern == value {
		return true
	}
	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(value, prefix)
	}
	return false
}

// aclIDMatch is aclMatch for component IDs. A "<prefix>*" pattern whose prefix
// does not already end in '-' or '.' matches only value == prefix or a value
// starting with prefix+"-". "*" , "court-persona-*", and "memory.*" are unchanged.
func aclIDMatch(pattern, value string) bool {
	if pattern == "*" || pattern == value {
		return true
	}
	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		if prefix != "" && !strings.HasSuffix(prefix, "-") && !strings.HasSuffix(prefix, ".") {
			return value == prefix || strings.HasPrefix(value, prefix+"-")
		}
		return strings.HasPrefix(value, prefix)
	}
	return false
}

func verifySignature(msg Message, pubKey ed25519.PublicKey) bool {
	// Create a copy without signature
	msgCopy := msg
	msgCopy.Signature = ""
	data, err := json.Marshal(msgCopy)
	if err != nil {
		return false
	}
	sigBytes, err := base64.StdEncoding.DecodeString(msg.Signature)
	if err != nil {
		return false
	}
	return ed25519.Verify(pubKey, data, sigBytes)
}

func verifyWireSignature(wire wireMessage, pubKey ed25519.PublicKey) bool {
	sig := wire.Signature
	wire.Signature = ""
	data, err := json.Marshal(wire)
	if err != nil {
		return false
	}
	sigBytes, err := base64.StdEncoding.DecodeString(sig)
	if err != nil {
		return false
	}
	return ed25519.Verify(pubKey, data, sigBytes)
}

func decodeHubFrame(dec *json.Decoder) (Message, wireMessage, error) {
	var raw json.RawMessage
	if err := dec.Decode(&raw); err != nil {
		return Message{}, wireMessage{}, err
	}
	var wire wireMessage
	if err := json.Unmarshal(raw, &wire); err != nil {
		return Message{}, wireMessage{}, err
	}
	var msg Message
	if err := json.Unmarshal(raw, &msg); err != nil {
		return Message{}, wireMessage{}, err
	}
	return msg, wire, nil
}

func startHub(cmd *cobra.Command, args []string) {
	loadACL()

	// Hot-reload support per aegishub.md
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			reloadACLIfChanged()
		}
	}()

	socket := expandPath(hubSocketPath)
	listener, err := listenHubUnixSocket(socket)
	if err != nil {
		fmt.Printf("Failed to start AegisHub: %v\n", err)
		os.Exit(1)
	}
	defer listener.Close()

	fmt.Println("AegisHub started. Listening on", socket)

	conns := &sync.Map{}

	// The hub listens only on its UNIX socket. Firecracker guests reach it
	// through the daemon's guest hub bridge (Firecracker exposes guest vsock
	// as host UNIX sockets, not host AF_VSOCK).

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("Accept error: %v", err)
			continue
		}
		go handleConnection(conn, conns)
	}
}

type gitConn struct {
	net.Conn
	r *bufio.Reader
}

func (g *gitConn) Read(p []byte) (int, error) { return g.r.Read(p) }

func parseCIDKey(s string) (uint32, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	u, err := strconv.ParseUint(s, 10, 32)
	if err != nil {
		return 0, false
	}
	return uint32(u), true
}

func lookupPeerTenant(pub string) string {
	pub = strings.TrimSpace(pub)
	if pub == "" {
		return ""
	}
	path := strings.TrimSpace(os.Getenv("AEGIS_GIT_IDENTITIES"))
	if path == "" {
		return ""
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var m map[string]string
	if json.Unmarshal(b, &m) != nil {
		return ""
	}
	return strings.TrimSpace(m[pub])
}

func tenantForGit(verifiedPub string, remoteAddr net.Addr) (string, error) {
	verifiedPub = strings.TrimSpace(verifiedPub)
	if verifiedPub == "" {
		return "", fmt.Errorf("ERR_UNKNOWN_PEER")
	}
	if !unixGitAllowed() {
		return "", fmt.Errorf("ERR_UNKNOWN_PEER")
	}
	tenant := lookupPeerTenant(verifiedPub)
	if tenant == "" {
		return "", fmt.Errorf("ERR_UNKNOWN_PEER")
	}
	return tenant, nil
}

// daemonUnleaseCID is in-process CAS unlease for tests (VM destroy). Production
// StopVM sends daemon-only Hub command cid.unlease. Git-connect/guest hangup
// must not call this.
func daemonUnleaseCID(cid uint32, expectedPub string) {
	hublease.UnleaseCID(cid, expectedPub)
}

// isPersistentDaemonHubID is assigned_id of the long-lived unix daemon
// registration. Not daemon-temp-* / aegis-daemon-temp-* glob.
func isPersistentDaemonHubID(id string) bool {
	return id == "daemon"
}

func daemonMayUnleaseCID(assignedID string, wire wireMessage, msg Message) bool {
	if assignedID == "git-remote-hub" || msg.Source == "git-remote-hub" {
		return false
	}
	if isPersistentDaemonHubID(assignedID) {
		return true
	}
	registeredMutex.RLock()
	d, ok := registered["daemon"]
	registeredMutex.RUnlock()
	if !ok || d == nil || len(d.PublicKey) == 0 {
		return false
	}
	if msg.Signature == "" || msg.Signature == "dummy" {
		return false
	}
	if verifyWireSignature(wire, d.PublicKey) {
		return true
	}
	return verifySignature(msg, d.PublicKey)
}

func parseCIDUnleasePayload(payload interface{}) (uint32, string, bool) {
	m, ok := payload.(map[string]interface{})
	if !ok || m == nil {
		return 0, "", false
	}
	var cid uint32
	switch v := m["cid"].(type) {
	case float64:
		if v <= 0 || v != float64(uint32(v)) {
			return 0, "", false
		}
		cid = uint32(v)
	case uint32:
		cid = v
	case json.Number:
		u, err := strconv.ParseUint(v.String(), 10, 32)
		if err != nil || u == 0 {
			return 0, "", false
		}
		cid = uint32(u)
	case string:
		u, err := strconv.ParseUint(strings.TrimSpace(v), 10, 32)
		if err != nil || u == 0 {
			return 0, "", false
		}
		cid = uint32(u)
	default:
		return 0, "", false
	}
	pub, _ := m["public_key"].(string)
	if strings.TrimSpace(pub) == "" {
		pub, _ = m["pub"].(string)
	}
	pub = strings.TrimSpace(pub)
	if cid == 0 || pub == "" {
		return 0, "", false
	}
	return cid, pub, true
}

func handleCIDUnlease(msg Message, wire wireMessage, conn net.Conn, connID string) Message {
	deny := Message{
		Source:      "hub",
		Destination: msg.Source,
		Command:     "error",
		Payload:     "ERR_UNAUTHORIZED",
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}
	if conn != nil {
		if _, isVsock := conn.RemoteAddr().(*vsock.Addr); isVsock {
			return deny
		}
	}
	if !daemonMayUnleaseCID(connID, wire, msg) {
		return deny
	}
	cid, pub, ok := parseCIDUnleasePayload(msg.Payload)
	if !ok {
		deny.Payload = "ERR_INVALID_PAYLOAD"
		return deny
	}
	_, live := hublease.LoadLease(cid)
	unleased := hublease.UnleaseCID(cid, pub)
	if unleased || !live {
		if path := strings.TrimSpace(os.Getenv("AEGIS_GIT_CID_KEYS")); path != "" {
			hublease.DeleteCIDKeyIf(path, cid, pub)
		}
	}
	if !unleased {
		return Message{
			Source:      "hub",
			Destination: msg.Source,
			Command:     "response",
			Payload:     map[string]interface{}{"status": "noop"},
			Timestamp:   time.Now().UTC().Format(time.RFC3339),
		}
	}
	return Message{
		Source:      "hub",
		Destination: msg.Source,
		Command:     "response",
		Payload:     map[string]interface{}{"status": "ok"},
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}
}

func handleCIDLease(msg Message, wire wireMessage, conn net.Conn, connID string) Message {
	_ = wire
	_ = conn
	_ = connID
	return Message{
		Source:      "hub",
		Destination: msg.Source,
		Command:     "error",
		Payload:     "ERR_UNAUTHORIZED",
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}
}

func handleCIDLeaseCommand(msg Message, wire wireMessage, conn net.Conn, connID string) Message {
	if msg.Command == "cid.lease" {
		return handleCIDLease(msg, wire, conn, connID)
	}
	return handleCIDUnlease(msg, wire, conn, connID)
}

func verifyGitRegisterSignature(raw []byte, msg Message, pubKey ed25519.PublicKey) bool {
	if msg.Signature == "" || msg.Signature == "dummy" {
		return false
	}
	var wire wireMessage
	if json.Unmarshal(raw, &wire) == nil && verifyWireSignature(wire, pubKey) {
		return true
	}
	return verifySignature(msg, pubKey)
}

func handleConnection(conn net.Conn, conns *sync.Map) {
	defer conn.Close()
	// Peer creds exist only on a real UNIX socket (*net.UnixConn).
	// vsock peers are microVMs bound by CID lease, not host uid.
	// net.Pipe (and test wrappers around it) is in-process and has no SO_PEERCRED.
	if _, isUnix := conn.(*net.UnixConn); isUnix {
		peerUID, peerOK := hubPeerUIDLookup(conn)
		if !authorizeHubPeer(peerUID, peerOK, os.Geteuid(), hubOriginalUID()) {
			_ = json.NewEncoder(conn).Encode(map[string]string{"error": "ERR_UNAUTHORIZED_PEER"})
			log.Printf("Audit: rejected unix hub peer uid=%d peerOK=%v", peerUID, peerOK)
			return
		}
	}
	br := bufio.NewReader(conn)
	encoder := json.NewEncoder(conn)

	// First message must be register, line-oriented so git-remote-hub can
	// follow with a plaintext git-connect line. json.Decoder.Buffered() leaves
	// the trailing newline (or worse, swallows git-connect) and Hub closes
	// before the helper writes the service line.
	// Note: the readiness probe in startManagedHub does a Dial + immediate Close()
	// (no data) to test if the socket is accepting. That produces a clean EOF here
	// on the first read and is expected / harmless (not a real client register
	// failure). We log at debug or only for non-EOF errors to keep startup logs clean.
	raw, err := br.ReadBytes('\n')
	if err != nil {
		es := err.Error()
		if es != "EOF" && es != "unexpected EOF" && err != io.EOF && err != io.ErrUnexpectedEOF {
			log.Printf("Failed to decode register message: %v (remote=%v local=%v)", err, conn.RemoteAddr(), conn.LocalAddr())
		}
		return
	}
	if strings.HasPrefix(strings.TrimSpace(string(raw)), "git-connect") {
		_, _ = fmt.Fprintf(conn, "deny no git identity\n")
		return
	}
	var regMsg Message
	if err := json.Unmarshal(raw, &regMsg); err != nil {
		log.Printf("Failed to decode register message: %v (remote=%v local=%v)", err, conn.RemoteAddr(), conn.LocalAddr())
		return
	}
	if regMsg.Destination != "hub" || regMsg.Command != "register" {
		log.Printf("First message not register: %+v", regMsg)
		encoder.Encode(map[string]string{"error": "ERR_INVALID_HANDSHAKE"})
		return
	}
	// Before pubkey parsing, storeCIDLease, and any registered/conns mutation.
	// A rejected id must not fill a CID lease.
	// The hub has no vsock transport; guests come through the guest hub
	// bridge on the UNIX socket.
	if isVMHubTransport(conn) {
		_ = encoder.Encode(map[string]string{"error": "ERR_UNAUTHORIZED_PEER"})
		log.Printf("Audit: rejected vsock hub peer (%v)", conn.RemoteAddr())
		return
	}
	if reason, reserved := reservedIDReason(regMsg.Source, isVMHubTransport(conn)); reserved {
		_ = encoder.Encode(map[string]string{"error": "ERR_RESERVED_ID"})
		log.Printf("Audit: rejected registration of reserved hub id %s (%s)", regMsg.Source, reason)
		return
	}

	// Parse payload for public key
	payloadMap, ok := regMsg.Payload.(map[string]interface{})
	if !ok {
		encoder.Encode(map[string]string{"error": "ERR_INVALID_PAYLOAD"})
		return
	}
	pubKeyStr, ok := payloadMap["public_key"].(string)
	if !ok {
		encoder.Encode(map[string]string{"error": "ERR_MISSING_PUBLIC_KEY"})
		return
	}
	pubKeyBytes, err := base64.StdEncoding.DecodeString(pubKeyStr)
	if err != nil {
		encoder.Encode(map[string]string{"error": "ERR_INVALID_PUBLIC_KEY"})
		return
	}
	if len(pubKeyBytes) != ed25519.PublicKeySize {
		encoder.Encode(map[string]string{"error": "ERR_INVALID_PUBLIC_KEY"})
		return
	}
	pubKey := ed25519.PublicKey(pubKeyBytes)

	if regMsg.Source == "git-remote-hub" {
		// Possession of AEGIS_HUB_PRIVKEY: dummy/empty never count, even in AEGIS_DEV_MODE.
		// vsock: lease[CID] pub must equal verified pub, then tenant=identities[pub].
		// unix: production always ERR_UNKNOWN_PEER (no Serve). Sit hubBin: -tags testunixgit. Never payload.tenant.
		// Helper/git-connect hangup must not UnleaseCID.
		if !verifyGitRegisterSignature(raw, regMsg, pubKey) {
			_ = encoder.Encode(map[string]string{"error": "ERR_INVALID_SIGNATURE"})
			return
		}
		tenant, err := tenantForGit(pubKeyStr, conn.RemoteAddr())
		if err != nil || tenant == "" {
			code := "ERR_UNKNOWN_PEER"
			if err != nil {
				code = err.Error()
			}
			_ = encoder.Encode(map[string]string{"error": code})
			return
		}
		if err := encoder.Encode(map[string]string{"status": "registered"}); err != nil {
			return
		}
		hubgit.Serve(&gitConn{Conn: conn, r: br}, tenant, strings.TrimSpace(os.Getenv("AEGIS_STORE_GIT_SOCKET")))
		return
	}

	// Extract version from payload if available
	version := "unknown"
	if versionStr, ok := payloadMap["version"].(string); ok {
		version = versionStr
		log.Printf("Hub: Registered component %s with version %s", regMsg.Source, version)
	} else {
		log.Printf("Hub: Registered component %s with no version (payload: %+v)", regMsg.Source, payloadMap)
	}

	// Check if already registered
	registeredMutex.Lock()
	componentID := regMsg.Source

	// For daemon connections: if already registered, use a temporary ID.
	// That id is distinct, so Swap must not close the persistent daemon conn.
	if regMsg.Source == "daemon" {
		if _, exists := registered[regMsg.Source]; exists {
			// This is a fresh daemon connection (not the persistent one)
			// Give it a temporary ID
			tempConnMutex.Lock()
			tempConnCounter++
			componentID = fmt.Sprintf("daemon-temp-%d", tempConnCounter)
			tempConnMutex.Unlock()
			log.Printf("Hub: Fresh daemon connection registered as %s (original daemon still at %s)", componentID, regMsg.Source)
		}
	} else if _, exists := registered[regMsg.Source]; exists {
		// Allow re-registration when a guest hub bridge reconnects or a VM restarts.
		log.Printf("Hub: component %s re-registering — replacing previous connection", regMsg.Source)
	}

	decoder := json.NewDecoder(br)
	encoders := &ComponentEncoders{
		Encoder: encoder,
		Decoder: decoder,
		Mutex:   sync.Mutex{},
	}
	registered[componentID] = &RegisteredComponent{ID: componentID, PublicKey: pubKey, Encoders: encoders, Version: version}
	old, loaded := conns.Swap(componentID, conn)
	registeredMutex.Unlock()
	if loaded && old != conn {
		if oldConn, ok := old.(net.Conn); ok && oldConn != nil {
			log.Printf("Hub: closing replaced connection for %s", componentID)
			_ = oldConn.Close()
		}
	}
	debugLog("hub", fmt.Sprintf("Registered component %s (hub id %s) version %s", regMsg.Source, componentID, version))

	// A re-register overwrites registered[id] before this conn's defer runs.
	// Delete only if this conn still owns the slot.
	defer func(id string, enc *ComponentEncoders, c net.Conn) {
		registeredMutex.Lock()
		owns := false
		if cur, ok := registered[id]; ok && cur != nil && cur.Encoders == enc {
			delete(registered, id)
			owns = true
		}
		registeredMutex.Unlock()
		conns.CompareAndDelete(id, c)
		if owns {
			debugLog("hub", fmt.Sprintf("Cleaned up registration for %s", id))
		}
	}(componentID, encoders, conn)

	// Send ACL rules for this component, including the assigned ID
	response := map[string]interface{}{
		"status":      "registered",
		"assigned_id": componentID, // Send the assigned ID back to the client
		"acls":        aclRules,    // TODO: filter for this component
	}
	encoders.Mutex.Lock()
	encoders.Encoder.Encode(response)
	encoders.Mutex.Unlock()

	// Push permission + visibility snapshot to agent-like microVMs (permissions-model.md §Enforcement flow step 1).
	if shouldReceivePermissionSnapshot(componentID) {
		snap, _ := fetchPermissionSnapshotFromStore(componentID)
		pushPermissionSnapshot(componentID, encoders, snap)
	}

	if isEphemeralHubClient(componentID) {
		ephemeralHubRPCLoop(componentID, encoders, conn)
		return
	}

	// Now handle normal messages
	for {
		msg, wire, err := decodeHubFrame(decoder)
		if err != nil {
			debugLog("hub", fmt.Sprintf("Decode error: %v", err))
			return
		}

		debugLog("hub", fmt.Sprintf("Received message from %s to %s, command: %s", msg.Source, msg.Destination, msg.Command))
		if collab.TraceEnabled() && (msg.Command == "channel.updated" || msg.Command == "channel.activity" || msg.Command == "channel.turn" || msg.Command == "channel.post" || msg.Command == "channel.relay_activity" || msg.Command == "channel.get_relevant_since" || msg.Command == "channel.get_messages") {
			collab.Tracef("hub", "route", "src=%s dest=%s cmd=%s", msg.Source, msg.Destination, msg.Command)
		}

		// Signed Source is a claim. Verify against this connection's registered
		// componentID, then overwrite msg.Source before ACL/route so a daemon
		// forging Source=store cannot wrong-actor pass as store (e.g. pr.merge).
		registeredMutex.RLock()
		regComp, exists := registered[componentID]
		registeredMutex.RUnlock()
		if !exists {
			debugLog("hub", fmt.Sprintf("Unauthorized connection componentID %s (claimed source %s)", componentID, msg.Source))
			encoder.Encode(map[string]string{"error": "ERR_UNAUTHORIZED"})
			log.Printf("Audit: unauthorized connection %s (claimed source %s)", componentID, msg.Source)
			continue
		}
		// Signature is now strictly required for all real traffic (per aegishub.md + security model).
		// "dummy" is only for early dev; it is logged and treated as failure in non-dev mode.
		if msg.Signature == "" || msg.Signature == "dummy" {
			if os.Getenv("AEGIS_DEV_MODE") != "1" {
				encoder.Encode(map[string]string{"error": "ERR_SIGNATURE_REQUIRED"})
				log.Printf("Audit: missing or dummy signature from %s (set AEGIS_DEV_MODE=1 to allow during development)", componentID)
				continue
			}
			log.Printf("DEV MODE: allowing dummy signature from %s", componentID)
		} else if !verifyWireSignature(wire, regComp.PublicKey) {
			encoder.Encode(map[string]string{"error": "ERR_INVALID_SIGNATURE"})
			log.Printf("Audit: invalid signature from %s", componentID)
			continue
		}
		msg.Source = componentID

		if msg.Destination == "hub" && (msg.Command == "cid.unlease" || msg.Command == "cid.lease") {
			encoders.Mutex.Lock()
			_ = encoders.Encoder.Encode(handleCIDLeaseCommand(msg, wire, conn, componentID))
			encoders.Mutex.Unlock()
			continue
		}

		// Check ACL (skip for version commands for debugging)
		if msg.Command != "get-version" && !checkACL(msg.Source, msg.Destination, msg.Command) {
			encoder.Encode(map[string]string{"error": "ERR_ACL_VIOLATION"})
			log.Printf("Audit: ACL violation %s -> %s : %s", msg.Source, msg.Destination, msg.Command)
			continue
		}

		// Fine-grained permission check (permissions-model.md §Enforcement)
		if allowed, reason := checkHubPermission(msg.Source, msg.Command); !allowed {
			encoder.Encode(map[string]string{"error": reason})
			log.Printf("Audit: permission denied %s : %s -> %s", msg.Source, msg.Command, msg.Destination)
			continue
		}

		// Hot invalidation on grant/visibility changes from Store
		if msg.Command == "permission.granted" || msg.Command == "permission.revoked" || msg.Command == "visibility.set" {
			handlePermissionInvalidationEvent(msg)
		}

		if msg.Destination == "hub" {
			if msg.Command == "component.list" {
				debugLog("hub", fmt.Sprintf("Received component.list query from %s", msg.Source))
				// Return list of all registered components with versions
				var components []map[string]string
				registeredMutex.RLock()
				for id, comp := range registered {
					if id != "daemon" { // Don't list the daemon itself
						debugLog("hub", fmt.Sprintf("  Including component %s version %s", id, comp.Version))
						components = append(components, map[string]string{
							"id":      id,
							"version": comp.Version,
						})
					}
				}
				registeredMutex.RUnlock()
				response := map[string]interface{}{
					"components": components,
				}
				debugLog("hub", fmt.Sprintf("Sending component.list response with %d components", len(components)))
				encoder.Encode(response)
			} else if msg.Command == "tool.list" {
				// Forward to store
				storeMsg := msg
				storeMsg.Destination = "store"
				registeredMutex.RLock()
				storeComp, ok := registered["store"]
				registeredMutex.RUnlock()
				if ok && storeComp.Encoders != nil {
					storeComp.Encoders.Mutex.Lock()
					storeComp.Encoders.Encoder.Encode(storeMsg)
					storeComp.Encoders.Mutex.Unlock()
					// Wait for response from store
					var storeResp Message
					err := decoder.Decode(&storeResp)
					if err != nil {
						errorMsg := map[string]string{"error": "failed to get from store"}
						encoder.Encode(errorMsg)
					} else {
						encoder.Encode(storeResp.Payload)
					}
				} else {
					errorMsg := map[string]string{"error": "store not available"}
					encoder.Encode(errorMsg)
				}
			} else {
				// Handle other hub commands
				response := map[string]interface{}{
					"status": "ok",
					"echo":   msg.Payload,
				}
				encoder.Encode(response)
			}
		} else {
			// Correlate replies (store channel.*, memory.*, PM response, etc.) with in-flight
			// hub RPC waiters. Without this, a store reply is mistaken for a new outbound RPC
			// from store and the original caller (aegis-cli-internal, daemon-internal) hangs.
			if deliverPendingRPC(msg) {
				continue
			}
			// One-way replies (agent poll/chat responses) vs synchronous RPC (memory.get_context, llm.call).
			if isOneWayHubReply(msg.Command) {
				forwardReplyToRequester(msg)
				continue
			}
			// Store channel relays are fire-and-forget. Do not RPC-correlate on store's
			// connection or the hub injects response/error frames into the store read loop.
			if msg.Source == "store" && msg.Command == "channel.relay_activity" {
				registeredMutex.RLock()
				destComponent, exists := registered[msg.Destination]
				registeredMutex.RUnlock()
				if exists && destComponent.Encoders != nil {
					destComponent.Encoders.Mutex.Lock()
					_ = destComponent.Encoders.Encoder.Encode(msg)
					destComponent.Encoders.Mutex.Unlock()
				} else {
					debugLog("hub", fmt.Sprintf("channel.relay_activity dropped: %s not registered", msg.Destination))
				}
				continue
			}
			if msg.Source == "store" && msg.Command == "channel.updated" {
				collab.Tracef("hub", "channel.updated.forward", "dest=%s", msg.Destination)
				registeredMutex.RLock()
				destComponent, exists := registered[msg.Destination]
				registeredMutex.RUnlock()
				if exists && destComponent.Encoders != nil {
					destComponent.Encoders.Mutex.Lock()
					_ = destComponent.Encoders.Encoder.Encode(msg)
					destComponent.Encoders.Mutex.Unlock()
				}
				continue
			}
			if componentID == "store" && msg.Source == "store" {
				debugLog("hub", fmt.Sprintf("store outbound ignored cmd=%q dest=%s", msg.Command, msg.Destination))
				continue
			}
			reply := forwardHubRPC(componentID, msg)
			encoders.Mutex.Lock()
			_ = encoders.Encoder.Encode(reply)
			encoders.Mutex.Unlock()
		}
	}
}

// isOneWayHubReply reports commands that are fire-and-forget replies on the wire (hubclient.Reply),
// not request/response RPC pairs (hubclient.Send).
// isOneWayHubPush reports dest-bound commands that must not wait for the dest
// to Reply. channel.turn is inboxed by the guest while it is inside llm.call;
// blocking the facilitator RPC for that Reply made deliverTurn time out and
// fall through to the unregistered "project-manager" alias (ERR_DESTINATION_NOT_FOUND).
// llm.usage.record is best-effort metrics. Waiting for Store would stall
// network-boundary's hub read loop, and the next llm.call.response behind it,
// when Store is slow or hung. Store logs rejections and does not reply.
// audit.append is the same: senders write it and move on, and Store does not
// reply, so waiting would hold the sender's frames until the RPC timeout.
func isOneWayHubPush(command string) bool {
	switch command {
	case "channel.turn", "channel.activity", "channel.member_notify", "llm.usage.record", "audit.append":
		return true
	default:
		return false
	}
}

func isOneWayHubReply(command string) bool {
	if hubclient.IsUnsolicitedCommand(command) {
		return false
	}
	if command == "response" || command == "ack" {
		return true
	}
	// Callee RPC replies. Without this, llm.call.response is treated as a new
	// outbound RPC from network-boundary and blocks that guest's hub loop.
	if strings.HasSuffix(command, ".response") {
		return true
	}
	switch command {
	case "channel.posted", "channel.data", "channel.created", "channel.joined",
		"channel.archived", "channel.list", "channel.member_added",
		"memory.context", "memory.response":
		return true
	}
	return false
}

// ephemeralHubRPCLoop serves one-shot daemon hub clients (sendToComponentViaHub).
// It is the only reader on the connection, avoiding races with hubclient.Send.
func ephemeralHubRPCLoop(requesterID string, encoders *ComponentEncoders, conn net.Conn) {
	for {
		encoders.Mutex.Lock()
		msg, wire, err := decodeHubFrame(encoders.Decoder)
		encoders.Mutex.Unlock()
		if err != nil {
			debugLog("hub", fmt.Sprintf("ephemeral RPC %s decode end: %v", requesterID, err))
			return
		}
		// Signed Source is a claim; bind to this connection assigned ID.
		msg.Source = requesterID
		if msg.Destination == "hub" && (msg.Command == "cid.unlease" || msg.Command == "cid.lease") {
			reply := handleCIDLeaseCommand(msg, wire, conn, requesterID)
			encoders.Mutex.Lock()
			_ = encoders.Encoder.Encode(reply)
			encoders.Mutex.Unlock()
			continue
		}
		if msg.Command != "get-version" && !checkACL(msg.Source, msg.Destination, msg.Command) {
			reply := Message{Command: "error", Payload: "ERR_ACL_VIOLATION"}
			encoders.Mutex.Lock()
			_ = encoders.Encoder.Encode(reply)
			encoders.Mutex.Unlock()
			continue
		}
		reply := forwardHubRPC(requesterID, msg)
		encoders.Mutex.Lock()
		_ = encoders.Encoder.Encode(reply)
		encoders.Mutex.Unlock()
	}
}

func forwardHubRPC(requesterID string, msg Message) Message {
	registeredMutex.RLock()
	destComponent, exists := registered[msg.Destination]
	registeredMutex.RUnlock()
	if !exists || destComponent.Encoders == nil {
		debugLog("hub", fmt.Sprintf("RPC %s -> %s: ERR_DESTINATION_NOT_FOUND (registered=%d)", msg.Source, msg.Destination, len(registered)))
		return Message{Command: "error", Payload: "ERR_DESTINATION_NOT_FOUND"}
	}

	if isOneWayHubPush(msg.Command) {
		debugLog("hub", fmt.Sprintf("push %s -> %s command %s", msg.Source, msg.Destination, msg.Command))
		destComponent.Encoders.Mutex.Lock()
		err := destComponent.Encoders.Encoder.Encode(msg)
		destComponent.Encoders.Mutex.Unlock()
		if err != nil {
			return Message{Command: "error", Payload: err.Error()}
		}
		return Message{
			Source:      "hub",
			Destination: requesterID,
			Command:     "response",
			Payload:     map[string]string{"status": "accepted"},
			Timestamp:   time.Now().UTC().Format(time.RFC3339),
		}
	}

	debugLog("hub", fmt.Sprintf("RPC %s -> %s command %s (awaiting reply)", msg.Source, msg.Destination, msg.Command))
	waitCh := registerPendingRPC(requesterID, msg.Destination, msg.Command)
	defer clearPendingRPC(requesterID)

	destComponent.Encoders.Mutex.Lock()
	if err := destComponent.Encoders.Encoder.Encode(msg); err != nil {
		destComponent.Encoders.Mutex.Unlock()
		return Message{Command: "error", Payload: err.Error()}
	}
	destComponent.Encoders.Mutex.Unlock()

	rpcTimeout := 120 * time.Second
	switch msg.Command {
	case "chat.message", "user.turn", "user.goal":
		// user.goal (PM plan generation via real LLM) can exceed 120s on cold on-demand PM boot + Ollama.
		rpcTimeout = 600 * time.Second
	case "chat.tool_events", "chat.thought_events", "chat.stream_progress":
		rpcTimeout = 8 * time.Second
	case "channel.activity", "channel.turn":
		// Agent may channel.post to store before replying.
		rpcTimeout = 180 * time.Second
	case "channel.post":
		rpcTimeout = 60 * time.Second
	}

	select {
	case reply := <-waitCh:
		maybeInvalidatePermissionsFromReply(reply)
		return reply
	case <-time.After(rpcTimeout):
		return Message{Command: "error", Payload: "ERR_RPC_TIMEOUT"}
	}
}

func forwardReplyToRequester(msg Message) {
	registeredMutex.RLock()
	destComponent, exists := registered[msg.Destination]
	registeredMutex.RUnlock()
	if !exists || destComponent.Encoders == nil {
		return
	}
	if deliverPendingRPC(msg) {
		// Ephemeral daemon RPC consumed the reply; ack the sender so hubclient.Send
		// decode does not block and steal the next inbound RPC (e.g. chat.message).
		registeredMutex.RLock()
		srcComponent, srcOK := registered[msg.Source]
		registeredMutex.RUnlock()
		if srcOK && srcComponent.Encoders != nil {
			ack := Message{
				Source:      "hub",
				Destination: msg.Source,
				Command:     "ack",
				Payload:     map[string]string{"status": "delivered"},
				Timestamp:   time.Now().Format(time.RFC3339),
			}
			srcComponent.Encoders.Mutex.Lock()
			_ = srcComponent.Encoders.Encoder.Encode(ack)
			srcComponent.Encoders.Mutex.Unlock()
		}
		return
	}
	destComponent.Encoders.Mutex.Lock()
	_ = destComponent.Encoders.Encoder.Encode(msg)
	destComponent.Encoders.Mutex.Unlock()
}

func debugLog(component, msg string) {
	f, _ := os.OpenFile("/tmp/hub-debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
	if f != nil {
		defer f.Close()
		fmt.Fprintf(f, "[%s][%s] %s\n", component, time.Now().Format("15:04:05.000"), msg)
	}
}

func main() {
	if env := os.Getenv("AEGIS_HUB_SOCKET"); env != "" {
		hubSocketPath = env
	}
	var rootCmd = &cobra.Command{Use: "aegishub"}

	var startCmd = &cobra.Command{
		Use:   "start",
		Short: "Start the AegisHub",
		Run:   startHub,
	}

	rootCmd.AddCommand(startCmd)
	rootCmd.Execute()
}
